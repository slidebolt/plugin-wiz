package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/slidebolt/sdk-entities/light"
	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

type wizBulb struct {
	MAC             string `json:"mac"`
	IP              string `json:"ip"`
	SupportsRGB     bool   `json:"supports_rgb"`
	SupportsTemp    bool   `json:"supports_temp"`
	SupportsScene   bool   `json:"supports_scene"`
	SupportsDimming bool   `json:"supports_dimming"`
}

type WizPlugin struct {
	client     WizClient
	eventSink  runner.EventSink
	rawStore   runner.RawStore
	stopListen func()

	mu    sync.RWMutex
	bulbs map[string]wizBulb

	discoveryMu   sync.Mutex
	lastDiscovery time.Time
	discovering   int32
}

func NewWizPlugin(client WizClient) *WizPlugin {
	if client == nil {
		client = &RealWizClient{}
	}
	return &WizPlugin{client: client, bulbs: map[string]wizBulb{}}
}

func (p *WizPlugin) OnInitialize(config runner.Config, state types.Storage) (types.Manifest, types.Storage) {
	p.eventSink = config.EventSink
	p.rawStore = config.RawStore
	if p.bulbs == nil {
		p.bulbs = make(map[string]wizBulb)
	}
	log.Printf("plugin-wiz initializing")
	return types.Manifest{ID: "plugin-wiz", Name: "Wiz Plugin", Version: "1.0.0"}, state
}

func (p *WizPlugin) OnReady() {
	stop, err := p.client.Listen(func(ip string, result WizSystemConfig) {
		p.upsertBulb(ip, result.Mac)
	})
	if err != nil {
		log.Printf("plugin-wiz listen failed: %v", err)
		return
	}
	p.stopListen = stop
	p.triggerDiscovery()
}

func (p *WizPlugin) WaitReady(ctx context.Context) error {
	return nil
}

func (p *WizPlugin) OnShutdown() {
	if p.stopListen != nil {
		p.stopListen()
	}
}

func (p *WizPlugin) OnHealthCheck() (string, error) { return "perfect", nil }

func (p *WizPlugin) OnStorageUpdate(current types.Storage) (types.Storage, error) {
	return current, nil
}

func (p *WizPlugin) OnDeviceCreate(dev types.Device) (types.Device, error) { return dev, nil }
func (p *WizPlugin) OnDeviceUpdate(dev types.Device) (types.Device, error) { return dev, nil }
func (p *WizPlugin) OnDeviceDelete(id string) error                        { return nil }

