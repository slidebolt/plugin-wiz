package app_test

import (
	"encoding/json"
	"reflect"
	"testing"

	wizapp "github.com/slidebolt/plugin-wiz/app"
	domain "github.com/slidebolt/sb-domain"
	managersdk "github.com/slidebolt/sb-manager-sdk"
)

func TestStorageContract_WizEntityRoundTrips(t *testing.T) {
	env := managersdk.NewTestEnv(t)
	env.Start("messenger")
	env.Start("storage")

	entity := domain.Entity{
		ID:       "light-6c299047d3ee",
		Plugin:   wizapp.PluginID,
		DeviceID: "wiz-6c299047d3ee",
		Type:     "light",
		Name:     "WiZ d3ee",
		Commands: []string{"light_turn_on", "light_turn_off", "light_set_brightness", "light_set_color_temp", "light_set_rgb"},
		State: domain.Light{
			Power:       true,
			Brightness:  75,
			RGB:         []int{255, 0, 0},
			Temperature: 2700,
		},
	}
	if err := env.Storage().Save(entity); err != nil {
		t.Fatalf("save entity: %v", err)
	}

	raw, err := env.Storage().Get(domain.EntityKey{Plugin: wizapp.PluginID, DeviceID: "wiz-6c299047d3ee", ID: "light-6c299047d3ee"})
	if err != nil {
		t.Fatalf("get entity: %v", err)
	}
	var got domain.Entity
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	state, ok := got.State.(domain.Light)
	if !ok {
		t.Fatalf("state type = %T", got.State)
	}
	if !reflect.DeepEqual(state.RGB, []int{255, 0, 0}) || state.Brightness != 75 {
		t.Fatalf("state = %+v", state)
	}
}
