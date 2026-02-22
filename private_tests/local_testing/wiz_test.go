package local_testing

import (
	"bufio"
	"fmt"
	"os"
	"github.com/slidebolt/plugin-framework"
	"github.com/slidebolt/plugin-sdk"
	 "github.com/slidebolt/plugin-wiz/pkg/bundle"
	"strings"
	"testing"
	"time"
)

func init() {
	loadEnv(".env")
}

func loadEnv(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		os.Setenv(parts[0], parts[1])
	}
}

func TestWizEndToEnd(t *testing.T) {
	// 1. Setup Framework
	bundleID := "plugin-wiz-e2e-test"
	b, err := framework.RegisterBundle(bundleID)
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}
	defer os.RemoveAll("state")

	// 2. Initialize Wiz Plugin
	p := &bundle.WizPlugin{}
	if err := p.Init(b); err != nil {
		t.Fatalf("Failed to init: %v", err)
	}

	// 3. Wait for discovery (real or mock based on WIZ_HOSTS)
	fmt.Println("Waiting for Wiz discovery...")
	var target sdk.Device
	for i := 0; i < 10; i++ {
		devs := b.GetDevices()
		if len(devs) > 0 {
			target = devs[0]
			break
		}
		time.Sleep(1 * time.Second)
	}

	if target == nil {
		t.Log("No Wiz devices found, skipping hardware control checks")
		return
	}

	fmt.Printf("Testing against Wiz Device: %s (%s)\n", target.Metadata().Name, target.ID())

	// 4. Test Lua Integration
	// We'll inject a script that intercepts TurnOn and updates a specific status
	lua := `
		function onCommand(cmd, payload, ctx)
			ctx.Log("Wiz Lua intercepted: " .. cmd)
			if cmd == "TurnOn" then
				ctx.UpdateState("status_from_lua")
			elseif cmd == "SetBrightness" then
				ctx.UpdateRaw({ last_brightness = payload.level })
			end
		end
	`
	ents, _ := target.GetEntities()
	if len(ents) == 0 {
		t.Fatal("Device has no entities")
	}
	ent := ents[0]
	ent.UpdateScript(lua)
	time.Sleep(500 * time.Millisecond) // Wait for worker

	// 5. Trigger TurnOn via SDK
	fmt.Println("Triggering TurnOn via SDK Interface...")
	if li, ok := ent.(sdk.Light); ok {
		li.TurnOn()
	} else {
		t.Fatal("Wiz entity is not a Light")
	}

	// 6. Verify Lua processed it
	var state sdk.EntityState
	for i := 0; i < 20; i++ {
		state = ent.State()
		if state.Status == "status_from_lua" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if state.Status != "status_from_lua" {
		t.Errorf("Lua failed to update state. Current status: %s", state.Status)
	}

	// 7. Trigger Brightness Change
	fmt.Println("Triggering SetBrightness(42) via SDK...")
	if li, ok := ent.(sdk.Light); ok {
		li.SetBrightness(42)
	}

	// 8. Verify Raw Update via Lua
	var raw map[string]interface{}
	for i := 0; i < 20; i++ {
		raw = ent.Raw()
		if val, ok := raw["last_brightness"].(float64); ok && val == 42 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if raw["last_brightness"].(float64) != 42 {
		t.Errorf("Lua failed to update raw brightness. Got: %v", raw["last_brightness"])
	}

	fmt.Println("Wiz End-to-End Test Passed!")
}

func TestWizShutdown(t *testing.T) {
	b, err := framework.RegisterBundle("plugin-wiz-shutdown-test")
	if err != nil {
		t.Fatalf("Failed to register: %v", err)
	}
	defer os.RemoveAll("state")

	p := bundle.NewPlugin()
	if err := p.Init(b); err != nil {
		t.Fatalf("Failed to init: %v", err)
	}

	// The probe goroutine is always started by Init regardless of config.
	// Shutdown must cancel the ticker loop and return within 3s.
	done := make(chan struct{})
	go func() { p.Shutdown(); close(done) }()

	select {
	case <-done:
		// Shutdown returned cleanly — probe goroutine exited.
	case <-time.After(3 * time.Second):
		t.Error("Shutdown did not return within 3s — probe goroutine may have leaked")
	}
}
