package main

import (
	"testing"

	runner "github.com/slidebolt/sdk-runner"
	"github.com/slidebolt/sdk-types"
)

func TestNullStateThenUpsertDoesNotPanic(t *testing.T) {
	p := NewWizPlugin(nil)
	p.OnInitialize(runner.Config{}, types.Storage{Data: []byte("null")})

	// This currently panics in production when bulbs map is nil.
	p.upsertBulb("192.168.1.50", "aa:bb:cc:dd:ee:ff")

	if got := len(p.snapshotBulbs()); got != 1 {
		t.Fatalf("expected 1 bulb after upsert, got %d", got)
	}
}
