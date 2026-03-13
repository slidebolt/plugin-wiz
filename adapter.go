package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/slidebolt/plugin-wiz/pkg/wiz"
	regsvc "github.com/slidebolt/registry"
	"github.com/slidebolt/sdk-entities/light"
	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

type WizBulb struct {
	MAC             string `json:"mac"`
	IP              string `json:"ip"`
	SupportsRGB     bool   `json:"supports_rgb"`
	SupportsTemp    bool   `json:"supports_temp"`
	SupportsScene   bool   `json:"supports_scene"`
	SupportsDimming bool   `json:"supports_dimming"`
}

type wizState struct {
	Bulbs map[string]WizBulb `json:"bulbs"` // deviceID → WizBulb
}

type WizPlugin struct {
	client  wiz.Client
	reg     *regsvc.Registry
	events  runner.EventService
	bulbs   map[string]WizBulb
	bulbsMu sync.RWMutex

	stopListen    func()
	discoveryDone chan struct{}
	discovering   int32
	discoveryMu   sync.Mutex
	lastDiscovery time.Time
}

func NewWizPlugin(client wiz.Client) *WizPlugin {
	if client == nil {
		client = wiz.NewRealClient()
	}
	return &WizPlugin{
		client:        client,
		bulbs:         make(map[string]WizBulb),
		discoveryDone: make(chan struct{}),
	}
}

func (p *WizPlugin) Initialize(ctx runner.PluginContext) (types.Manifest, error) {
	p.reg = ctx.Registry
	p.events = ctx.Events

	if state, ok := ctx.Registry.LoadState(); ok && len(state.Data) > 0 {
		var ws wizState
		if err := json.Unmarshal(state.Data, &ws); err == nil && ws.Bulbs != nil {
			p.bulbsMu.Lock()
			p.bulbs = ws.Bulbs
			p.bulbsMu.Unlock()
			for _, bulb := range ws.Bulbs {
				p.registerBulb(bulb)
			}
		}
	}

	return types.Manifest{ID: "plugin-wiz", Name: "Wiz Plugin", Version: "1.0.0", Schemas: types.CoreDomains()}, nil
}

func (p *WizPlugin) Start(ctx context.Context) error {
	stop, err := p.client.Listen(func(ip string, result wiz.SystemConfig) {
		p.upsertBulb(ip, result.Mac)
	})
	if err != nil {
		return nil
	}
	p.stopListen = stop
	p.triggerDiscovery()
	return p.waitReady(ctx)
}

