//go:build integration



package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/slidebolt/sdk-integration-testing"
	"github.com/slidebolt/sdk-types"
)

// world holds per-scenario state.
type world struct {
	t             *testing.T
	suite         *integrationtesting.Suite
	lastCode      int
	lastBody      []byte
	lastCommandID string
	deviceID      string
	pluginID      string
	mockDevice    string
}

func (w *world) registerSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the wiz plugin starts with mock device "([^"]*)"$`, w.theWizPluginStartsWithMockDevice)
	sc.Step(`^the plugin "([^"]*)" should register with the gateway$`, w.thePluginShouldRegisterWithGateway)
	sc.Step(`^the device "([^"]*)" should appear under plugin "([^"]*)"$`, w.theDeviceShouldAppearUnderPlugin)
	sc.Step(`^the device should have a "([^"]*)" entity$`, w.theDeviceShouldHaveEntity)
	sc.Step(`^the device "([^"]*)" should have a "([^"]*)" entity$`, w.theNamedDeviceShouldHaveEntity)
	sc.Step(`^the plugin is restarted$`, w.thePluginIsRestarted)
	sc.Step(`^the device "([^"]*)" should still be registered$`, w.theDeviceShouldStillBeRegistered)
	sc.Step(`^I send command "([^"]*)" to device "([^"]*)" entity "([^"]*)"$`, w.iSendSimpleCommand)
	sc.Step(`^I send command "([^"]*)" with rgb \[(\d+),(\d+),(\d+)\] to device "([^"]*)" entity "([^"]*)"$`, w.iSendRGBCommand)
	sc.Step(`^I send command "([^"]*)" with brightness (\d+) to device "([^"]*)" entity "([^"]*)"$`, w.iSendBrightnessCommand)
	sc.Step(`^the command should succeed$`, w.theCommandShouldSucceed)
	sc.Step(`^the command should fail with "([^"]*)"$`, w.theCommandShouldFailWith)
}

func (w *world) theWizPluginStartsWithMockDevice(device string) error {
	w.mockDevice = device
	w.t.Setenv("WIZ_MOCK_DEVICES", device)
	w.suite = integrationtesting.New(w.t, "github.com/slidebolt/plugin-wiz", ".")
	w.pluginID = "plugin-wiz"
	return nil
}

func (w *world) thePluginShouldRegisterWithGateway(pluginID string) error {
	if w.suite == nil {
		return fmt.Errorf("harness not started; call 'the wiz plugin starts with mock device' first")
	}
	w.suite.RequirePlugin(pluginID)
	return nil
}

func (w *world) theDeviceShouldAppearUnderPlugin(deviceID, pluginID string) error {
	w.deviceID = deviceID
	ok := w.suite.WaitFor(30*time.Second, func() bool {
		return w.deviceExistsUnderPlugin(deviceID, pluginID)
	})
	if !ok {
		return fmt.Errorf("device %q not found under plugin %q after 30s", deviceID, pluginID)
	}
	return nil
}

func (w *world) theDeviceShouldHaveEntity(entityID string) error {
	return w.theNamedDeviceShouldHaveEntity(w.deviceID, entityID)
}

func (w *world) theNamedDeviceShouldHaveEntity(deviceID, entityID string) error {
	w.deviceID = deviceID
	ok := w.suite.WaitFor(15*time.Second, func() bool {
		return w.entityExists(w.pluginID, deviceID, entityID)
	})
	if !ok {
		return fmt.Errorf("entity %q not found on device %q after 15s", entityID, deviceID)
	}
	return nil
}

func (w *world) thePluginIsRestarted() error {
	if w.suite != nil {
		w.suite.Stop()
	}
	// WIZ_MOCK_DEVICES is still set from theWizPluginStartsWithMockDevice.
	w.suite = integrationtesting.New(w.t, "github.com/slidebolt/plugin-wiz", ".")
	w.suite.RequirePlugin(w.pluginID)
	return nil
}

func (w *world) theDeviceShouldStillBeRegistered(deviceID string) error {
	ok := w.suite.WaitFor(30*time.Second, func() bool {
		return w.deviceExistsUnderPlugin(deviceID, w.pluginID)
	})
	if !ok {
		return fmt.Errorf("device %q not found after restart", deviceID)
	}
	return nil
}

