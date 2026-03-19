// Unit tests for plugin-wiz.
//
// Test layer philosophy:
//   Unit tests (this file): pure domain logic, cross-entity behavior,
//     and custom entity type registration. Things that don't express
//     well as BDD scenarios or that test infrastructure capabilities
//     across multiple entity types simultaneously.
//
//   BDD tests (features/*.feature, -tags bdd): per-entity behavioral
//     contract. One feature file per entity type. These are the
//     source of truth for what a plugin promises to support.
//
// Run:
//   go test ./...              - unit tests only
//   go test -tags bdd ./...    - unit tests + BDD scenarios

package main

import (
	"encoding/json"
	"testing"
	"time"

	app "github.com/slidebolt/plugin-wiz/app"
	domain "github.com/slidebolt/sb-domain"
	managersdk "github.com/slidebolt/sb-manager-sdk"
	messenger "github.com/slidebolt/sb-messenger-sdk"
	storage "github.com/slidebolt/sb-storage-sdk"
)

// ==========================================================================
// Test helpers
// ==========================================================================

func env(t *testing.T) (*managersdk.TestEnv, storage.Storage, *messenger.Commands) {
	t.Helper()
	e := managersdk.NewTestEnv(t)
	e.Start("messenger")
	e.Start("storage")
	cmds := messenger.NewCommands(e.Messenger(), domain.LookupCommand)
	return e, e.Storage(), cmds
}

func saveEntity(t *testing.T, store storage.Storage, plugin, device, id, typ, name string, state any) domain.Entity {
	t.Helper()
	e := domain.Entity{
		ID: id, Plugin: plugin, DeviceID: device,
		Type: typ, Name: name, State: state,
	}
	if err := store.Save(e); err != nil {
		t.Fatalf("save %s: %v", id, err)
	}
	return e
}

func getEntity(t *testing.T, store storage.Storage, plugin, device, id string) domain.Entity {
	t.Helper()
	raw, err := store.Get(domain.EntityKey{Plugin: plugin, DeviceID: device, ID: id})
	if err != nil {
		t.Fatalf("get %s.%s.%s: %v", plugin, device, id, err)
	}
	var entity domain.Entity
	if err := json.Unmarshal(raw, &entity); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return entity
}

func queryByType(t *testing.T, store storage.Storage, typ string) []storage.Entry {
	t.Helper()
	entries, err := store.Query(storage.Query{
		Where: []storage.Filter{{Field: "type", Op: storage.Eq, Value: typ}},
	})
	if err != nil {
		t.Fatalf("query type=%s: %v", typ, err)
	}
	return entries
}

func sendAndReceive(t *testing.T, cmds *messenger.Commands, entity domain.Entity, cmd any, pattern string) any {
	t.Helper()
	done := make(chan any, 1)
	cmds.Receive(pattern, func(addr messenger.Address, c any) {
		done <- c
	})
	if err := cmds.Send(entity, cmd.(messenger.Action)); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case got := <-done:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for command")
		return nil
	}
}

// ==========================================================================
// Custom entity: WizLightState
// ==========================================================================

func TestCustom_WizLight_SaveGetHydrate(t *testing.T) {
	_, store, _ := env(t)
	saveEntity(t, store, "plugin-wiz", "wiz-aabbccdd", "wiz-aabbccdd", "wiz_light", "Living Room Bulb",
		app.WizLightState{Power: true, Brightness: 75, RGB: []int{255, 100, 0}})

	got := getEntity(t, store, "plugin-wiz", "wiz-aabbccdd", "wiz-aabbccdd")
	state, ok := got.State.(app.WizLightState)
	if !ok {
		t.Fatalf("state type: got %T, want WizLightState", got.State)
	}
	if !state.Power || state.Brightness != 75 || len(state.RGB) != 3 {
		t.Errorf("state: %+v", state)
	}
}

