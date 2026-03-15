//go:build local



package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
	regsvc "github.com/slidebolt/registry"
	"github.com/slidebolt/sdk-entities/light"
	"github.com/slidebolt/sdk-integration-testing"
	"github.com/slidebolt/sdk-types"
)

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestWizLocal_LightOperations boots a real stack, discovers the first WiZ
// bulb and exercises the full range of light commands:
//   - turn_off   → verify physical state is off
//   - turn_on    → verify physical state is on
//   - 10% soft white (set_temperature=3000, set_brightness=10)
//   - green 20%  (set_rgb=[0,255,0], set_brightness=20)
//   - soft white 20% (set_temperature=3000, set_brightness=20)
func TestWizLocal_LightOperations(t *testing.T) {
	s := integrationtesting.New(t, "github.com/slidebolt/plugin-wiz", ".")
	s.RequirePlugin("plugin-wiz")

	t.Log("waiting for WiZ bulbs to be discovered (up to 30s)...")
	targets := waitForWizTargets(t, s, 30*time.Second)
	if len(targets) == 0 {
		t.Skip("no WiZ bulbs discovered on this network")
	}
	tgt := targets[0]
	t.Logf("selected bulb: device=%s entity=%s ip=%s", tgt.DeviceID, tgt.EntityID, tgt.IP)

	send := func(payload map[string]any) string {
		t.Helper()
		id := postCommandAndWait(t, s, "plugin-wiz", tgt.DeviceID, tgt.EntityID, payload, 10*time.Second)
		waitForCommandState(t, s, "plugin-wiz", id, types.CommandSucceeded, 10*time.Second)
		return id
	}

	// --- turn off ---
	t.Log("=== turn off ===")
	send(map[string]any{"type": "turn_off"})
	if tgt.IP != "" {
		assertPhysicalState(t, tgt.IP, false, 8*time.Second)
	}

	// --- turn on ---
	t.Log("=== turn on ===")
	send(map[string]any{"type": "turn_on"})
	if tgt.IP != "" {
		assertPhysicalState(t, tgt.IP, true, 8*time.Second)
	}

	// --- 10% soft white ---
	t.Log("=== 10% soft white ===")
	send(map[string]any{"type": "set_temperature", "temperature": 3000})
	send(map[string]any{"type": "set_brightness", "brightness": 10})
	if tgt.IP != "" {
		assertPhysicalTemperature(t, tgt.IP, 3000, 8*time.Second)
		assertPhysicalBrightness(t, tgt.IP, 10, 8*time.Second)
	}

	// --- green 20% ---
	t.Log("=== green 20% ===")
	send(map[string]any{"type": light.ActionSetRGB, "rgb": []int{0, 255, 0}})
	send(map[string]any{"type": "set_brightness", "brightness": 20})
	if tgt.IP != "" {
		assertPhysicalPilotRGB(t, tgt.IP, 0, 255, 0, 8*time.Second)
		assertPhysicalBrightness(t, tgt.IP, 20, 8*time.Second)
	}

	// --- soft white 20% ---
	t.Log("=== soft white 20% ===")
	send(map[string]any{"type": "set_temperature", "temperature": 3000})
	send(map[string]any{"type": "set_brightness", "brightness": 20})
	if tgt.IP != "" {
		assertPhysicalTemperature(t, tgt.IP, 3000, 8*time.Second)
		assertPhysicalBrightness(t, tgt.IP, 20, 8*time.Second)
	}
}

