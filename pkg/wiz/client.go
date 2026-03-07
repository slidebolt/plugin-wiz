package wiz

import (
	"encoding/json"
	"net"
	"strings"
	"time"
)

type Client interface {
	SendProbe() error
	Listen(callback func(ip string, result SystemConfig)) (func(), error)
	SetPilot(ip string, mac string, params map[string]any) error
	GetPilot(ip string) (map[string]any, error)
	GetSystemConfig(ip string) (*SystemConfig, error)
	Close() error
}

type RealClient struct {
	udpConn *net.UDPConn
}

func NewRealClient() *RealClient {
	return &RealClient{}
}

type SystemConfig struct {
	Mac string `json:"mac"`
}

type configResponse struct {
	Result SystemConfig `json:"result"`
}

type mapResponse struct {
	Result map[string]any `json:"result"`
}

func (c *RealClient) SendProbe() error {
	sysConfig := []byte(`{"method":"getSystemConfig","params":{}}`)

	conn := c.udpConn
	ephemeral := false
	if conn == nil {
		var err error
		conn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			return NewError("network", err)
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

func (c *RealClient) Listen(callback func(ip string, result SystemConfig)) (func(), error) {
	addr := &net.UDPAddr{IP: net.IPv4zero, Port: 38899}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		addr = &net.UDPAddr{IP: net.IPv4zero, Port: 0}
		conn, err = net.ListenUDP("udp", addr)
		if err != nil {
			return nil, NewError("network", err)
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
			var resp configResponse
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

func (c *RealClient) SetPilot(ip string, _ string, params map[string]any) error {
	payload := map[string]any{
		"method": "setPilot",
		"params": params,
	}
	data, _ := json.Marshal(payload)
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(strings.TrimSpace(ip), "38899"))
	if err != nil {
		return NewError("network", err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return NewError("network", err)
	}
	defer conn.Close()
	_, err = conn.WriteToUDP(data, addr)
	if err != nil {
		return NewError("network", err)
	}
	return nil
}

func (c *RealClient) GetPilot(ip string) (map[string]any, error) {
	return c.query(ip, `{"method":"getPilot","params":{}}`)
}

func (c *RealClient) GetSystemConfig(ip string) (*SystemConfig, error) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(strings.TrimSpace(ip), "38899"))
	if err != nil {
		return nil, NewError("network", err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, NewError("network", err)
	}
	defer conn.Close()

	if _, err = conn.Write([]byte(`{"method":"getSystemConfig","params":{}}`)); err != nil {
		return nil, NewError("network", err)
	}
	buf := make([]byte, 2048)
	_ = conn.SetReadDeadline(time.Now().Add(350 * time.Millisecond))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, NewError("timeout", err)
		}
		return nil, NewError("offline", err)
	}
	var resp configResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, NewError("network", err)
	}
	if resp.Result.Mac == "" {
		return nil, NewError("offline", ErrDeviceOffline)
	}
	return &resp.Result, nil
}

func (c *RealClient) query(ip string, payload string) (map[string]any, error) {
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(strings.TrimSpace(ip), "38899"))
	if err != nil {
		return nil, NewError("network", err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, NewError("network", err)
	}
	defer conn.Close()
	if _, err = conn.Write([]byte(payload)); err != nil {
		return nil, NewError("network", err)
	}
	buf := make([]byte, 2048)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return nil, NewError("timeout", err)
		}
		return nil, NewError("offline", err)
	}
	var resp mapResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, NewError("network", err)
	}
	return resp.Result, nil
}

func (c *RealClient) Close() error {
	if c.udpConn != nil {
		return c.udpConn.Close()
	}
	return nil
}

type MockClient struct {
	SendProbeFunc       func() error
	ListenFunc          func(callback func(ip string, result SystemConfig)) (func(), error)
	SetPilotFunc        func(ip string, mac string, params map[string]any) error
	GetPilotFunc        func(ip string) (map[string]any, error)
	GetSystemConfigFunc func(ip string) (*SystemConfig, error)
	CloseFunc           func() error
}

func (m *MockClient) SendProbe() error {
	if m.SendProbeFunc != nil {
		return m.SendProbeFunc()
	}
	return nil
}

func (m *MockClient) Listen(callback func(ip string, result SystemConfig)) (func(), error) {
	if m.ListenFunc != nil {
		return m.ListenFunc(callback)
	}
	return func() {}, nil
}

func (m *MockClient) SetPilot(ip string, mac string, params map[string]any) error {
	if m.SetPilotFunc != nil {
		return m.SetPilotFunc(ip, mac, params)
	}
	return nil
}

func (m *MockClient) GetPilot(ip string) (map[string]any, error) {
	if m.GetPilotFunc != nil {
		return m.GetPilotFunc(ip)
	}
	return nil, nil
}

func (m *MockClient) GetSystemConfig(ip string) (*SystemConfig, error) {
	if m.GetSystemConfigFunc != nil {
		return m.GetSystemConfigFunc(ip)
	}
	return nil, nil
}

func (m *MockClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}