func (p *WizPlugin) OnDevicesList(current []types.Device) ([]types.Device, error) {
	p.triggerDiscovery()

	if p.rawStore != nil {
		for _, dev := range current {
			mac := normalizeMAC(dev.SourceID)
			p.mu.RLock()
			_, known := p.bulbs[mac]
			p.mu.RUnlock()
			if known {
				continue
			}
			raw, err := p.rawStore.ReadRawDevice(dev.ID)
			if err != nil || len(raw) == 0 {
				continue
			}
			var b wizBulb
			if err := json.Unmarshal(raw, &b); err == nil && b.MAC != "" {
				p.mu.Lock()
				p.bulbs[b.MAC] = b
				p.mu.Unlock()
			}
		}
	}

	existing := map[string]types.Device{}
	for _, d := range current {
		existing[d.ID] = d
	}
	for _, bulb := range p.snapshotBulbs() {
		id := deviceIDForMAC(bulb.MAC)
		discoveredDev := types.Device{
			ID:         id,
			SourceID:   bulb.MAC,
			SourceName: "WiZ Light",
		}
		if existingDev, ok := existing[id]; ok {
			existing[id] = runner.ReconcileDevice(existingDev, discoveredDev)
		} else {
			existing[id] = runner.ReconcileDevice(types.Device{}, discoveredDev)
		}
	}
	out := make([]types.Device, 0, len(existing))
	for _, d := range existing {
		out = append(out, d)
	}
	return out, nil
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

func (p *WizPlugin) OnEntitiesList(deviceID string, current []types.Entity) ([]types.Entity, error) {
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

func (p *WizPlugin) OnCommandTyped(req types.CommandRequest[types.GenericPayload], entity types.Entity) (types.Entity, error) {
	if entity.Domain != light.Type {
		return entity, fmt.Errorf("unsupported domain: %s", entity.Domain)
	}
	bulb, ok := p.findByDeviceID(entity.DeviceID)
	if !ok {
		return entity, fmt.Errorf("wiz bulb not found for device %s", entity.DeviceID)
	}
	lc := light.Command{}
	if err := decodeGenericPayload(req.Payload, &lc); err != nil {
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
	entity.Data.SyncStatus = "pending"

	evt := light.Event{Type: lc.Type, Cause: "wiz_ack", AvailableActions: entity.Actions}
	evt.Brightness = lc.Brightness
	evt.RGB = lc.RGB
	evt.Temperature = lc.Temperature
	evt.Scene = lc.Scene
	payload, _ := json.Marshal(evt)
	if p.eventSink != nil {
		deviceID := entity.DeviceID
		entityID := entity.ID
		correlationID := req.CommandID
		go func() {
			time.Sleep(20 * time.Millisecond)
			_ = p.eventSink.EmitTypedEvent(types.InboundEventTyped[types.GenericPayload]{
				DeviceID:      deviceID,
				EntityID:      entityID,
				CorrelationID: correlationID,
				Payload:       rawToGeneric(payload),
			})
		}()
	}
	return entity, nil
}

func (p *WizPlugin) OnEventTyped(evt types.EventTyped[types.GenericPayload], entity types.Entity) (types.Entity, error) {
	if entity.Domain != light.Type {
		return entity, fmt.Errorf("unsupported domain: %s", entity.Domain)
	}
	le := light.Event{}
	if err := decodeGenericPayload(evt.Payload, &le); err != nil {
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
	entity.Data.SyncStatus = "in_sync"
	return entity, nil
}

func decodeGenericPayload(payload types.GenericPayload, out any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func rawToGeneric(raw []byte) types.GenericPayload {
	out := types.GenericPayload{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func (p *WizPlugin) triggerDiscovery() {
	if !atomic.CompareAndSwapInt32(&p.discovering, 0, 1) {
		return
	}
	go func() {
		defer atomic.StoreInt32(&p.discovering, 0)
		p.discover()
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
		hosts = localSubnetCandidates()
	}
	probeHostsConcurrently(hosts, 64, func(host string) {
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
	p.mu.Lock()
	defer p.mu.Unlock()
	b := p.bulbs[n]
	b.MAC = n
	b.IP = ip
	if !b.SupportsDimming {
		b.SupportsDimming = true
	}
	if !b.SupportsTemp {
		b.SupportsTemp = true
	}
	if !b.SupportsRGB {
		b.SupportsRGB = true
	}
	if !b.SupportsScene {
		b.SupportsScene = true
	}
	p.bulbs[n] = b

	deviceID := deviceIDForMAC(n)
	if raw, err := json.Marshal(b); err == nil {
		if p.rawStore != nil {
			_ = p.rawStore.WriteRawDevice(deviceID, raw)
		}
	}
}

func (p *WizPlugin) snapshotBulbs() []wizBulb {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]wizBulb, 0, len(p.bulbs))
	for _, b := range p.bulbs {
		out = append(out, b)
	}
	return out
}

func (p *WizPlugin) findByDeviceID(deviceID string) (wizBulb, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, b := range p.bulbs {
		if deviceIDForMAC(b.MAC) == deviceID {
			return b, true
		}
	}
	return wizBulb{}, false
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

func bulbActions(b wizBulb) []string {
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

func localSubnetCandidates() []string {
	set := map[string]struct{}{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			if !ip4.IsPrivate() || ip4.IsLoopback() {
				continue
			}
			for i := 1; i <= 254; i++ {
				host := fmt.Sprintf("%d.%d.%d.%d", ip4[0], ip4[1], ip4[2], i)
				set[host] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for host := range set {
		out = append(out, host)
	}
	return out
}

func probeHostsConcurrently(hosts []string, workers int, fn func(host string)) {
	if workers <= 0 {
		workers = 1
	}
	var idx int64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				j := int(atomic.AddInt64(&idx, 1) - 1)
				if j >= len(hosts) {
					return
				}
				fn(hosts[j])
			}
		}()
	}
	wg.Wait()
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
