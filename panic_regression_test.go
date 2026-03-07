package main

import (
	"encoding/json"
	"testing"

	"github.com/slidebolt/plugin-wiz/pkg/wiz"
	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

// mockRawStore implements a simple RawStore for testing
type mockRawStore struct {
	devices  map[string][]byte
	entities map[string]map[string][]byte
}

func newMockRawStore() *mockRawStore {
	return &mockRawStore{
		devices:  make(map[string][]byte),
		entities: make(map[string]map[string][]byte),
	}
}

func (m *mockRawStore) ReadRawDevice(deviceID string) (json.RawMessage, error) {
	return m.devices[deviceID], nil
}

func (m *mockRawStore) WriteRawDevice(deviceID string, data json.RawMessage) error {
	m.devices[deviceID] = data
	return nil
}

func (m *mockRawStore) ReadRawEntity(deviceID, entityID string) (json.RawMessage, error) {
	if m.entities[deviceID] == nil {
		return nil, nil
	}
	return m.entities[deviceID][entityID], nil
}

func (m *mockRawStore) WriteRawEntity(deviceID, entityID string, data json.RawMessage) error {
	if m.entities[deviceID] == nil {
		m.entities[deviceID] = make(map[string][]byte)
	}
	m.entities[deviceID][entityID] = data
	return nil
}

func TestNullStateThenUpsertDoesNotPanic(t *testing.T) {
	p := NewWizPlugin(&wiz.MockClient{})

	// Create mock config
	mockStore := newMockRawStore()
	config := runner.Config{RawStore: mockStore}
	p.OnInitialize(config, types.Storage{Data: []byte("null")})

	// This should not panic - we no longer use local maps, we use RawStore
	p.upsertBulb("192.168.1.50", "aa:bb:cc:dd:ee:ff")

	// Verify the bulb was written to the raw store
	deviceID := "wiz-aabbccddeeff"
	raw, err := mockStore.ReadRawDevice(deviceID)
	if err != nil {
		t.Fatalf("failed to read device from raw store: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected bulb to be written to raw store, got nil")
	}

	var bulb WizBulb
	if err := json.Unmarshal(raw, &bulb); err != nil {
		t.Fatalf("failed to unmarshal bulb: %v", err)
	}

	if bulb.MAC != "aabbccddeeff" {
		t.Errorf("expected MAC aabbccddeeff, got %s", bulb.MAC)
	}
	if bulb.IP != "192.168.1.50" {
		t.Errorf("expected IP 192.168.1.50, got %s", bulb.IP)
	}
}