func (p *WizPlugin) waitReady(ctx context.Context) error {
	select {
	case <-p.discoveryDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *WizPlugin) Stop() error {
	if p.stopListen != nil {
		p.stopListen()
	}
	return nil
}

func (p *WizPlugin) OnReset() error {
	p.bulbsMu.Lock()
	p.bulbs = make(map[string]WizBulb)
	p.bulbsMu.Unlock()
	if p.reg != nil {
		return p.reg.DeleteState()
	}
	return nil
}

func (p *WizPlugin) runCommand(req types.Command, entity types.Entity) (types.Entity, error) {
	if entity.Domain != light.Type {
		return entity, fmt.Errorf("unsupported domain: %s", entity.Domain)
	}
	bulb, ok := p.findByDeviceID(entity.DeviceID)
	if !ok {
		return entity, fmt.Errorf("wiz bulb not found for device %s", entity.DeviceID)
	}
	lc := light.Command{}
	if err := json.Unmarshal(req.Payload, &lc); err != nil {
		return entity, err
	}
	if err := light.ValidateCommand(lc); err != nil {
		return entity, err
	}

	actions := entity.Actions
	if len(actions) == 0 {
		actions = bulbActions(bulb)
	}
	if len(actions) > 0 && !containsAction(actions, lc.Type) {
		return entity, fmt.Errorf("action %q is not supported by this light", lc.Type)
	}

	params, err := commandToPilotParams(lc)
	if err != nil {
		return entity, err
	}

	if err := p.client.SetPilot(bulb.IP, bulb.MAC, params); err != nil {
		entity.Data.SyncStatus = types.SyncStatusFailed
		errorMap := map[string]string{"error": mapErrorToMessage(err)}
		if rawErr, jsonErr := json.Marshal(errorMap); jsonErr == nil {
			entity.Data.Reported = rawErr
		}
		if p.reg != nil {
			_ = p.reg.SaveEntity(entity)
		}
		return entity, err
	}

	// Fetch current entity state from registry before updating.
	if p.reg != nil {
		if ent, ok := p.reg.GetEntity(p.reg.Namespace(), entity.DeviceID, entity.ID); ok {
			entity = ent
		}
	}
	store := light.Bind(&entity)
	if err := store.SetDesiredFromCommand(lc); err != nil {
		return entity, err
	}
	entity.Data.SyncStatus = types.SyncStatusPending

	if p.reg != nil {
		_ = p.reg.SaveEntity(entity)
	}

	pilot, err := p.client.GetPilot(bulb.IP)
	if err != nil {
		pilot = nil
	}
	payload, _ := json.Marshal(lightEventPayload(lc, pilot, entity.Actions))
	if p.events != nil {
		deviceID := entity.DeviceID
		entityID := entity.ID
		correlationID := req.ID
		go func() {
			time.Sleep(20 * time.Millisecond)
			_ = p.events.PublishEvent(types.InboundEvent{
				DeviceID:      deviceID,
				EntityID:      entityID,
				CorrelationID: correlationID,
				Payload:       json.RawMessage(payload),
			})
		}()
	}
	return entity, nil
}

func lightEventPayload(cmd light.Command, pilot map[string]any, actions []string) map[string]any {
	payload := map[string]any{
		"type":              cmd.Type,
		"cause":             "wiz_ack",
		"available_actions": actions,
	}

	if power, ok := pilotBool(pilot, "state"); ok {
		payload["power"] = power
	} else {
		switch cmd.Type {
		case light.ActionTurnOn:
			payload["power"] = true
		case light.ActionTurnOff:
			payload["power"] = false
		}
	}

	if brightness, ok := pilotInt(pilot, "dimming"); ok {
		payload["brightness"] = brightness
	} else if cmd.Brightness != nil {
		payload["brightness"] = *cmd.Brightness
	}

	if rgb, ok := pilotRGB(pilot); ok {
		payload["rgb"] = rgb
		payload["color_mode"] = light.ColorModeRGB
	} else if cmd.RGB != nil && len(*cmd.RGB) == 3 {
		payload["rgb"] = append([]int(nil), (*cmd.RGB)...)
		payload["color_mode"] = light.ColorModeRGB
	}

	if temperature, ok := pilotInt(pilot, "temp"); ok {
		payload["temperature"] = temperature
		if _, hasRGB := payload["rgb"]; !hasRGB {
			payload["color_mode"] = light.ColorModeTemperature
		}
	} else if cmd.Temperature != nil {
		payload["temperature"] = *cmd.Temperature
		if _, hasRGB := payload["rgb"]; !hasRGB {
			payload["color_mode"] = light.ColorModeTemperature
		}
	}

	if scene, ok := pilotScene(pilot); ok {
		payload["scene"] = scene
		if _, hasRGB := payload["rgb"]; !hasRGB {
			if _, hasTemp := payload["temperature"]; !hasTemp {
				payload["color_mode"] = light.ColorModeScene
			}
		}
	} else if cmd.Scene != nil && *cmd.Scene != "" {
		payload["scene"] = *cmd.Scene
		if _, hasRGB := payload["rgb"]; !hasRGB {
			if _, hasTemp := payload["temperature"]; !hasTemp {
				payload["color_mode"] = light.ColorModeScene
			}
		}
	}

	return payload
}

func pilotBool(pilot map[string]any, key string) (bool, bool) {
	if pilot == nil {
		return false, false
	}
	v, ok := pilot[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func pilotInt(pilot map[string]any, key string) (int, bool) {
	if pilot == nil {
		return 0, false
	}
	v, ok := pilot[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func pilotRGB(pilot map[string]any) ([]int, bool) {
	r, rok := pilotInt(pilot, "r")
	g, gok := pilotInt(pilot, "g")
	b, bok := pilotInt(pilot, "b")
	if !rok || !gok || !bok {
		return nil, false
	}
	return []int{r, g, b}, true
}

func pilotScene(pilot map[string]any) (string, bool) {
	scene, ok := pilotInt(pilot, "sceneId")
	if !ok {
		return "", false
	}
	return strconv.Itoa(scene), true
}

func (p *WizPlugin) OnCommand(req types.Command, entity types.Entity) error {
	_, err := p.runCommand(req, entity)
	return err
}

func (p *WizPlugin) triggerDiscovery() {
	if !atomic.CompareAndSwapInt32(&p.discovering, 0, 1) {
		return
	}
	go func() {
		defer atomic.StoreInt32(&p.discovering, 0)
		p.discover()
		select {
		case <-p.discoveryDone:
		default:
			close(p.discoveryDone)
		}
	}()
}

func (p *WizPlugin) discover() {
	p.discoveryMu.Lock()
	if time.Since(p.lastDiscovery) < 3*time.Second {
		p.discoveryMu.Unlock()
		return
	}
	p.lastDiscovery = time.Now()
	p.discoveryMu.Unlock()

	_ = p.client.SendProbe()
	hosts := parseStaticHosts(os.Getenv("WIZ_STATIC_HOSTS"))
	if len(hosts) == 0 {
		hosts = wiz.LocalSubnetCandidates()
	}
	wiz.ProbeHostsConcurrently(hosts, 64, func(host string) {
		cfg, err := p.client.GetSystemConfig(host)
		if err != nil || cfg == nil || cfg.Mac == "" {
			return
		}
		p.upsertBulb(host, cfg.Mac)
	})
}

// registerBulb saves a bulb's device and entity to the registry.
// Used on startup to re-register bulbs loaded from persisted state.
func (p *WizPlugin) registerBulb(bulb WizBulb) {
	deviceID := deviceIDForMAC(bulb.MAC)
	_ = p.reg.SaveDevice(types.Device{
		ID:         deviceID,
		PluginID:   "plugin-wiz",
		SourceID:   bulb.MAC,
		SourceName: "WiZ Light",
		Labels:     map[string][]string{"ip": {bulb.IP}},
	})
	_ = p.reg.SaveEntity(types.Entity{
		ID:        "light",
		DeviceID:  deviceID,
		PluginID:  "plugin-wiz",
		Domain:    light.Type,
		LocalName: "Light",
		Actions:   bulbActions(bulb),
	})
}

func (p *WizPlugin) upsertBulb(ip, mac string) {
	if mac == "" || ip == "" {
		return
	}
	n := normalizeMAC(mac)
	bulb := WizBulb{
		MAC:             n,
		IP:              ip,
		SupportsRGB:     true,
		SupportsTemp:    true,
		SupportsScene:   true,
		SupportsDimming: true,
	}
	deviceID := deviceIDForMAC(n)

	p.bulbsMu.Lock()
	p.bulbs[deviceID] = bulb
	snapshot := make(map[string]WizBulb, len(p.bulbs))
	for k, v := range p.bulbs {
		snapshot[k] = v
	}
	p.bulbsMu.Unlock()

	if p.reg != nil {
		ws := wizState{Bulbs: snapshot}
		if data, err := json.Marshal(ws); err == nil {
			_ = p.reg.SaveState(types.Storage{Data: data})
		}
		p.registerBulb(bulb)
	}
}

func (p *WizPlugin) findByDeviceID(deviceID string) (WizBulb, bool) {
	p.bulbsMu.RLock()
	defer p.bulbsMu.RUnlock()
	b, ok := p.bulbs[deviceID]
	return b, ok
}

func commandToPilotParams(cmd light.Command) (map[string]any, error) {
	testMode := strings.ToLower(os.Getenv("WIZ_TEST_MODE")) == "true"

	switch cmd.Type {
	case light.ActionTurnOn:
		return map[string]any{"state": true}, nil
	case light.ActionTurnOff:
		return map[string]any{"state": false}, nil
	case light.ActionSetBrightness:
		if cmd.Brightness == nil {
			return nil, fmt.Errorf("brightness is required")
		}
		brightness := *cmd.Brightness
		if testMode && brightness > 10 {
			brightness = 10
		}
		return map[string]any{"dimming": brightness}, nil
	case light.ActionSetRGB:
		if cmd.RGB == nil || len(*cmd.RGB) != 3 {
			return nil, fmt.Errorf("rgb must have 3 values")
		}
		rgb := *cmd.RGB
		r, g, b := rgb[0], rgb[1], rgb[2]
		if testMode {
			if r > 20 {
				r = 20
			}
			if g > 20 {
				g = 20
			}
			if b > 20 {
				b = 20
			}
		}
		params := map[string]any{"r": r, "g": g, "b": b}
		if testMode {
			params["dimming"] = 10
		}
		return params, nil
	case light.ActionSetTemperature:
		if cmd.Temperature == nil {
			return nil, fmt.Errorf("temperature is required")
		}
		return map[string]any{"temp": *cmd.Temperature}, nil
	case light.ActionSetScene:
		if cmd.Scene == nil || *cmd.Scene == "" {
			return nil, fmt.Errorf("scene is required")
		}
		sceneID, err := strconv.Atoi(*cmd.Scene)
		if err != nil {
			return nil, fmt.Errorf("scene must be numeric for wiz")
		}
		return map[string]any{"sceneId": sceneID}, nil
	default:
		return nil, fmt.Errorf("unsupported action: %s", cmd.Type)
	}
}

func bulbActions(b WizBulb) []string {
	actions := []string{light.ActionTurnOn, light.ActionTurnOff}
	if b.SupportsDimming {
		actions = append(actions, light.ActionSetBrightness)
	}
	if b.SupportsRGB {
		actions = append(actions, light.ActionSetRGB)
	}
	if b.SupportsTemp {
		actions = append(actions, light.ActionSetTemperature)
	}
	if b.SupportsScene {
		actions = append(actions, light.ActionSetScene)
	}
	return actions
}

func parseStaticHosts(raw string) []string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		h := strings.TrimSpace(p)
		if h != "" {
			out = append(out, h)
		}
	}
	return out
}

func normalizeMAC(mac string) string {
	repl := strings.NewReplacer(":", "", "-", "", ".", "")
	return strings.ToLower(repl.Replace(strings.TrimSpace(mac)))
}

func deviceIDForMAC(mac string) string { return "wiz-" + normalizeMAC(mac) }

func containsAction(actions []string, action string) bool {
	for _, a := range actions {
		if a == action {
			return true
		}
	}
	return false
}

func mapErrorToMessage(err error) string {
	switch {
	case wiz.IsDeviceOffline(err):
		return "Device is offline"
	case wiz.IsTimeout(err):
		return "Request timed out"
	case wiz.IsUnauthorized(err):
		return "Authentication failed"
	default:
		return err.Error()
	}
}