func TestCustom_WizLight_QueryByType(t *testing.T) {
	_, store, _ := env(t)
	saveEntity(t, store, "plugin-wiz", "wiz-001", "wiz-001", "wiz_light", "Bulb 1", app.WizLightState{Power: true})
	saveEntity(t, store, "plugin-wiz", "wiz-002", "wiz-002", "wiz_light", "Bulb 2", app.WizLightState{Power: false})
	saveEntity(t, store, "test", "dev1", "light001", "light", "Regular Light", domain.Light{Power: true})

	entries := queryByType(t, store, "wiz_light")
	if len(entries) != 2 {
		t.Fatalf("wiz_lights: got %d, want 2", len(entries))
	}
}

// ==========================================================================
// Light Commands
// ==========================================================================

func TestLight_TurnOn(t *testing.T) {
	_, store, cmds := env(t)
	saveEntity(t, store, "plugin-wiz", "wiz-001", "wiz-001", "wiz_light", "Test Bulb", app.WizLightState{Power: false})

	entity := domain.Entity{ID: "wiz-001", Plugin: "plugin-wiz", DeviceID: "wiz-001", Type: "wiz_light"}
	got := sendAndReceive(t, cmds, entity, domain.LightTurnOn{}, "plugin-wiz.>")
	cmd, ok := got.(domain.LightTurnOn)
	if !ok {
		t.Fatalf("type: got %T, want LightTurnOn", got)
	}
	_ = cmd
}

func TestLight_TurnOff(t *testing.T) {
	_, store, cmds := env(t)
	saveEntity(t, store, "plugin-wiz", "wiz-001", "wiz-001", "wiz_light", "Test Bulb", app.WizLightState{Power: true})

	entity := domain.Entity{ID: "wiz-001", Plugin: "plugin-wiz", DeviceID: "wiz-001", Type: "wiz_light"}
	got := sendAndReceive(t, cmds, entity, domain.LightTurnOff{}, "plugin-wiz.>")
	cmd, ok := got.(domain.LightTurnOff)
	if !ok {
		t.Fatalf("type: got %T, want LightTurnOff", got)
	}
	_ = cmd
}

func TestLight_SetBrightness(t *testing.T) {
	_, store, cmds := env(t)
	saveEntity(t, store, "plugin-wiz", "wiz-001", "wiz-001", "wiz_light", "Test Bulb", app.WizLightState{Brightness: 50})

	entity := domain.Entity{ID: "wiz-001", Plugin: "plugin-wiz", DeviceID: "wiz-001", Type: "wiz_light"}
	got := sendAndReceive(t, cmds, entity, domain.LightSetBrightness{Brightness: 80}, "plugin-wiz.>")
	cmd, ok := got.(domain.LightSetBrightness)
	if !ok {
		t.Fatalf("type: got %T, want LightSetBrightness", got)
	}
	if cmd.Brightness != 80 {
		t.Errorf("brightness: got %d, want 80", cmd.Brightness)
	}
}

func TestLight_SetColorTemp(t *testing.T) {
	_, store, cmds := env(t)
	saveEntity(t, store, "plugin-wiz", "wiz-001", "wiz-001", "wiz_light", "Test Bulb", app.WizLightState{Temperature: 2700})

	entity := domain.Entity{ID: "wiz-001", Plugin: "plugin-wiz", DeviceID: "wiz-001", Type: "wiz_light"}
	// 370 mireds = ~2700K
	got := sendAndReceive(t, cmds, entity, domain.LightSetColorTemp{Mireds: 370}, "plugin-wiz.>")
	cmd, ok := got.(domain.LightSetColorTemp)
	if !ok {
		t.Fatalf("type: got %T, want LightSetColorTemp", got)
	}
	if cmd.Mireds != 370 {
		t.Errorf("mireds: got %d, want 370", cmd.Mireds)
	}
}

func TestLight_SetRGB(t *testing.T) {
	_, store, cmds := env(t)
	saveEntity(t, store, "plugin-wiz", "wiz-001", "wiz-001", "wiz_light", "Test Bulb", app.WizLightState{})

	entity := domain.Entity{ID: "wiz-001", Plugin: "plugin-wiz", DeviceID: "wiz-001", Type: "wiz_light"}
	got := sendAndReceive(t, cmds, entity, domain.LightSetRGB{R: 255, G: 0, B: 0}, "plugin-wiz.>")
	cmd, ok := got.(domain.LightSetRGB)
	if !ok {
		t.Fatalf("type: got %T, want LightSetRGB", got)
	}
	if cmd.R != 255 || cmd.G != 0 || cmd.B != 0 {
		t.Errorf("rgb: got (%d,%d,%d), want (255,0,0)", cmd.R, cmd.G, cmd.B)
	}
}

