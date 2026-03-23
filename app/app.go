package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	wiz "github.com/slidebolt/plugin-wiz/internal/wiz"
	contract "github.com/slidebolt/sb-contract"
	domain "github.com/slidebolt/sb-domain"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

const PluginID = "plugin-wiz"


type App struct {
	msg    messenger.Messenger
	store  storage.Storage
	cmds   *messenger.Commands
	subs   []messenger.Subscription
	ctx    context.Context
	cancel context.CancelFunc
	ticker *time.Ticker

	mu      sync.RWMutex
	devices []wiz.Device
	seen    map[string]struct{}
}

func New() *App                      { return &App{} }
func (a *App) Devices() []wiz.Device { return a.devices }

func (a *App) Hello() contract.HelloResponse {
	return contract.HelloResponse{ID: PluginID, Kind: contract.KindPlugin, ContractVersion: contract.ContractVersion, DependsOn: []string{"messenger", "storage"}}
}

func (a *App) OnStart(deps map[string]json.RawMessage) (json.RawMessage, error) {
	msg, err := messenger.Connect(deps)
	if err != nil {
		return nil, fmt.Errorf("connect messenger: %w", err)
	}
	a.msg = msg
	storeClient, err := storage.Connect(deps)
	if err != nil {
		return nil, fmt.Errorf("connect storage: %w", err)
	}
	a.store = storeClient
	a.seen = make(map[string]struct{})
	a.cmds = messenger.NewCommands(msg, domain.LookupCommand)
	sub, err := a.cmds.Receive(PluginID+".>", a.handleCommand)
	if err != nil {
		return nil, fmt.Errorf("subscribe commands: %w", err)
	}
	a.subs = append(a.subs, sub)
	if err := a.discoverAndRegister(); err != nil {
		log.Printf("plugin-wiz: discovery error: %v", err)
	}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.ticker = time.NewTicker(30 * time.Second)
	go a.discoveryLoop()
	log.Println("plugin-wiz: started")
	return nil, nil
}

func (a *App) OnShutdown() error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.ticker != nil {
		a.ticker.Stop()
	}
	for _, sub := range a.subs {
		sub.Unsubscribe()
	}
	if a.store != nil {
		a.store.Close()
	}
	if a.msg != nil {
		a.msg.Close()
	}
	return nil
}

func (a *App) discoveryLoop() {
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-a.ticker.C:
			if err := a.discoverAndRegister(); err != nil {
				log.Printf("plugin-wiz: discovery refresh error: %v", err)
			}
		}
	}
}

func (a *App) discoverAndRegister() error {
	subnet := os.Getenv("WIZ_SUBNET")
	if subnet == "" {
		subnet = "192.168.88"
	}
	timeout := 500 * time.Millisecond
	if t := os.Getenv("WIZ_TIMEOUT_MS"); t != "" {
		if ms, err := strconv.Atoi(t); err == nil {
			timeout = time.Duration(ms) * time.Millisecond
		}
	}
	devices, err := wiz.DiscoverDevices(subnet, timeout)
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.devices = devices
	a.mu.Unlock()
	for _, dev := range devices {
		deviceID := MakeDeviceID(dev.Mac)
		a.mu.Lock()
		_, alreadySeen := a.seen[deviceID]
		a.seen[deviceID] = struct{}{}
		a.mu.Unlock()

		state := domain.Light{Power: false, Brightness: 100}
		var sceneID int
		if dev.GetPilot != nil {
			state = lightStateFromPilot(dev.GetPilot)
			if sid, ok := dev.GetPilot["sceneId"].(float64); ok {
				sceneID = int(sid)
			}
		}
		entityID := MakeEntityID(deviceID, "light")
		meta := map[string]json.RawMessage{
			"ip":  json.RawMessage(fmt.Sprintf(`"%s"`, dev.IP)),
			"mac": json.RawMessage(fmt.Sprintf(`"%s"`, dev.Mac)),
		}
		if sceneID > 0 {
			meta["scene_id"] = json.RawMessage(fmt.Sprintf(`%d`, sceneID))
		}
		entity := domain.Entity{
			ID: entityID, Plugin: PluginID, DeviceID: deviceID, Type: "light",
			Name:     fmt.Sprintf("WiZ %s", dev.Mac[len(dev.Mac)-6:]),
			Commands: []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp", "light_set_rgb"},
			State:    state,
			Meta:     meta,
		}
		if err := a.store.Save(entity); err != nil {
			log.Printf("plugin-wiz: failed to save entity %s: %v", deviceID, err)
		} else if !alreadySeen {
			log.Printf("plugin-wiz: registered %s at %s", deviceID, dev.IP)
		}
	}
	return nil
}

