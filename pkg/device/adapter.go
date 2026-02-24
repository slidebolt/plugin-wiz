package device

import (
	"fmt"
	"github.com/slidebolt/plugin-sdk"
	 "github.com/slidebolt/plugin-wiz/pkg/logic"
	"strings"
	"sync"
)

type wizAdapter struct {
	bundle  sdk.Bundle
	client  logic.WizClient
	ip      string
	device  sdk.Device
	light   sdk.Entity
	mac     string
	started bool
	mu      sync.Mutex
}

var (
	adaptersMu sync.Mutex
	adapters   = map[string]*wizAdapter{}
)

func Register(b sdk.Bundle, client logic.WizClient, ip string, mac string) {
	sid := normalizeMAC(mac)
	if sid == "" {
		return
	}

	adaptersMu.Lock()
	defer adaptersMu.Unlock()

	key := fmt.Sprintf("%s/%s", b.ID(), sid)
	if a, ok := adapters[key]; ok {
		a.mu.Lock()
		a.ip = ip
		a.mu.Unlock()
		_ = a.device.UpdateRaw(map[string]interface{}{"ip": ip, "mac": sid})
		// Do not call bindCommands again as it appends to handlers
		a.refreshState()
		return
	}

	var dev sdk.Device
	if obj, ok := b.GetBySourceID(sdk.SourceID(sid)); ok {
		dev = obj.(sdk.Device)
	} else {
		created, err := b.CreateDevice()
		if err != nil {
			return
		}
		dev = created
		_ = dev.UpdateMetadata("Wiz Light", sdk.SourceID(sid))
	}
	_ = dev.UpdateRaw(map[string]interface{}{"ip": ip, "mac": sid})

	var light sdk.Entity
	if ents, err := dev.GetEntities(); err == nil && len(ents) > 0 {
		light = ents[0]
	} else {
		caps := []string{sdk.CAP_BRIGHTNESS, sdk.CAP_TEMPERATURE, sdk.CAP_RGB, sdk.CAP_SCENE}
		created, err := dev.CreateEntityEx(sdk.TYPE_LIGHT, caps)
		if err != nil {
			return
		}
		light = created
		_ = light.UpdateMetadata("Light", sdk.SourceID(fmt.Sprintf("%s-light", sid)))
	}

	a := &wizAdapter{bundle: b, client: client, ip: ip, device: dev, light: light, mac: sid}
	adapters[key] = a
	a.bindCommands()
	a.refreshState()
	a.startPolling()

	b.Log().Info("Registered Wiz Light at %s (%s)", ip, sid)
}

func (a *wizAdapter) bindCommands() {
	handler := func(cmd string, payload map[string]interface{}) {
		a.bundle.Log().Info("Wiz command received [%s] %s: %v", a.mac, cmd, payload)
		params := make(map[string]interface{})
		state := make(map[string]interface{})
		switch cmd {
		case "TurnOn", "ToggleOn":
			params["state"] = true
			state["power"] = true
		case "TurnOff", "ToggleOff":
			params["state"] = false
			state["power"] = false
		case "SetBrightness":
			if val, ok := toInt(payload["level"]); ok {
				params["dimming"] = val
				params["state"] = true
				state["brightness"] = val
				state["power"] = true
			}
		case "SetRGB":
			r, rok := toInt(payload["r"])
			g, gok := toInt(payload["g"])
			b, bok := toInt(payload["b"])
			if rok && gok && bok {
				params["r"] = r
				params["g"] = g
				params["b"] = b
				params["state"] = true
				state["r"] = r
				state["g"] = g
				state["b"] = b
				state["power"] = true
			}
		case "SetTemperature":
			if val, ok := toInt(payload["kelvin"]); ok {
				params["temp"] = val
				params["state"] = true
				state["kelvin"] = val
				state["temperature"] = val
				state["power"] = true
			}
		case "SetScene":
			if scene, ok := payload["scene"].(string); ok {
				if id, ok := toInt(payload["scene_id"]); ok {
					params["sceneId"] = id
				} else {
					params["sceneId"] = scene
				}
				params["state"] = true
				state["scene"] = scene
				state["power"] = true
			}
		}

		if len(params) == 0 {
			return
		}

		a.mu.Lock()
		ip := a.ip
		mac := a.mac
		a.mu.Unlock()
		a.bundle.Log().Info("Wiz sending command to bulb [%s] at %s: %s", mac, ip, cmd)
		if err := a.client.SetPilot(ip, mac, params); err != nil {
			a.bundle.Log().Error("Wiz command failed [%s] %s: %v", a.mac, cmd, err)
			return
		}

		a.publishState(state)
		// Get authoritative state after command write.
		a.refreshState()
	}

	a.device.OnCommand(handler)
	a.light.OnCommand(handler)
}

func (a *wizAdapter) startPolling() {
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return
	}
	a.started = true
	a.mu.Unlock()

	a.bundle.Every15Seconds(func() {
		a.refreshState()
	})
}

func (a *wizAdapter) refreshState() {
	a.mu.Lock()
	ip := a.ip
	a.mu.Unlock()

	pilot, err := a.client.GetPilot(ip)
	if err != nil {
		a.bundle.Log().Debug("Wiz getPilot failed [%s]: %v", a.mac, err)
		return
	}

	state := map[string]interface{}{}
	if v, ok := pilot["state"].(bool); ok {
		state["power"] = v
	}
	if v, ok := toInt(pilot["dimming"]); ok {
		state["brightness"] = v
	}
	if v, ok := toInt(pilot["temp"]); ok {
		state["kelvin"] = v
		state["temperature"] = v
	}
	if r, rok := toInt(pilot["r"]); rok {
		if g, gok := toInt(pilot["g"]); gok {
			if b, bok := toInt(pilot["b"]); bok {
				state["r"] = r
				state["g"] = g
				state["b"] = b
			}
		}
	}
	if v, ok := toInt(pilot["sceneId"]); ok {
		state["scene_id"] = v
	}

	a.publishState(state)
}

func (a *wizAdapter) publishState(state map[string]interface{}) {
	if len(state) == 0 {
		return
	}

	// This method now handles both persistence and bus publishing in one clean contract.
	_ = a.light.UpdateProperties(state)
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case string:
		var i int
		_, err := fmt.Sscanf(n, "%d", &i)
		return i, err == nil
	default:
		return 0, false
	}
}

func normalizeMAC(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, ":", "")
	v = strings.ReplaceAll(v, "-", "")
	return v
}

func hasCustomScript(ent sdk.Entity) bool {
	s := strings.TrimSpace(ent.Script())
	return s != "" && s != "-- OnLoad() {}"
}