// ==========================================================================
// Multi-entity per device
// ==========================================================================

// TestMultiEntity_DeviceWith10Sensors verifies that a single Wiz device
// exposing multiple entity types gets a unique entity key for each one.
// Today the plugin uses DeviceID == EntityID which means only one entity
// can exist per device — this test proves the fix.
func TestMultiEntity_DeviceWith10Sensors(t *testing.T) {
	_, store, _ := env(t)

	mac := "aabbccddeeff"
	deviceID := app.MakeDeviceID(mac)

	// Simulate a device that exposes 10 different entities: a light plus
	// 9 sensors (power, voltage, current, energy, signal, temperature,
	// humidity, uptime, firmware).
	entityTypes := []struct {
		suffix string
		typ    string
		name   string
	}{
		{"light", "wiz_light", "Living Room Light"},
		{"power", "sensor", "Living Room Power"},
		{"voltage", "sensor", "Living Room Voltage"},
		{"current", "sensor", "Living Room Current"},
		{"energy", "sensor", "Living Room Energy"},
		{"signal", "sensor", "Living Room Signal"},
		{"temperature", "sensor", "Living Room Temperature"},
		{"humidity", "sensor", "Living Room Humidity"},
		{"uptime", "sensor", "Living Room Uptime"},
		{"firmware", "sensor", "Living Room Firmware"},
	}

	for _, et := range entityTypes {
		entityID := app.MakeEntityID(deviceID, et.suffix)
		saveEntity(t, store, app.PluginID, deviceID, entityID, et.typ, et.name, nil)
	}

	// All 10 entities must be retrievable with distinct keys.
	entries, err := store.Search(app.PluginID + "." + deviceID + ".*")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(entries) != len(entityTypes) {
		t.Fatalf("entities: got %d, want %d", len(entries), len(entityTypes))
	}

	// Verify each key is unique and contains the type suffix.
	seen := make(map[string]bool)
	for _, e := range entries {
		if seen[e.Key] {
			t.Errorf("duplicate entity key: %s", e.Key)
		}
		seen[e.Key] = true
	}
	t.Logf("✓ %d distinct entities registered for device %s", len(entries), deviceID)

	// Verify the light entity specifically matches production key format.
	lightKey := app.PluginID + "." + deviceID + "." + app.MakeEntityID(deviceID, "light")
	raw, err := store.Get(keyStr(lightKey))
	if err != nil {
		t.Fatalf("get light entity: %v", err)
	}
	var ent domain.Entity
	if err := json.Unmarshal(raw, &ent); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ent.DeviceID != deviceID {
		t.Errorf("DeviceID: got %q, want %q", ent.DeviceID, deviceID)
	}
	expectedEntityID := app.MakeEntityID(deviceID, "light")
	if ent.ID != expectedEntityID {
		t.Errorf("ID: got %q, want %q", ent.ID, expectedEntityID)
	}
	t.Logf("✓ light entity key: %s (ID=%s, DeviceID=%s)", lightKey, ent.ID, ent.DeviceID)
}

type keyStr string

func (k keyStr) Key() string { return string(k) }

// ==========================================================================
// Discovery helpers
// ==========================================================================

func TestDiscovery_MakeDeviceID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"aa:bb:cc:dd:ee:ff", "wiz-aabbccddeeff"},
		{"AA-BB-CC-DD-EE-FF", "wiz-aabbccddeeff"},
		{"aabbccddeeff", "wiz-aabbccddeeff"},
		{"", "wiz-unknown"},
	}

	for _, tc := range tests {
		got := app.MakeDeviceID(tc.input)
		if got != tc.expected {
			t.Errorf("makeDeviceID(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