func MakeDeviceID(mac string) string {
	cleaned := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(mac, ":", ""), "-", ""))
	if cleaned == "" {
		cleaned = "unknown"
	}
	return "wiz-" + cleaned
}

func MakeEntityID(deviceID, suffix string) string {
	mac := strings.TrimPrefix(deviceID, "wiz-")
	return suffix + "-" + mac
}

func (a *App) getDeviceIP(deviceID string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, dev := range a.devices {
		if MakeDeviceID(dev.Mac) == deviceID {
			return dev.IP
		}
	}
	return ""
}

func (a *App) handleCommand(addr messenger.Address, cmd any) {
	ip := a.getDeviceIP(addr.DeviceID)
	if ip == "" {
		log.Printf("plugin-wiz: no IP for device %s", addr.DeviceID)
		return
	}
	entityKey := domain.EntityKey{Plugin: addr.Plugin, DeviceID: addr.DeviceID, ID: addr.EntityID}
	raw, err := a.store.Get(entityKey)
	if err != nil {
		log.Printf("plugin-wiz: command for unknown entity %s: %v", addr.Key(), err)
		return
	}
	var entity domain.Entity
	if err := json.Unmarshal(raw, &entity); err != nil {
		log.Printf("plugin-wiz: failed to parse entity %s: %v", addr.Key(), err)
		return
	}
	switch c := cmd.(type) {
	case domain.LightTurnOn:
		if err := wiz.SetPilot(ip, map[string]any{"state": true}); err == nil {
			a.updateLightState(entity, func(s *domain.Light) { s.Power = true })
		}
	case domain.LightTurnOff:
		if err := wiz.SetPilot(ip, map[string]any{"state": false}); err == nil {
			a.updateLightState(entity, func(s *domain.Light) { s.Power = false })
		}
	case domain.LightSetBrightness:
		if err := wiz.SetPilot(ip, map[string]any{"state": true, "dimming": c.Brightness}); err == nil {
			a.updateLightState(entity, func(s *domain.Light) { s.Power = true; s.Brightness = c.Brightness })
		}
	case domain.LightSetColorTemp:
		kelvin := 1000000 / c.Mireds
		payload := map[string]any{"state": true, "temp": kelvin}
		if c.Brightness > 0 {
			payload["dimming"] = c.Brightness
		}
		if err := wiz.SetPilot(ip, payload); err == nil {
			a.updateLightState(entity, func(s *domain.Light) {
				s.Power = true
				s.Temperature = kelvin
				s.ColorMode = "color_temp"
				s.RGB = nil
				if c.Brightness > 0 {
					s.Brightness = c.Brightness
				}
			})
		}
	case domain.LightSetRGB:
		payload := map[string]any{"state": true, "r": c.R, "g": c.G, "b": c.B}
		if c.Brightness > 0 {
			payload["dimming"] = c.Brightness
		}
		if err := wiz.SetPilot(ip, payload); err == nil {
			a.updateLightState(entity, func(s *domain.Light) {
				s.Power = true
				s.RGB = []int{c.R, c.G, c.B}
				s.ColorMode = "rgb"
				if c.Brightness > 0 {
					s.Brightness = c.Brightness
				}
			})
		}
	default:
		log.Printf("plugin-wiz: unknown command %T for %s", cmd, addr.Key())
	}
}

func (a *App) updateLightState(entity domain.Entity, mutate func(*domain.Light)) {
	state, _ := entity.State.(domain.Light)
	mutate(&state)
	entity.State = state
	if err := a.store.Save(entity); err != nil {
		log.Printf("plugin-wiz: failed to update state for %s: %v", entity.ID, err)
	}
}

func lightStateFromPilot(pilot map[string]any) domain.Light {
	state := domain.Light{Power: false, Brightness: 100}
	if power, ok := pilot["state"].(bool); ok {
		state.Power = power
	}
	if dimming, ok := pilot["dimming"].(float64); ok {
		state.Brightness = int(dimming)
	}
	if temp, ok := pilot["temp"].(float64); ok {
		state.Temperature = int(temp)
		state.ColorMode = "color_temp"
	}
	if r, ok := pilot["r"].(float64); ok {
		if g, ok := pilot["g"].(float64); ok {
			if b, ok := pilot["b"].(float64); ok {
				state.RGB = []int{int(r), int(g), int(b)}
				state.ColorMode = "rgb"
			}
		}
	}
	return state
}
