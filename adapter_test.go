package main

import (
	"testing"

	"github.com/slidebolt/plugin-wiz/pkg/wiz"
	"github.com/slidebolt/sdk-entities/light"
)

func TestLightEventPayload_FromPilotSnapshot(t *testing.T) {
	cmd := light.Command{Type: light.ActionSetRGB}
	pilot := map[string]any{
		"state":   true,
		"dimming": 42,
		"r":       255,
		"g":       0,
		"b":       128,
	}

	got := lightEventPayload(cmd, pilot, []string{light.ActionSetRGB})

	if got["power"] != true {
		t.Fatalf("power = %#v, want true", got["power"])
	}
	if got["brightness"] != 42 {
		t.Fatalf("brightness = %#v, want 42", got["brightness"])
	}
	rgb, ok := got["rgb"].([]int)
	if !ok || len(rgb) != 3 || rgb[0] != 255 || rgb[1] != 0 || rgb[2] != 128 {
		t.Fatalf("rgb = %#v, want [255 0 128]", got["rgb"])
	}
	if got["color_mode"] != light.ColorModeRGB {
		t.Fatalf("color_mode = %#v, want %q", got["color_mode"], light.ColorModeRGB)
	}
}

func TestSimulatedMockClient_MergesPilotState(t *testing.T) {
	client := wiz.NewSimulatedMockClient([]wiz.SimulatedDevice{{IP: "10.0.0.1", MAC: "mac01"}})

	if err := client.SetPilot("10.0.0.1", "mac01", map[string]any{"state": false}); err != nil {
		t.Fatalf("SetPilot state: %v", err)
	}
	if err := client.SetPilot("10.0.0.1", "mac01", map[string]any{"r": 10, "g": 20, "b": 30}); err != nil {
		t.Fatalf("SetPilot rgb: %v", err)
	}

	pilot, err := client.GetPilot("10.0.0.1")
	if err != nil {
		t.Fatalf("GetPilot: %v", err)
	}
	if pilot["state"] != true {
		t.Fatalf("state = %#v, want true", pilot["state"])
	}
	if pilot["r"] != 10 || pilot["g"] != 20 || pilot["b"] != 30 {
		t.Fatalf("pilot rgb = %#v, want 10/20/30", pilot)
	}
}