// TestWizLocal_LabelsAndSearch discovers all WiZ devices, assigns each a
// human-readable name and labels, then proves:
//   - devices are findable by label via GET /api/search/devices
//   - entities are findable by label via GET /api/search/entities
//   - the updated state is persisted to disk in the registry file layout
func TestWizLocal_LabelsAndSearch(t *testing.T) {
	s := integrationtesting.New(t, "github.com/slidebolt/plugin-wiz", ".")
	s.RequirePlugin("plugin-wiz")

	t.Log("waiting for WiZ bulbs to be discovered (up to 30s)...")
	targets := waitForWizTargets(t, s, 30*time.Second)
	if len(targets) == 0 {
		t.Skip("no WiZ bulbs discovered on this network")
	}
	t.Logf("discovered %d WiZ device(s)", len(targets))

	for i, tgt := range targets {
		deviceName := fmt.Sprintf("WiZ Light %d", i+1)

		// Set the human-readable device name.
		t.Logf("[%d] patching device name → %q", i+1, deviceName)
		patchJSON(t, s, fmt.Sprintf("/api/plugins/plugin-wiz/devices/%s/name", tgt.DeviceID),
			map[string]any{"local_name": deviceName})

		// Set device labels: brand + ip so we can search later.
		// NOTE: the SDK runner replaces the entire labels map on update, so we
		// include the existing ip label explicitly to avoid losing it.
		t.Logf("[%d] patching device labels", i+1)
		patchJSON(t, s, fmt.Sprintf("/api/plugins/plugin-wiz/devices/%s/labels", tgt.DeviceID),
			map[string]any{"labels": map[string][]string{
				"brand": {"wiz"},
				"ip":    {tgt.IP},
			}})

		// Set entity labels on the light entity.
		if tgt.EntityID != "" {
			t.Logf("[%d] patching entity labels", i+1)
			patchJSON(t, s, fmt.Sprintf("/api/plugins/plugin-wiz/devices/%s/entities/%s/labels",
				tgt.DeviceID, tgt.EntityID),
				map[string]any{"labels": map[string][]string{
					"brand": {"wiz"},
					"room":  {"test"},
				}})
		}
	}

	// Give the registry a moment to flush writes to disk.
	time.Sleep(300 * time.Millisecond)

	// --- search by device label ---
	t.Log("searching devices by label brand:wiz...")
	devs := searchDevices(t, s, "brand:wiz")
	t.Logf("device search found %d result(s)", len(devs))
	if len(devs) != len(targets) {
		t.Errorf("expected %d devices in search results, got %d", len(targets), len(devs))
	}

	// --- search by entity label ---
	t.Log("searching entities by label brand:wiz...")
	ents := searchEntities(t, s, "brand:wiz")
	t.Logf("entity search found %d result(s)", len(ents))
	if len(ents) != len(targets) {
		t.Errorf("expected %d entities in search results, got %d", len(targets), len(ents))
	}

	t.Log("searching entities by label room:test...")
	entsByRoom := searchEntities(t, s, "room:test")
	t.Logf("room:test search found %d result(s)", len(entsByRoom))
	if len(entsByRoom) != len(targets) {
		t.Errorf("expected %d entities in room:test results, got %d", len(targets), len(entsByRoom))
	}

	// --- verify disk files ---
	dataDir := s.DataDir()
	t.Logf("checking disk files under %s", dataDir)
	for i, tgt := range targets {
		deviceName := fmt.Sprintf("WiZ Light %d", i+1)

		// Device file: {dataDir}/plugin-wiz/devices/{device-id}.json
		deviceFile := filepath.Join(dataDir, "plugin-wiz", "devices", tgt.DeviceID+".json")
		checkDiskFile(t, deviceFile, "local_name", deviceName)

		// Entity file: {dataDir}/plugin-wiz/devices/{device-id}/entities/{entity-id}.json
		if tgt.EntityID != "" {
			entityFile := filepath.Join(dataDir, "plugin-wiz", "devices", tgt.DeviceID, "entities", tgt.EntityID+".json")
			checkDiskFile(t, entityFile, "brand", "wiz")
		}
	}
}

