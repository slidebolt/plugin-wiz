package main

import (
	"encoding/json"
	"testing"

	"github.com/slidebolt/plugin-wiz/pkg/wiz"
	"github.com/slidebolt/sdk-entities/light"
)

func TestLightEventPayloadContract_TurnOn(t *testing.T) {
	cmd := light.Command{Type: light.ActionTurnOn}
	pilot := map[string]any{"state": true, "dimming": 80}
	payload := lightEventPayload(cmd, pilot, []string{light.ActionTurnOn, light.ActionTurnOff})
	b, _ := json.Marshal(payload)
	var got map[string]any
	json.Unmarshal(b, &got)
	if _, hasType := got["type"]; hasType {
		t.Errorf("payload must not contain 'type' field (old format), got: %v", got)
	}
	power, ok := got["power"].(bool)
	if !ok {
		t.Fatalf("payload must contain 'power' bool field, got: %v", got)
	}
	if !power {
		t.Errorf("expected power=true, got false")
	}
}

func TestLightEventPayloadContract_TurnOff(t *testing.T) {
	cmd := light.Command{Type: light.ActionTurnOff}
	pilot := map[string]any{"state": false}
	payload := lightEventPayload(cmd, pilot, []string{light.ActionTurnOn, light.ActionTurnOff})
	b, _ := json.Marshal(payload)
	var got map[string]any
	json.Unmarshal(b, &got)
	if _, hasType := got["type"]; hasType {
		t.Errorf("payload must not contain 'type' field (old format), got: %v", got)
	}
	power, ok := got["power"].(bool)
	if !ok {
		t.Fatalf("payload must contain 'power' bool field, got: %v", got)
	}
	if power {
		t.Errorf("expected power=false, got true")
	}
}

func TestLightEventPayloadContract_Brightness(t *testing.T) {
	cmd := light.Command{Type: light.ActionSetBrightness}
	pilot := map[string]any{"state": true, "dimming": 75}
	payload := lightEventPayload(cmd, pilot, []string{light.ActionSetBrightness})
	b, _ := json.Marshal(payload)
	var got map[string]any
	json.Unmarshal(b, &got)
	if _, hasType := got["type"]; hasType {
		t.Errorf("payload must not contain 'type' field, got: %v", got)
	}
	if _, ok := got["brightness"]; !ok {
		t.Errorf("payload must contain 'brightness' field, got: %v", got)
	}
}

func TestLightEventPayloadContract_RGB(t *testing.T) {
	cmd := light.Command{Type: light.ActionSetRGB}
	pilot := map[string]any{"state": true, "r": 255, "g": 0, "b": 128}
	payload := lightEventPayload(cmd, pilot, []string{light.ActionSetRGB})
	b, _ := json.Marshal(payload)
	var got map[string]any
	json.Unmarshal(b, &got)
	if _, hasType := got["type"]; hasType {
		t.Errorf("payload must not contain 'type' field, got: %v", got)
	}
	if _, ok := got["rgb"]; !ok {
		t.Errorf("payload must contain 'rgb' field, got: %v", got)
	}
}

func TestLightEventPayloadContract_Temperature(t *testing.T) {
	cmd := light.Command{Type: light.ActionSetTemperature}
	pilot := map[string]any{"state": true, "temp": 2700}
	payload := lightEventPayload(cmd, pilot, []string{light.ActionSetTemperature})
	b, _ := json.Marshal(payload)
	var got map[string]any
	json.Unmarshal(b, &got)
	if _, hasType := got["type"]; hasType {
		t.Errorf("payload must not contain 'type' field, got: %v", got)
	}
	if _, ok := got["temperature"]; !ok {
		t.Errorf("payload must contain 'temperature' field, got: %v", got)
	}
}

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
