package wiz

import (
	"sync"
	"testing"
)

func TestLocalSubnetCandidates(t *testing.T) {
	// This test will fail in environments without network interfaces
	// but should pass in most CI environments
	candidates := LocalSubnetCandidates()

	// We can't make strong assertions about the results since
	// it depends on the network configuration of the test environment
	// But we can verify it doesn't panic and returns a slice
	t.Logf("Found %d subnet candidates", len(candidates))
}

func TestProbeHostsConcurrently(t *testing.T) {
	t.Run("probes all hosts with single worker", func(t *testing.T) {
		hosts := []string{"host1", "host2", "host3"}
		probed := make(map[string]bool)

		ProbeHostsConcurrently(hosts, 1, func(host string) {
			probed[host] = true
		})

		if len(probed) != 3 {
			t.Errorf("expected 3 hosts probed, got %d", len(probed))
		}

		for _, h := range hosts {
			if !probed[h] {
				t.Errorf("host %s was not probed", h)
			}
		}
	})

	t.Run("probes all hosts with multiple workers", func(t *testing.T) {
		hosts := []string{"a", "b", "c", "d", "e"}
		probed := make(map[string]bool)
		var mu sync.Mutex

		ProbeHostsConcurrently(hosts, 3, func(host string) {
			mu.Lock()
			probed[host] = true
			mu.Unlock()
		})

		if len(probed) != 5 {
			t.Errorf("expected 5 hosts probed, got %d", len(probed))
		}
	})

	t.Run("handles empty host list", func(t *testing.T) {
		probed := false

		ProbeHostsConcurrently([]string{}, 2, func(host string) {
			probed = true
		})

		if probed {
			t.Error("should not have probed any hosts")
		}
	})

	t.Run("uses default worker count of 1", func(t *testing.T) {
		hosts := []string{"host1"}
		probed := false

		ProbeHostsConcurrently(hosts, 0, func(host string) {
			probed = true
		})

		if !probed {
			t.Error("should have probed the host with default worker count")
		}
	})
}

func TestParseStaticHosts(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single host",
			input:    "192.168.1.1",
			expected: []string{"192.168.1.1"},
		},
		{
			name:     "multiple hosts",
			input:    "192.168.1.1, 192.168.1.2, 192.168.1.3",
			expected: []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"},
		},
		{
			name:     "hosts with whitespace",
			input:    "  192.168.1.1  ,  192.168.1.2  ",
			expected: []string{"192.168.1.1", "192.168.1.2"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: []string{},
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: []string{},
		},
		{
			name:     "empty entries",
			input:    "192.168.1.1,,192.168.1.2",
			expected: []string{"192.168.1.1", "192.168.1.2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseStaticHosts(tt.input)
			if len(got) != len(tt.expected) {
				t.Errorf("expected %d hosts, got %d", len(tt.expected), len(got))
				return
			}
			for i, want := range tt.expected {
				if got[i] != want {
					t.Errorf("expected host %d to be %q, got %q", i, want, got[i])
				}
			}
		})
	}
}