// TestWizLocal_GroupFanout creates a plugin-wiz-owned group entity with a
// CommandQuery that matches all WiZ entities by label, then
// sends a single command to the group and verifies every physical bulb reached
// the new state.
//
// Requires all WiZ entities to already have the label brand:wiz. If they don't
// this test labels them before creating the group.
func TestWizLocal_GroupFanout(t *testing.T) {
	s := integrationtesting.New(t, "github.com/slidebolt/plugin-wiz", ".")
	s.RequirePlugin("plugin-wiz")

	t.Log("waiting for WiZ bulbs to be discovered (up to 30s)...")
	targets := waitForWizTargets(t, s, 30*time.Second)
	if len(targets) == 0 {
		t.Skip("no WiZ bulbs discovered on this network")
	}
	t.Logf("discovered %d WiZ device(s) — labelling all with brand:wiz", len(targets))

	for _, tgt := range targets {
		patchJSON(t, s, fmt.Sprintf("/api/plugins/plugin-wiz/devices/%s/entities/%s/labels",
			tgt.DeviceID, tgt.EntityID),
			map[string]any{"labels": map[string][]string{
				"brand": {"wiz"},
			}})
	}
	// Let aggregate registry propagate the label updates.
	time.Sleep(300 * time.Millisecond)

	// Create the group entity in the plugin-wiz namespace via the NATS registry.
	t.Log("creating plugin-wiz group entity wiz-all-lights via NATS registry...")
	createPluginGroupEntity(t, s, "plugin-wiz", types.Entity{
		ID:       "wiz-all-lights",
		DeviceID: "wiz-group",
		Domain:   "light",
		PluginID: "plugin-wiz",
		CommandQuery: &types.SearchQuery{
			Labels: map[string][]string{"brand": {"wiz"}},
		},
	})

	// Real WiZ bulbs on this network clamp low brightness to 10%, so target that
	// minimum physical level directly for a stable hardware assertion.
	t.Log("sending set_brightness(10) to group entity...")
	cmdID := postCommandAndWait(t, s, "plugin-wiz", "wiz-group", "wiz-all-lights",
		map[string]any{"type": "set_brightness", "brightness": 10}, 10*time.Second)
	waitForCommandState(t, s, "plugin-wiz", cmdID, types.CommandSucceeded, 15*time.Second)

	t.Log("sending set_temperature(3000) to group entity...")
	cmdID = postCommandAndWait(t, s, "plugin-wiz", "wiz-group", "wiz-all-lights",
		map[string]any{"type": "set_temperature", "temperature": 3000}, 10*time.Second)
	waitForCommandState(t, s, "plugin-wiz", cmdID, types.CommandSucceeded, 15*time.Second)

	// Verify each physical bulb reached the target state.
	t.Logf("verifying all %d physical bulbs reached 10%% soft white...", len(targets))
	for _, tgt := range targets {
		if tgt.IP == "" {
			continue
		}
		t.Logf("checking physical bulb at %s...", tgt.IP)
		assertPhysicalBrightness(t, tgt.IP, 10, 10*time.Second)
		assertPhysicalTemperature(t, tgt.IP, 3000, 10*time.Second)
	}
}

