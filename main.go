package main

import (
	"log"
	"os"
	"strings"

	"github.com/slidebolt/plugin-wiz/pkg/wiz"
	runner "github.com/slidebolt/sdk-runner"
)

func newClient() wiz.Client {
	if raw := strings.TrimSpace(os.Getenv("WIZ_MOCK_DEVICES")); raw != "" {
		return wiz.NewSimulatedMockClient(wiz.ParseMockDevices(raw))
	}
	return wiz.NewRealClient()
}

func main() {
	if err := runner.RunCLI(func() runner.Plugin { return NewWizPlugin(newClient()) }); err != nil {
		log.Fatal(err)
	}
}
