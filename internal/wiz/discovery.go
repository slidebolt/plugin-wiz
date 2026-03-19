package wiz

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Mac string `json:"mac"`
}
type configResponse struct {
	Result Config `json:"result"`
}
type pilotResponse struct {
	Result map[string]any `json:"result"`
}
type Device struct {
	IP       string
	Mac      string
	GetPilot map[string]any
}

func DiscoverDevices(subnet string, timeout time.Duration) ([]Device, error) {
	parts := strings.Split(subnet, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("subnet must be like 192.168.88")
	}
	base := strings.Join(parts, ".") + "."
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	discoveryCmd := []byte(`{"method":"getSystemConfig","params":{}}`)
	var devices []Device
	var mu sync.Mutex
	found := make(map[string]bool)
	done := make(chan bool)
	go func() {
		buf := make([]byte, 4096)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(timeout))
			n, remoteAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				select {
				case <-done:
					return
				default:
					continue
				}
			}
			ip := remoteAddr.IP.String()
			var sysResp configResponse
			if err := json.Unmarshal(buf[:n], &sysResp); err == nil && sysResp.Result.Mac != "" {
				mu.Lock()
				if !found[ip] {
					found[ip] = true
					devices = append(devices, Device{IP: ip, Mac: sysResp.Result.Mac})
				}
				mu.Unlock()
			}
		}
	}()
	for i := 1; i <= 254; i++ {
		targetIP := base + strconv.Itoa(i)
		_, _ = conn.WriteToUDP(discoveryCmd, &net.UDPAddr{IP: net.ParseIP(targetIP), Port: 38899})
	}
	_, _ = conn.WriteToUDP(discoveryCmd, &net.UDPAddr{IP: net.IPv4bcast, Port: 38899})
	time.Sleep(timeout)
	close(done)
	for i, dev := range devices {
		if state, err := GetPilot(dev.IP); err == nil {
			devices[i].GetPilot = state
		}
	}
	return devices, nil
}

func GetPilot(ip string) (map[string]any, error) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(ip, "38899"))
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err = conn.Write([]byte(`{"method":"getPilot","params":{}}`)); err != nil {
		return nil, err
	}
	buf := make([]byte, 2048)
	_ = conn.SetReadDeadline(time.Now().Add(350 * time.Millisecond))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	var resp pilotResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

func SetPilot(ip string, params map[string]any) error {
	data, err := json.Marshal(map[string]any{"method": "setPilot", "params": params})
	if err != nil {
		return err
	}
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(ip, "38899"))
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.WriteToUDP(data, addr)
	return err
}