// TestWizLocal_LuaColorSync installs two Lua scripts and verifies:
//
//   - Script 1 (on light-A): when the light is turned off, the script
//     immediately turns it back on.
//   - Script 2 (on light-B): when the light color changes, the script
//     reads all other WiZ lights via QueryService and syncs the color to them.
func TestWizLocal_LuaColorSync(t *testing.T) {
	s := integrationtesting.New(t, "github.com/slidebolt/plugin-wiz", ".")
	s.RequirePlugin("plugin-wiz")

	targets := waitForWizTargets(t, s, 30*time.Second)
	if len(targets) < 2 {
		t.Skip("need at least 2 WiZ bulbs for color-sync test")
	}
	lightA := targets[0]
	lightB := targets[1]
	t.Logf("lightA=%s lightB=%s", lightA.DeviceID, lightB.DeviceID)

	// Label all wiz entities so the QueryService.Find call inside the script
	// can locate them.
	for _, tgt := range targets {
		patchJSON(t, s, fmt.Sprintf("/api/plugins/plugin-wiz/devices/%s/entities/%s/labels",
			tgt.DeviceID, tgt.EntityID),
			map[string]any{"labels": map[string][]string{"brand": {"wiz"}}})
	}
	time.Sleep(200 * time.Millisecond)

	// --- Script 1: turn-off guard ---
	// When the light reports turn_off, the script turns it back on.
	guardScript := `
function OnInit(ctx)
  This.OnEvent(ctx, function(env)
    if env.Payload.type == "turn_off" then
      This.SendCommand("turn_on", {})
    end
  end)
end
`
	t.Log("installing turn-off guard script on lightA...")
	putScript(t, s, "plugin-wiz", lightA.DeviceID, lightA.EntityID, guardScript)

	t.Log("sending turn_off to lightA — script should immediately turn it back on...")
	cmdID := postCommandAndWait(t, s, "plugin-wiz", lightA.DeviceID, lightA.EntityID,
		map[string]any{"type": "turn_off"}, 10*time.Second)
	waitForCommandState(t, s, "plugin-wiz", cmdID, types.CommandSucceeded, 10*time.Second)

	// After the script fires, the light should be on again.
	time.Sleep(1 * time.Second)
	if lightA.IP != "" {
		if usesMockWizDevices() {
			waitForEntityPower(t, s, "plugin-wiz", lightA.DeviceID, lightA.EntityID, true, 5*time.Second)
		} else {
			assertPhysicalState(t, lightA.IP, true, 5*time.Second)
		}
	}

	// --- Script 2: color sync ---
	// When lightB's color changes, sync the new color to all other WiZ lights.
	colorSyncScript := `
This.OnCommand("set_rgb", function(cmd)
  local others = QueryService.Scripting.Find("?label=brand:wiz")
  others:each(function(e)
    if e.DeviceID ~= This.DeviceID then
      CommandService.Scripting.Send(e, "set_rgb", {rgb = cmd.Params.rgb})
    end
  end)
end)
`
	t.Log("installing color-sync script on lightB...")
	putScript(t, s, "plugin-wiz", lightB.DeviceID, lightB.EntityID, colorSyncScript)

	t.Log("setting lightB to red — all other WiZ lights should follow...")
	cmdID = postCommandAndWait(t, s, "plugin-wiz", lightB.DeviceID, lightB.EntityID,
		map[string]any{"type": light.ActionSetRGB, "rgb": []int{255, 0, 0}}, 10*time.Second)
	waitForCommandState(t, s, "plugin-wiz", cmdID, types.CommandSucceeded, 10*time.Second)

	// Give the script time to fan out.
	time.Sleep(2 * time.Second)

	// Every other WiZ bulb should now be red.
	for _, tgt := range targets {
		if tgt.DeviceID == lightB.DeviceID || tgt.IP == "" {
			continue
		}
		if usesMockWizDevices() {
			t.Logf("verifying lightB color-sync updated entity state for %s...", tgt.DeviceID)
			waitForEntityRGB(t, s, "plugin-wiz", tgt.DeviceID, tgt.EntityID, 255, 0, 0, 5*time.Second)
		} else {
			t.Logf("verifying lightB color-sync reached %s...", tgt.IP)
			assertPhysicalPilotRGB(t, tgt.IP, 255, 0, 0, 5*time.Second)
		}
	}
}

