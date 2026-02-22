package logic

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

type WizClient interface {
	SendProbe() error
	Listen(callback func(ip string, result WizSystemConfig)) (func(), error)
	SetPilot(ip string, mac string, params map[string]interface{}) error
	GetPilot(ip string) (map[string]interface{}, error)
	GetUserConfig(ip string) (map[string]interface{}, error)
	GetDevInfo(ip string) (map[string]interface{}, error)
	GetSystemConfig(ip string) (*WizSystemConfig, error)
	Close() error
}

type RealWizClient struct {
	udpConn *net.UDPConn
}

type WizSystemConfig struct {
	Mac string `json:"mac"`
}

type WizResponse struct {
	Result WizSystemConfig `json:"result"`
}

type wizMapResponse struct {
	Result map[string]interface{} `json:"result"`
}

func (c *RealWizClient) SendProbe() error {
	sysConfig := []byte(`{"method":"getSystemConfig","params":{}}`)
	port := "38899"

	if c.udpConn == nil {
		fmt.Printf("[DEBUG-WIZ] Sending probe to 255.255.255.255:%s (temporary connection)\n", port)
		dest := &net.UDPAddr{IP: net.IPv4bcast, Port: 38899}
		addr := &net.UDPAddr{IP: net.IPv4zero, Port: 0}
		conn, err := net.ListenUDP("udp", addr)
		if err == nil {
			_, _ = conn.WriteToUDP(sysConfig, dest)
			conn.Close()
		}
		return nil
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		fmt.Printf("[DEBUG-WIZ] net.Interfaces failed: %v\n", err)
		return err
	}

	for _, iface := range ifaces {
		fmt.Printf("[DEBUG-WIZ] Checking interface %s flags=%v\n", iface.Name, iface.Flags)
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			fmt.Printf("[DEBUG-WIZ] iface.Addrs failed for %s: %v\n", iface.Name, err)
			continue
		}
		for _, addr := range addrs {
			fmt.Printf("[DEBUG-WIZ] Interface %s has addr %s\n", iface.Name, addr.String())
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			// Calculate broadcast address
			ip := ipnet.IP.To4()
			mask := ipnet.Mask
			broadcast := make(net.IP, len(ip))
			for i := range ip {
				broadcast[i] = ip[i] | ^mask[i]
			}
			dest := &net.UDPAddr{IP: broadcast, Port: 38899}
			fmt.Printf("[DEBUG-WIZ] Sending probe from %s to %s on %s\n", c.udpConn.LocalAddr().String(), dest.String(), iface.Name)
			_, _ = c.udpConn.WriteToUDP(sysConfig, dest)
		}
	}
	// Also send to global broadcast
	dest := &net.UDPAddr{IP: net.IPv4bcast, Port: 38899}
	_, _ = c.udpConn.WriteToUDP(sysConfig, dest)

	return nil
}

func (c *RealWizClient) Listen(callback func(ip string, result WizSystemConfig)) (func(), error) {
	// Wiz devices respond to the source port, or broadcast back to 38899.
	// We bind to 38899 to receive broadcast responses and standard replies.
	addr := &net.UDPAddr{IP: net.IPv4zero, Port: 38899}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		// If 38899 is taken, try ephemeral as fallback
		addr = &net.UDPAddr{IP: net.IPv4zero, Port: 0}
		conn, err = net.ListenUDP("udp", addr)
		if err != nil {
			return nil, err
		}
	}
	c.udpConn = conn

	stop := func() { conn.Close() }

	go func() {
		fmt.Printf("[DEBUG-WIZ] UDP Listener started on %s\n", conn.LocalAddr().String())
		buf := make([]byte, 4096)
		for {
			n, remoteAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}

			rawJSON := string(buf[:n])
			fmt.Printf("[DEBUG-WIZ] Received raw packet from %s: %s\n", remoteAddr.String(), rawJSON)
			b := make([]byte, n)
			copy(b, buf[:n])

			var resp WizResponse
			if err := json.Unmarshal(b, &resp); err == nil && resp.Result.Mac != "" {
				callback(remoteAddr.IP.String(), resp.Result)
			} else if err != nil {
				fmt.Printf("[DEBUG-WIZ] Failed to unmarshal from %s: %v\n", remoteAddr.String(), err)
			}
		}
	}()
	return stop, nil
}

