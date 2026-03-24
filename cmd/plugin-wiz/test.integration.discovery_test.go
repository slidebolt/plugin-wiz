//go:build integration

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	app "github.com/slidebolt/plugin-wiz/app"
	wiz "github.com/slidebolt/plugin-wiz/internal/wiz"
	testkit "github.com/slidebolt/sb-testkit"
)

// TestDiscovery_PrintAllDevices discovers real Wiz bulbs on the network and
// prints every device/entity ID so we can map them to production_groups.csv.
//
// Run: go test -tags integration -v -run TestDiscovery_PrintAllDevices ./cmd/plugin-wiz/
func TestDiscovery_PrintAllDevices(t *testing.T) {
	subnet := os.Getenv("WIZ_SUBNET")
	if subnet == "" {
		subnet = "192.168.88"
	}
	timeoutMs := 2000
	timeout := time.Duration(timeoutMs) * time.Millisecond

	t.Logf("discovering Wiz devices on %s.* (timeout %v)...", subnet, timeout)

	devices, err := wiz.DiscoverDevices(subnet, timeout)
	if err != nil {
		t.Fatalf("discovery error: %v", err)
	}
	if len(devices) == 0 {
		t.Fatal("no Wiz devices found on subnet " + subnet)
	}

	t.Logf("\n=== Wiz Discovery Results: %d device(s) ===", len(devices))
	t.Logf("%-24s %-18s %-18s %s", "DEVICE_ID", "MAC", "IP", "PILOT_STATE")
	t.Log("---")

	for _, dev := range devices {
		deviceID := app.MakeDeviceID(dev.Mac)
		pilotStr := ""
		if dev.GetPilot != nil {
			if b, err := json.Marshal(dev.GetPilot); err == nil {
				pilotStr = string(b)
			}
		}
		t.Logf("%-24s %-18s %-18s %s", deviceID, dev.Mac, dev.IP, pilotStr)
	}

	// Now start the full plugin to see what entity keys get registered.
	t.Log("\n=== Starting plugin to see registered entity keys ===")

	env := testkit.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")

	p := app.New()
	deps := map[string]json.RawMessage{
		"messenger": env.MessengerPayload(),
	}
	if _, err := p.OnStart(deps); err != nil {
		t.Fatalf("plugin OnStart: %v", err)
	}
	t.Cleanup(func() { p.OnShutdown() })

	// Wait for discovery to complete (already happened in OnStart).
	store := env.Storage()

	// Search for all plugin-wiz entities.
	entries, err := store.Search("plugin-wiz.*.*")
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	t.Logf("\n=== Registered Entities: %d ===", len(entries))
	t.Logf("%-60s %-10s %s", "ENTITY_KEY", "TYPE", "NAME")
	t.Log("---")

	type entityRow struct {
		Key      string
		Type     string
		Name     string
		DeviceID string
		ID       string
		MAC      string
		IP       string
	}
	var rows []entityRow

	for _, e := range entries {
		var ent struct {
			ID       string                       `json:"id"`
			Plugin   string                       `json:"plugin"`
			DeviceID string                       `json:"deviceID"`
			Type     string                       `json:"type"`
			Name     string                       `json:"name"`
			Meta     map[string]json.RawMessage    `json:"meta"`
		}
		if json.Unmarshal(e.Data, &ent) != nil {
			continue
		}
		mac, ip := "", ""
		if m, ok := ent.Meta["mac"]; ok {
			json.Unmarshal(m, &mac)
		}
		if m, ok := ent.Meta["ip"]; ok {
			json.Unmarshal(m, &ip)
		}
		rows = append(rows, entityRow{
			Key: e.Key, Type: ent.Type, Name: ent.Name,
			DeviceID: ent.DeviceID, ID: ent.ID, MAC: mac, IP: ip,
		})
		t.Logf("%-60s %-10s %s (mac=%s ip=%s)", e.Key, ent.Type, ent.Name, mac, ip)
	}

	// Print a CSV-friendly summary for mapping.
	t.Log("\n=== CSV Mapping ===")
	t.Log("entity_key,device_id,entity_id,type,name,mac,ip")
	for _, r := range rows {
		t.Logf("%s,%s,%s,%s,%s,%s,%s", r.Key, r.DeviceID, r.ID, r.Type, r.Name, r.MAC, r.IP)
	}

	fmt.Fprintf(os.Stderr, "\nWiz discovery complete: %d devices, %d entities\n", len(devices), len(rows))
}