// TestWizLocal_DiscoverAndSetRGB boots a real stack (no mock devices), waits
// for the plugin to discover real WiZ bulbs on the network, then sends a
// set_rgb command and verifies both the gateway entity state and the physical
// bulb state via a direct UDP getPilot query.
func TestWizLocal_DiscoverAndSetRGB(t *testing.T) {
	s := integrationtesting.New(t, "github.com/slidebolt/plugin-wiz", ".")
	s.RequirePlugin("plugin-wiz")

	t.Log("waiting for WiZ bulbs to be discovered (up to 30s)...")
	targets := waitForWizTargets(t, s, 30*time.Second)
	if len(targets) == 0 {
		t.Skip("no WiZ bulbs discovered on this network")
	}

	t.Logf("discovered %d WiZ device(s):", len(targets))
	for i, tgt := range targets {
		t.Logf("  [%d] device=%s entity=%s ip=%s", i, tgt.DeviceID, tgt.EntityID, tgt.IP)
	}

	target := targets[0]
	t.Logf("testing against device=%s entity=%s ip=%s", target.DeviceID, target.EntityID, target.IP)

	color := []int{0, 255, 0}
	cmdID := postCommandAndWait(t, s, "plugin-wiz", target.DeviceID, target.EntityID,
		map[string]any{"type": light.ActionSetRGB, "rgb": color}, 10*time.Second)

	t.Logf("command accepted: id=%s — waiting for CommandSucceeded...", cmdID)
	waitForCommandState(t, s, "plugin-wiz", cmdID, types.CommandSucceeded, 10*time.Second)

	t.Log("command succeeded — checking entity Reported state...")
	waitForEntityRGB(t, s, "plugin-wiz", target.DeviceID, target.EntityID, 0, 255, 0, 8*time.Second)

	if target.IP != "" {
		t.Logf("checking physical pilot at %s...", target.IP)
		assertPhysicalPilotRGB(t, target.IP, 0, 255, 0, 8*time.Second)
	} else {
		t.Log("no IP label on device — skipping physical pilot check")
	}
}

// TestWizLocal_StabilityUnderDiscoveryLoad polls device count for 20 seconds
// to verify the plugin remains stable under continuous discovery activity.
func TestWizLocal_StabilityUnderDiscoveryLoad(t *testing.T) {
	s := integrationtesting.New(t, "github.com/slidebolt/plugin-wiz", ".")
	s.RequirePlugin("plugin-wiz")

	t.Log("starting 20s stability soak...")
	deadline := time.Now().Add(20 * time.Second)
	maxDevices := 0
	tick := 0
	for time.Now().Before(deadline) {
		devs := listPluginDevices(t, s, "plugin-wiz")
		if len(devs) > maxDevices {
			maxDevices = len(devs)
			t.Logf("tick %d: new max %d device(s)", tick, maxDevices)
			for _, d := range devs {
				t.Logf("  device=%s ip=%s", d.DeviceID, d.IP)
			}
		}
		tick++
		time.Sleep(250 * time.Millisecond)
	}
	if maxDevices == 0 {
		t.Skip("no WiZ bulbs discovered during stability soak")
	}
	t.Logf("stability soak complete: max=%d devices seen over %d polls", maxDevices, tick)
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

// patchJSON sends a PATCH request with a JSON body and fatals on non-2xx.
func patchJSON(t *testing.T, s *integrationtesting.Suite, path string, body any) []byte {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("patchJSON: marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, s.APIURL()+path, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("patchJSON: new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patchJSON %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out []byte
	json.NewDecoder(resp.Body).Decode(&out) //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("patchJSON %s: status %d body=%s", path, resp.StatusCode, string(out))
	}
	return out
}

// putJSON sends a PUT request with a JSON body and fatals on non-2xx.
func putJSON(t *testing.T, s *integrationtesting.Suite, path string, body any) []byte {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("putJSON: marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, s.APIURL()+path, bytes.NewReader(b))
	if err != nil {
		t.Fatalf("putJSON: new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("putJSON %s: %v", path, err)
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	raw.ReadFrom(resp.Body) //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("putJSON %s: status %d body=%s", path, resp.StatusCode, raw.String())
	}
	return raw.Bytes()
}

// putScript uploads a Lua script source for an entity.
// This will FAIL until sdk-runner implements the scripts/put RPC method.
func putScript(t *testing.T, s *integrationtesting.Suite, pluginID, deviceID, entityID, source string) {
	t.Helper()
	path := fmt.Sprintf("/api/plugins/%s/devices/%s/entities/%s/script", pluginID, deviceID, entityID)
	putJSON(t, s, path, map[string]any{"source": source})
}

// searchDevices calls GET /api/search/devices?label=... and returns results.
func searchDevices(t *testing.T, s *integrationtesting.Suite, labels ...string) []types.Device {
	t.Helper()
	u := s.APIURL() + "/api/search/devices"
	for i, l := range labels {
		if i == 0 {
			u += "?label=" + l
		} else {
			u += "&label=" + l
		}
	}
	resp, err := http.Get(u) //nolint:noctx
	if err != nil {
		t.Fatalf("searchDevices: %v", err)
	}
	defer resp.Body.Close()
	var out []types.Device
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("searchDevices: decode: %v", err)
	}
	return out
}

// searchEntities calls GET /api/search/entities?label=... and returns results.
func searchEntities(t *testing.T, s *integrationtesting.Suite, labels ...string) []types.Entity {
	t.Helper()
	u := s.APIURL() + "/api/search/entities"
	for i, l := range labels {
		if i == 0 {
			u += "?label=" + l
		} else {
			u += "&label=" + l
		}
	}
	resp, err := http.Get(u) //nolint:noctx
	if err != nil {
		t.Fatalf("searchEntities: %v", err)
	}
	defer resp.Body.Close()
	var out []types.Entity
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("searchEntities: decode: %v", err)
	}
	return out
}

// createPluginGroupEntity saves a query-backed group entity under the provided
// plugin namespace into the aggregate registry via NATS.
func createPluginGroupEntity(t *testing.T, s *integrationtesting.Suite, pluginID string, ent types.Entity) {
	t.Helper()
	nc, err := nats.Connect(s.NATSURL())
	if err != nil {
		t.Fatalf("createPluginGroupEntity: NATS connect: %v", err)
	}
	defer nc.Close()

	reg := regsvc.RegistryService(pluginID,
		regsvc.WithNATS(nc),
		regsvc.WithPersist(regsvc.PersistNever),
	)
	if err := reg.Start(); err != nil {
		t.Fatalf("createPluginGroupEntity: registry start: %v", err)
	}
	defer reg.Stop()

	if err := reg.SaveEntity(ent); err != nil {
		t.Fatalf("createPluginGroupEntity: SaveEntity: %v", err)
	}
	time.Sleep(60 * time.Millisecond) // let the aggregate registry propagate
}

// checkDiskFile verifies the JSON file at path exists and contains a field
// with the given string value anywhere in its JSON structure.
func checkDiskFile(t *testing.T, path, field, wantValue string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("checkDiskFile: %s not found: %v", path, err)
		return
	}
	if !strings.Contains(string(data), wantValue) {
		t.Errorf("checkDiskFile: %s does not contain %q (field=%s)\ncontent: %s",
			path, wantValue, field, string(data))
	} else {
		t.Logf("disk file OK: %s contains %s=%q", filepath.Base(path), field, wantValue)
	}
}