func (c *RealWizClient) SetPilot(ip string, mac string, params map[string]interface{}) error {
	payload := map[string]interface{}{
		"method": "setPilot",
		"params": params,
	}
	data, _ := json.Marshal(payload)
	fmt.Printf("[DEBUG-WIZ] Preparing to send setPilot to %s: %s\n", ip, string(data))

	addr, err := net.ResolveUDPAddr("udp", ip+":38899")
	if err != nil {
		fmt.Printf("[DEBUG-WIZ] ResolveUDPAddr failed for %s: %v\n", ip, err)
		return err
	}

	// Use ephemeral port for sending command
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		fmt.Printf("[DEBUG-WIZ] ListenUDP failed: %v\n", err)
		return err
	}
	defer conn.Close()

	n, err := conn.WriteToUDP(data, addr)
	if err != nil {
		fmt.Printf("[DEBUG-WIZ] WriteToUDP failed to %s: %v\n", ip, err)
		return err
	}
	fmt.Printf("[DEBUG-WIZ] Sent %d bytes to %s from %s\n", n, ip, conn.LocalAddr().String())

	return nil
}

func (c *RealWizClient) GetPilot(ip string) (map[string]interface{}, error) {
	return c.query(ip, `{"method":"getPilot","params":{}}`)
}

func (c *RealWizClient) GetUserConfig(ip string) (map[string]interface{}, error) {
	return c.query(ip, `{"method":"getUserConfig","params":{}}`)
}

func (c *RealWizClient) GetDevInfo(ip string) (map[string]interface{}, error) {
	return c.query(ip, `{"method":"getDevInfo","params":{}}`)
}

func (c *RealWizClient) GetSystemConfig(ip string) (*WizSystemConfig, error) {
	addr, err := net.ResolveUDPAddr("udp", ip+":38899")
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	msg := `{"method":"getSystemConfig","params":{}}`
	_, err = conn.Write([]byte(msg))
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 2048)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}

	var resp WizResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, err
	}
	return &resp.Result, nil
}

func (c *RealWizClient) query(ip string, payload string) (map[string]interface{}, error) {
	addr, err := net.ResolveUDPAddr("udp", ip+":38899")
	if err != nil {
		return nil, err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_, err = conn.Write([]byte(payload))
	if err != nil {
		return nil, err
	}

	buf := make([]byte, 2048)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
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

type ClientConstructor func() WizClient

var DefaultConstructor ClientConstructor = func() WizClient {
	return &RealWizClient{}
}

type MockWizClient struct {
	LastParams map[string]interface{}
	LastIP     string
}

func (m *MockWizClient) SendProbe() error { return nil }

func (m *MockWizClient) Listen(callback func(ip string, result WizSystemConfig)) (func(), error) {
	return func() {}, nil
}

func (m *MockWizClient) SetPilot(ip string, mac string, params map[string]interface{}) error {
	m.LastIP = ip
	m.LastParams = params
	return nil
}

func (m *MockWizClient) GetPilot(ip string) (map[string]interface{}, error) {
	return map[string]interface{}{"state": false}, nil
}

func (m *MockWizClient) GetUserConfig(ip string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *MockWizClient) GetDevInfo(ip string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

func (m *MockWizClient) GetSystemConfig(ip string) (*WizSystemConfig, error) {
	return &WizSystemConfig{Mac: "mock-mac-123"}, nil
}

func (m *MockWizClient) Close() error { return nil }
