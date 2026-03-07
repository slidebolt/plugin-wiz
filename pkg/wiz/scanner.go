package wiz

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
)

// ParseStaticHosts parses a comma-separated list of static host IPs
func ParseStaticHosts(raw string) []string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		h := strings.TrimSpace(p)
		if h != "" {
			out = append(out, h)
		}
	}
	return out
}

func LocalSubnetCandidates() []string {
	set := map[string]struct{}{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			if !ip4.IsPrivate() || ip4.IsLoopback() {
				continue
			}
			for i := 1; i <= 254; i++ {
				host := fmt.Sprintf("%d.%d.%d.%d", ip4[0], ip4[1], ip4[2], i)
				set[host] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for host := range set {
		out = append(out, host)
	}
	return out
}

func ProbeHostsConcurrently(hosts []string, workers int, fn func(host string)) {
	if workers <= 0 {
		workers = 1
	}
	var idx int64
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				j := int(atomic.AddInt64(&idx, 1) - 1)
				if j >= len(hosts) {
					return
				}
				fn(hosts[j])
			}
		}()
	}
	wg.Wait()
}