func (w *world) iSendSimpleCommand(cmdType, deviceID, entityID string) error {
	payload := map[string]any{"type": cmdType}
	return w.sendCommand(deviceID, entityID, payload)
}

func (w *world) iSendRGBCommand(cmdType string, r, g, b int, deviceID, entityID string) error {
	payload := map[string]any{"type": cmdType, "rgb": []int{r, g, b}}
	return w.sendCommand(deviceID, entityID, payload)
}

func (w *world) iSendBrightnessCommand(cmdType string, brightness int, deviceID, entityID string) error {
	payload := map[string]any{"type": cmdType, "brightness": brightness}
	return w.sendCommand(deviceID, entityID, payload)
}

func (w *world) theCommandShouldSucceed() error {
	if w.lastCode != http.StatusAccepted && w.lastCode != http.StatusOK {
		return fmt.Errorf("expected 2xx, got %d: %s", w.lastCode, w.lastBody)
	}
	return nil
}

func (w *world) theCommandShouldFailWith(msg string) error {
	if w.lastCode != http.StatusAccepted {
		return fmt.Errorf("expected 202 Accepted, got %d", w.lastCode)
	}
	if w.lastCommandID == "" {
		return fmt.Errorf("no command ID captured from response")
	}
	// Poll command status until failed or timeout
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var st types.CommandStatus
		path := fmt.Sprintf("/api/plugins/%s/commands/%s", w.pluginID, w.lastCommandID)
		if err := w.suite.GetJSON(path, &st); err == nil {
			if st.State == types.CommandFailed {
				if msg != "" && st.Error != msg {
					return fmt.Errorf("expected error %q, got %q", msg, st.Error)
				}
				return nil
			}
			if st.State == types.CommandSucceeded {
				return fmt.Errorf("expected command to fail, but it succeeded")
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("command did not reach failed state within 5s (id=%s)", w.lastCommandID)
}

// --- helpers ---

func (w *world) sendCommand(deviceID, entityID string, payload map[string]any) error {
	path := fmt.Sprintf("%s/api/plugins/%s/devices/%s/entities/%s/commands",
		w.suite.APIURL(), w.pluginID, deviceID, entityID)
	body, _ := json.Marshal(payload)
	resp, err := http.Post(path, "application/json", bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	w.lastCode = resp.StatusCode
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body) //nolint:errcheck
	w.lastBody = buf.Bytes()
	// Capture command ID for status polling
	if resp.StatusCode == http.StatusAccepted {
		var st types.CommandStatus
		if err := json.Unmarshal(w.lastBody, &st); err == nil {
			w.lastCommandID = st.CommandID
		}
	}
	return nil
}

func (w *world) deviceExistsUnderPlugin(deviceID, pluginID string) bool {
	var devices []types.Device
	if err := w.suite.GetJSON(fmt.Sprintf("/api/plugins/%s/devices", pluginID), &devices); err != nil {
		return false
	}
	for _, d := range devices {
		if d.ID == deviceID {
			return true
		}
	}
	return false
}

func (w *world) entityExists(pluginID, deviceID, entityID string) bool {
	var entities []types.Entity
	path := fmt.Sprintf("/api/plugins/%s/devices/%s/entities", pluginID, deviceID)
	if err := w.suite.GetJSON(path, &entities); err != nil {
		return false
	}
	for _, e := range entities {
		if e.ID == entityID {
			return true
		}
	}
	return false
}

// TestIntegration_BDD runs the Gherkin feature files against a live stack.
func TestIntegration_BDD(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping BDD integration suite in short mode")
	}

	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			// Use a scenario-scoped T so t.Setenv is cleaned up after each
			// scenario and never leaks WIZ_MOCK_DEVICES into subsequent tests.
			st := t
			sc.Before(func(ctx context.Context, s *godog.Scenario) (context.Context, error) {
				st = t // reset for each scenario; replaced per-subtest when godog creates one
				return ctx, nil
			})
			w := &world{t: st}
			w.registerSteps(sc)
			// Ensure suite is stopped and env is clean after each scenario.
			sc.After(func(ctx context.Context, s *godog.Scenario, err error) (context.Context, error) {
				if w.suite != nil {
					w.suite.Stop()
					w.suite = nil
				}
				return ctx, nil
			})
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("BDD scenarios failed")
	}
}