// ---------------------------------------------------------------------------
// Discovery helpers
// ---------------------------------------------------------------------------

func waitForWizTargets(t *testing.T, s *integrationtesting.Suite, timeout time.Duration) []wizTarget {
	t.Helper()
	var targets []wizTarget
	s.WaitFor(timeout, func() bool {
		targets = listPluginDevices(t, s, "plugin-wiz")
		return len(targets) > 0
	})
	return targets
}

func listPluginDevices(t *testing.T, s *integrationtesting.Suite, pluginID string) []wizTarget {
	t.Helper()
	var devices []types.Device
	if err := s.GetJSON(fmt.Sprintf("/api/plugins/%s/devices", pluginID), &devices); err != nil {
		t.Logf("listPluginDevices: GetJSON error: %v", err)
		return nil
	}
	out := make([]wizTarget, 0, len(devices))
	for _, d := range devices {
		ip := ""
		if ips, ok := d.Labels["ip"]; ok && len(ips) > 0 {
			ip = ips[0]
		}
		entityID := firstLightEntityID(t, s, pluginID, d.ID)
		out = append(out, wizTarget{DeviceID: d.ID, EntityID: entityID, IP: ip})
	}
	return out
}

func firstLightEntityID(t *testing.T, s *integrationtesting.Suite, pluginID, deviceID string) string {
	t.Helper()
	var entities []types.Entity
	path := fmt.Sprintf("/api/plugins/%s/devices/%s/entities", pluginID, deviceID)
	if err := s.GetJSON(path, &entities); err != nil {
		return ""
	}
	for _, e := range entities {
		if e.Domain == light.Type {
			return e.ID
		}
	}
	if len(entities) > 0 {
		return entities[0].ID
	}
	return ""
}

