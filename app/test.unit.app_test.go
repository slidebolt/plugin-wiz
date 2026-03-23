package app_test

import (
	"testing"

	"github.com/slidebolt/plugin-wiz/app"
)

func TestHelloManifest(t *testing.T) {
	if got := app.New().Hello(); got.ID != app.PluginID {
		t.Fatalf("Hello().ID = %q, want %q", got.ID, app.PluginID)
	}
}
