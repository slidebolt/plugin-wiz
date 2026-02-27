package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

type WizClient interface {
	SendProbe() error
	Listen(callback func(ip string, result WizSystemConfig)) (func(), error)
	SetPilot(ip string, mac string, params map[string]any) error
	GetPilot(ip string) (map[string]any, error)
	GetSystemConfig(ip string) (*WizSystemConfig, error)
	Close() error
}

type RealWizClient struct {
	udpConn *net.UDPConn
}

type WizSystemConfig struct {
	Mac string `json:"mac"`
}

type wizConfigResponse struct {
	Result WizSystemConfig `json:"result"`
}

type wizMapResponse struct {
	Result map[string]any `json:"result"`
}

func (c *RealWizClient) SendProbe() error {
	sysConfig := []byte(`{"method":"getSystemConfig","params":{}}`)

	conn := c.udpConn
	ephemeral := false
	if conn == nil {
		var err error
		conn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			return err
		}
		ephemeral = true
	}
	if ephemeral {
		defer conn.Close()
	}

	_, _ = conn.WriteToUDP(sysConfig, &net.UDPAddr{IP: net.IPv4bcast, Port: 38899})

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
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
			mask := ipnet.Mask
			bcast := make(net.IP, len(ip4))
			for i := range ip4 {
				bcast[i] = ip4[i] | ^mask[i]
			}
			_, _ = conn.WriteToUDP(sysConfig, &net.UDPAddr{IP: bcast, Port: 38899})
		}
	}
	return nil
}

func (c *RealWizClient) Listen(callback func(ip string, result WizSystemConfig)) (func(), error) {
	addr := &net.UDPAddr{IP: net.IPv4zero, Port: 38899}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		addr = &net.UDPAddr{IP: net.IPv4zero, Port: 0}
		conn, err = net.ListenUDP("udp", addr)
		if err != nil {
			return nil, err
		}
	}
	c.udpConn = conn

	stop := func() { _ = conn.Close() }
	go func() {
		buf := make([]byte, 4096)
		for {
			n, remoteAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var resp wizConfigResponse
			if err := json.Unmarshal(buf[:n], &resp); err != nil {
				continue
			}
			if resp.Result.Mac == "" {
				continue
			}
			callback(remoteAddr.IP.String(), resp.Result)
		}
	}()
	return stop, nil
}

func (c *RealWizClient) SetPilot(ip string, _ string, params map[string]any) error {
	payload := map[string]any{
		"method": "setPilot",
		"params": params,
	}
	data, _ := json.Marshal(payload)
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(strings.TrimSpace(ip), "38899"))
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

func (c *RealWizClient) GetPilot(ip string) (map[string]any, error) {
	return c.query(ip, `{"method":"getPilot","params":{}}`)
}

func (c *RealWizClient) GetSystemConfig(ip string) (*WizSystemConfig, error) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(strings.TrimSpace(ip), "38899"))
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if _, err = conn.Write([]byte(`{"method":"getSystemConfig","params":{}}`)); err != nil {
		return nil, err
	}
	buf := make([]byte, 2048)
	_ = conn.SetReadDeadline(time.Now().Add(350 * time.Millisecond))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	var resp wizConfigResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, err
	}
	if resp.Result.Mac == "" {
		return nil, fmt.Errorf("missing mac in system config")
	}
	return &resp.Result, nil
}

func (c *RealWizClient) query(ip string, payload string) (map[string]any, error) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(strings.TrimSpace(ip), "38899"))
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err = conn.Write([]byte(payload)); err != nil {
		return nil, err
	}
	buf := make([]byte, 2048)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	var resp wizMapResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

func (c *RealWizClient) Close() error {
	if c.udpConn != nil {
		return c.udpConn.Close()
	}
	return nil
}
