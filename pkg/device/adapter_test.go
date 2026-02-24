package device

import (
	"os"
	"github.com/slidebolt/plugin-framework"
	"github.com/slidebolt/plugin-sdk"
	 "github.com/slidebolt/plugin-wiz/pkg/logic"
	"testing"
	"time"
)

type testWizClient struct {
	pilot      map[string]interface{}
	lastSetIP  string
	lastParams map[string]interface{}
}

func (m *testWizClient) SendProbe() error { return nil }
func (m *testWizClient) Listen(callback func(ip string, result logic.WizSystemConfig)) (func(), error) {
	return func() {}, nil
}
func (m *testWizClient) SetPilot(ip string, mac string, params map[string]interface{}) error {
	m.lastSetIP = ip
	m.lastParams = params
	for k, v := range params {
		m.pilot[k] = v
	}
	return nil
}
func (m *testWizClient) GetPilot(ip string) (map[string]interface{}, error) { return m.pilot, nil }
func (m *testWizClient) GetUserConfig(ip string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (m *testWizClient) GetDevInfo(ip string) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}
func (m *testWizClient) GetSystemConfig(ip string) (*logic.WizSystemConfig, error) {
	return &logic.WizSystemConfig{Mac: "aabbcc"}, nil
}
func (m *testWizClient) Close() error { return nil }

func TestRegisterAndCommand_WizPublishesState(t *testing.T) {
	_ = os.RemoveAll("state")
	_ = os.RemoveAll("logs")
	t.Cleanup(func() {
		_ = os.RemoveAll("state")
		_ = os.RemoveAll("logs")
	})

	b, err := framework.RegisterBundle("plugin-wiz-adapter-test")
	if err != nil {
		t.Fatalf("register bundle: %v", err)
	}

	client := &testWizClient{pilot: map[string]interface{}{"state": false, "dimming": 12, "temp": 3000}}
	Register(b, client, "192.168.1.10", "AA:BB:CC")
	time.Sleep(150 * time.Millisecond)

	obj, ok := b.GetBySourceID(sdk.SourceID("aabbcc"))
	if !ok {
		t.Fatalf("expected device by source id")
	}
	dev := obj.(sdk.Device)
	ents, _ := dev.GetEntities()
	if len(ents) == 0 {
		t.Fatalf("expected light entity")
	}
	light := ents[0].(sdk.Light)
	if light.State().Status != "active" {
		t.Fatalf("expected initial active status, got %s", light.State().Status)
	}

	_ = light.SetTemperature(4200)
	time.Sleep(150 * time.Millisecond)

	if client.lastSetIP != "192.168.1.10" {
		t.Fatalf("expected command sent to wiz ip")
	}
	if got, _ := client.lastParams["temp"].(int); got != 4200 {
		t.Fatalf("expected temp command 4200, got %+v", client.lastParams)
	}

	if light.State().Status != "active" {
		t.Fatalf("expected light to move active after command, got %s", light.State().Status)
	}
}