// ---------------------------------------------------------------------------
// Command helpers
// ---------------------------------------------------------------------------

// postCommandAndWait sends a command and returns the command ID.
func postCommandAndWait(t *testing.T, s *integrationtesting.Suite, pluginID, deviceID, entityID string, payload map[string]any, timeout time.Duration) string {
	t.Helper()
	path := fmt.Sprintf("%s/api/plugins/%s/devices/%s/entities/%s/commands",
		s.APIURL(), pluginID, deviceID, entityID)
	body, _ := json.Marshal(payload)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Post(path, "application/json", bytes.NewReader(body)) //nolint:noctx
		if err != nil {
			t.Logf("postCommand: http error: %v — retrying", err)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		var st types.CommandStatus
		_ = json.NewDecoder(resp.Body).Decode(&st)
		resp.Body.Close()
		if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusOK {
			return st.CommandID
		}
		t.Logf("postCommand: unexpected status %d — retrying", resp.StatusCode)
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("command to %s/%s/%s did not get accepted within %s", pluginID, deviceID, entityID, timeout)
	return ""
}

// waitForCommandState polls the command status endpoint until the desired
// state is reached, logging every state transition.
func waitForCommandState(t *testing.T, s *integrationtesting.Suite, pluginID, cmdID string, want types.CommandState, timeout time.Duration) {
	t.Helper()
	path := fmt.Sprintf("/api/plugins/%s/commands/%s", pluginID, cmdID)
	last := types.CommandState("")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var st types.CommandStatus
		if err := s.GetJSON(path, &st); err != nil {
			t.Logf("waitForCommandState: GetJSON error: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if st.State != last {
			t.Logf("command %s: state=%s error=%q", cmdID, st.State, st.Error)
			last = st.State
		}
		if st.State == want {
			return
		}
		if st.State == types.CommandFailed {
			t.Fatalf("command %s failed: %s", cmdID, st.Error)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("command %s did not reach state %q within %s (last=%s)", cmdID, want, timeout, last)
}

// ---------------------------------------------------------------------------
// Entity state helpers
// ---------------------------------------------------------------------------

func waitForEntityRGB(t *testing.T, s *integrationtesting.Suite, pluginID, deviceID, entityID string, wantR, wantG, wantB int, timeout time.Duration) {
	t.Helper()
	path := fmt.Sprintf("/api/plugins/%s/devices/%s/entities", pluginID, deviceID)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var entities []types.Entity
		if err := s.GetJSON(path, &entities); err != nil {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		for _, e := range entities {
			if e.ID != entityID {
				continue
			}
			t.Logf("entity %s: desired=%s reported=%s", e.ID, string(e.Data.Desired), string(e.Data.Reported))
			if len(e.Data.Reported) > 0 {
				var st light.State
				if err := json.Unmarshal(e.Data.Reported, &st); err == nil {
					if len(st.RGB) == 3 && st.RGB[0] == wantR && st.RGB[1] == wantG && st.RGB[2] == wantB {
						t.Logf("entity %s reached reported RGB [%d,%d,%d]", entityID, wantR, wantG, wantB)
						return
					}
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Errorf("entity %s/%s/%s did not reach RGB [%d,%d,%d] within %s",
		pluginID, deviceID, entityID, wantR, wantG, wantB, timeout)
}

func waitForEntityPower(t *testing.T, s *integrationtesting.Suite, pluginID, deviceID, entityID string, wantOn bool, timeout time.Duration) {
	t.Helper()
	path := fmt.Sprintf("/api/plugins/%s/devices/%s/entities", pluginID, deviceID)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var entities []types.Entity
		if err := s.GetJSON(path, &entities); err != nil {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		for _, e := range entities {
			if e.ID != entityID {
				continue
			}
			t.Logf("entity %s: desired=%s reported=%s", e.ID, string(e.Data.Desired), string(e.Data.Reported))
			if len(e.Data.Reported) > 0 {
				var st light.State
				if err := json.Unmarshal(e.Data.Reported, &st); err == nil && st.Power == wantOn {
					t.Logf("entity %s reached reported power=%v", entityID, wantOn)
					return
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Errorf("entity %s/%s/%s did not reach power=%v within %s",
		pluginID, deviceID, entityID, wantOn, timeout)
}

func usesMockWizDevices() bool {
	return strings.TrimSpace(os.Getenv("WIZ_MOCK_DEVICES")) != ""
}

// ---------------------------------------------------------------------------
// Physical bulb assertion helpers (via direct UDP getPilot)
// ---------------------------------------------------------------------------

func assertPhysicalState(t *testing.T, ip string, wantOn bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		params, err := getPilot(ip)
		if err != nil {
			t.Logf("getPilot(%s): %v", ip, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		got, ok := params["state"].(bool)
		t.Logf("getPilot(%s): state=%v", ip, got)
		if ok && got == wantOn {
			t.Logf("physical bulb at %s confirmed state=%v", ip, wantOn)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("physical bulb at %s did not reach state=%v within %s", ip, wantOn, timeout)
}

func assertPhysicalBrightness(t *testing.T, ip string, wantDimming int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		params, err := getPilot(ip)
		if err != nil {
			t.Logf("getPilot(%s): %v", ip, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		got, ok := toInt(params["dimming"])
		t.Logf("getPilot(%s): dimming=%v", ip, got)
		if ok && got == wantDimming {
			t.Logf("physical bulb at %s confirmed dimming=%d", ip, wantDimming)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("physical bulb at %s did not reach dimming=%d within %s", ip, wantDimming, timeout)
}

func assertPhysicalTemperature(t *testing.T, ip string, wantTemp int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		params, err := getPilot(ip)
		if err != nil {
			t.Logf("getPilot(%s): %v", ip, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		got, ok := toInt(params["temp"])
		t.Logf("getPilot(%s): temp=%v", ip, got)
		if ok && got == wantTemp {
			t.Logf("physical bulb at %s confirmed temp=%d", ip, wantTemp)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("physical bulb at %s did not reach temp=%d within %s", ip, wantTemp, timeout)
}

func assertPhysicalPilotRGB(t *testing.T, ip string, wantR, wantG, wantB int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		params, err := getPilot(ip)
		if err != nil {
			t.Logf("getPilot(%s): error: %v", ip, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		t.Logf("getPilot(%s): result=%v", ip, params)
		r, rok := toInt(params["r"])
		g, gok := toInt(params["g"])
		b, bok := toInt(params["b"])
		if rok && gok && bok {
			if r == wantR && g == wantG && b == wantB {
				t.Logf("physical bulb at %s confirmed RGB [%d,%d,%d]", ip, wantR, wantG, wantB)
				return
			}
			t.Logf("physical bulb at %s: got [%d,%d,%d], want [%d,%d,%d]", ip, r, g, b, wantR, wantG, wantB)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("physical bulb at %s did not reach RGB [%d,%d,%d] within %s", ip, wantR, wantG, wantB, timeout)
}

// getPilot sends a raw UDP getPilot request to a WiZ bulb and returns the
// result map.
func getPilot(ip string) (map[string]any, error) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(ip, "38899"))
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"method":"getPilot","params":{}}`)); err != nil {
		return nil, err
	}
	buf := make([]byte, 2048)
	conn.SetReadDeadline(time.Now().Add(600 * time.Millisecond)) //nolint:errcheck
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// toInt robustly converts JSON-decoded numeric values to int.
func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case json.Number:
		i, err := x.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(x)
		return i, err == nil
	}
	return 0, false
}

// wizTarget holds the resolved address for a discovered WiZ device.
type wizTarget struct {
	DeviceID string
	EntityID string
	IP       string
}
