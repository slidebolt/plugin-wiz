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

type WizPlugin struct {
	client     wiz.Client
	eventSink  runner.EventSink
	rawStore   runner.RawStore
	stopListen func()

	discoveryMu   sync.Mutex
	lastDiscovery time.Time
	discovering   int32
	discoveryDone chan struct{} // signals when discovery completes
}

func NewWizPlugin(client wiz.Client) *WizPlugin {
	if client == nil {
		client = wiz.NewRealClient()
	}
	return &WizPlugin{
		client:        client,
		discoveryDone: make(chan struct{}),
	}
}

func (p *WizPlugin) OnInitialize(config runner.Config, state types.Storage) (types.Manifest, types.Storage) {
	p.eventSink = config.EventSink
	p.rawStore = config.RawStore
	return types.Manifest{ID: "plugin-wiz", Name: "Wiz Plugin", Version: "1.0.0", Schemas: types.CoreDomains()}, state
}

func (p *WizPlugin) OnReady() {
	stop, err := p.client.Listen(func(ip string, result wiz.SystemConfig) {
		p.upsertBulb(ip, result.Mac)
	})
	if err != nil {
		return
	}
	p.stopListen = stop
	p.triggerDiscovery()
}

func (p *WizPlugin) WaitReady(ctx context.Context) error {
	select {
	case <-p.discoveryDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *WizPlugin) OnShutdown() {
	if p.stopListen != nil {
		p.stopListen()
	}
}

func (p *WizPlugin) OnHealthCheck() (string, error) { return "perfect", nil }

func (p *WizPlugin) OnConfigUpdate(current types.Storage) (types.Storage, error) {
	return current, nil
}

func (p *WizPlugin) OnDeviceCreate(dev types.Device) (types.Device, error) { return dev, nil }
func (p *WizPlugin) OnDeviceUpdate(dev types.Device) (types.Device, error) { return dev, nil }
func (p *WizPlugin) OnDeviceDelete(id string) error                        { return nil }

func (p *WizPlugin) OnDeviceDiscover(current []types.Device) ([]types.Device, error) {
	p.triggerDiscovery()

	existing := map[string]types.Device{}
	for _, d := range current {
		existing[d.ID] = d
	}

	// Check raw store for any devices not in current
	if p.rawStore != nil {
		for _, dev := range current {
			raw, err := p.rawStore.ReadRawDevice(dev.ID)
			if err != nil || len(raw) == 0 {
				continue
			}
			var b WizBulb
			if err := json.Unmarshal(raw, &b); err == nil && b.MAC != "" {
				id := deviceIDForMAC(b.MAC)
				discoveredDev := types.Device{
					ID:         id,
					SourceID:   b.MAC,
					SourceName: "WiZ Light",
				}
				if existingDev, ok := existing[id]; ok {
					existing[id] = runner.ReconcileDevice(existingDev, discoveredDev)
				} else {
					existing[id] = runner.ReconcileDevice(types.Device{}, discoveredDev)
				}
			}
		}
	}

	out := make([]types.Device, 0, len(existing))
	for _, d := range existing {
		out = append(out, d)
	}
	return runner.EnsureCoreDevice("plugin-wiz", out), nil
}

func (p *WizPlugin) OnDeviceSearch(q types.SearchQuery, results []types.Device) ([]types.Device, error) {
	return results, nil
}

func (p *WizPlugin) OnEntityCreate(ent types.Entity) (types.Entity, error) {
	if ent.Domain == light.Type {
		store := light.Bind(&ent)
		store.EnsureDefaultActions()
	}
	return ent, nil
}

func (p *WizPlugin) OnEntityUpdate(ent types.Entity) (types.Entity, error) { return ent, nil }
func (p *WizPlugin) OnEntityDelete(deviceID, entityID string) error        { return nil }

func (p *WizPlugin) OnEntityDiscover(deviceID string, current []types.Entity) ([]types.Entity, error) {
	current = runner.EnsureCoreEntities("plugin-wiz", deviceID, current)
	bulb, ok := p.findByDeviceID(deviceID)
	if !ok {
		return current, nil
	}
	entityID := entityIDForMAC(bulb.MAC)
	for _, e := range current {
		if e.ID == entityID {
			return current, nil
		}
	}
	actions := bulbActions(bulb)
	ent := types.Entity{
		ID:        entityID,
		DeviceID:  deviceID,
		Domain:    light.Type,
		LocalName: "Light",
		Actions:   actions,
	}
	return append(current, ent), nil
}

func (p *WizPlugin) OnCommand(req types.Command, entity types.Entity) (types.Entity, error) {
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
		// Map error to sync status
		entity.Data.SyncStatus = types.SyncStatusFailed
		errorMap := map[string]string{"error": mapErrorToMessage(err)}
		if rawErr, err := json.Marshal(errorMap); err == nil {
			entity.Data.Reported = rawErr
		}
		return entity, err
	}

	if p.rawStore != nil {
		if sentRaw, err := json.Marshal(params); err == nil {
			_ = p.rawStore.WriteRawEntity(entity.DeviceID, entity.ID, sentRaw)
		}
	}
	store := light.Bind(&entity)
	if err := store.SetDesiredFromCommand(lc); err != nil {
		return entity, err
	}
	entity.Data.SyncStatus = types.SyncStatusPending

	evt := light.Event{Type: lc.Type, Cause: "wiz_ack", AvailableActions: entity.Actions}
	evt.Brightness = lc.Brightness
	evt.RGB = lc.RGB
	evt.Temperature = lc.Temperature
	evt.Scene = lc.Scene
	switch lc.Type {
	case light.ActionSetRGB:
		mode := light.ColorModeRGB
		evt.ColorMode = &mode
	case light.ActionSetTemperature:
		mode := light.ColorModeTemperature
		evt.ColorMode = &mode
	case light.ActionSetScene:
		mode := light.ColorModeScene
		evt.ColorMode = &mode
	}
	payload, _ := json.Marshal(evt)
	if p.eventSink != nil {
		deviceID := entity.DeviceID
		entityID := entity.ID
		correlationID := req.ID
		go func() {
			time.Sleep(20 * time.Millisecond)
			_ = p.eventSink.EmitEvent(types.InboundEvent{
				DeviceID:      deviceID,
				EntityID:      entityID,
				CorrelationID: correlationID,
				Payload:       json.RawMessage(payload),
			})
		}()
	}
	return entity, nil
}

