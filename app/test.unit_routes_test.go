package app

import (
	"testing"

	wiz "github.com/slidebolt/plugin-wiz/internal/wiz"
)

func TestMergeDevicesPreservesKnownRoutesAcrossPartialDiscovery(t *testing.T) {
	known := []wiz.Device{
		{IP: "192.168.88.10", Mac: "6c299047aaaa"},
		{IP: "192.168.88.11", Mac: "6c299047bbbb"},
	}
	discovered := []wiz.Device{
		{IP: "192.168.88.12", Mac: "6c299047bbbb"},
	}

	got := mergeDevices(known, discovered)

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].IP != "192.168.88.10" {
		t.Fatalf("first device IP = %q, want preserved route", got[0].IP)
	}
	if got[1].IP != "192.168.88.12" {
		t.Fatalf("second device IP = %q, want refreshed route", got[1].IP)
	}
}
