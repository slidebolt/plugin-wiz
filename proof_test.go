package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slidebolt/plugin-wiz/pkg/wiz"
	"github.com/slidebolt/sdk-types"
)

func TestProofOfValidation(t *testing.T) {
	p := NewWizPlugin(&wiz.MockClient{})

	// Setup mock raw store with a bulb
	mockStore := newMockRawStore()
	p.rawStore = mockStore

	deviceID := "wiz-001122334455"
	bulb := WizBulb{IP: "127.0.0.1", MAC: "00:11:22:33:44:55"}
	bulbRaw, _ := json.Marshal(bulb)
	mockStore.WriteRawDevice(deviceID, bulbRaw)

	entity := types.Entity{
		ID:       "light",
		DeviceID: deviceID,
		Domain:   "light",
	}

	t.Run("Rejects arbitrary JSON", func(t *testing.T) {
		// Even though Payload is json.RawMessage (which accepts anything),
		// the plugin unmarshals it into a light.Command and validates it.
		invalidPayload := json.RawMessage(`{"foo":"bar"}`)
		req := types.Command{
			ID:      "cmd-1",
			Payload: invalidPayload,
		}

		_, err := p.OnCommand(req, entity)
		if err == nil {
			t.Fatal("Expected error for arbitrary JSON, got nil")
		}
		if !strings.Contains(err.Error(), "unsupported light command") {
			t.Errorf("Expected 'unsupported light command' error, got: %v", err)
		}
	})

	t.Run("Rejects valid JSON with invalid values", func(t *testing.T) {
		// Valid light.Command structure, but invalid value (brightness > 100)
		invalidValue := json.RawMessage(`{"type":"set_brightness", "brightness": 150}`)
		req := types.Command{
			ID:      "cmd-2",
			Payload: invalidValue,
		}

		_, err := p.OnCommand(req, entity)
		if err == nil {
			t.Fatal("Expected error for brightness 150, got nil")
		}
		if !strings.Contains(err.Error(), "brightness must be between 0 and 100") {
			t.Errorf("Expected brightness validation error, got: %v", err)
		}
	})
}