func (p *WizPlugin) OnEvent(evt types.Event, entity types.Entity) (types.Entity, error) {
	if entity.Domain != light.Type {
		return entity, fmt.Errorf("unsupported domain: %s", entity.Domain)
	}
	le := light.Event{}
	if err := json.Unmarshal(evt.Payload, &le); err != nil {
		return entity, err
	}
	if err := light.ValidateEvent(le); err != nil {
		return entity, err
	}
	if len(le.AvailableActions) > 0 {
		entity.Actions = append([]string(nil), le.AvailableActions...)
	}
	store := light.Bind(&entity)
	if err := store.SetReportedFromEvent(le); err != nil {
		return entity, err
	}
	entity.Data.SyncStatus = types.SyncStatusSynced
	return entity, nil
}

func (p *WizPlugin) triggerDiscovery() {
	if !atomic.CompareAndSwapInt32(&p.discovering, 0, 1) {
		return
	}
	go func() {
		defer atomic.StoreInt32(&p.discovering, 0)
		p.discover()
		// Signal discovery complete (for discovery mode)
		select {
		case <-p.discoveryDone:
			// already closed
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

func (p *WizPlugin) upsertBulb(ip, mac string) {
	if mac == "" || ip == "" {
		return
	}
	n := normalizeMAC(mac)

	// Write individual device raw data
	deviceID := deviceIDForMAC(n)
	if p.rawStore != nil {
		b := WizBulb{
			MAC:             n,
			IP:              ip,
			SupportsDimming: true,
			SupportsTemp:    true,
			SupportsRGB:     true,
			SupportsScene:   true,
		}
		if raw, err := json.Marshal(b); err == nil {
			_ = p.rawStore.WriteRawDevice(deviceID, raw)
		}
	}
}

func (p *WizPlugin) findByDeviceID(deviceID string) (WizBulb, bool) {
	if p.rawStore == nil {
		return WizBulb{}, false
	}

	// Try to read from individual device raw store
	raw, err := p.rawStore.ReadRawDevice(deviceID)
	if err != nil || len(raw) == 0 {
		return WizBulb{}, false
	}

	var b WizBulb
	if err := json.Unmarshal(raw, &b); err != nil {
		return WizBulb{}, false
	}
	return b, true
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
func entityIDForMAC(mac string) string { return "light-" + normalizeMAC(mac) }

func shortMAC(mac string) string {
	n := normalizeMAC(mac)
	if len(n) <= 6 {
		return strings.ToUpper(n)
	}
	return strings.ToUpper(n[len(n)-6:])
}

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
