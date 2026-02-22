package bundle

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/slidebolt/plugin-wiz/pkg/device"
	"github.com/slidebolt/plugin-wiz/pkg/logic"
	"github.com/slidebolt/plugin-sdk"
)

type WizPlugin struct {
	bundle sdk.Bundle
	client logic.WizClient
	cancel context.CancelFunc
	wait   func()
}

func (p *WizPlugin) Init(b sdk.Bundle) error {
	p.bundle = b
	b.UpdateMetadata("WiZ Connected")
	p.client = &logic.RealWizClient{}
	b.Log().Info("Wiz Plugin Initializing...")

	// Start UDP Listener for discovery responses
	p.client.Listen(func(ip string, result logic.WizSystemConfig) {
		b.Log().Info("Wiz discovery response from %s (mac: %s)", ip, result.Mac)
		device.Register(p.bundle, p.client, ip, result.Mac)
	})

	// Initial Probe
	b.Log().Info("Wiz sending initial probe")
	p.client.SendProbe()

	// Static Discovery
	p.discoverStatic()

	// Background Discovery Polling
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel

	var wg sync.WaitGroup
	p.wait = wg.Wait

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.client.SendProbe()
				p.discoverStatic()
			}
		}
	}()

	return nil
}

func (p *WizPlugin) Shutdown() {
	if p.cancel != nil {
		p.cancel()
		p.wait()
	}
}

func (p *WizPlugin) discoverStatic() {
	raw := p.bundle.Raw()
	hosts, _ := raw["wiz_hosts"].(string)
	if hosts == "" {
		hosts = os.Getenv("WIZ_HOSTS")
	}
	if hosts == "" {
		p.bundle.Log().Debug("Wiz no static hosts configured")
		return
	}

	p.bundle.Log().Info("Wiz static discovery of: %s", hosts)
	for _, host := range strings.Split(hosts, ",") {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}

		cfg, err := p.client.GetSystemConfig(host)
		if err == nil {
			p.bundle.Log().Info("Wiz static discovery success: %s (mac: %s)", host, cfg.Mac)
			device.Register(p.bundle, p.client, host, cfg.Mac)
		} else {
			p.bundle.Log().Error("Wiz static discovery failed: %s: %v", host, err)
		}
	}
}

func NewPlugin() *WizPlugin { return &WizPlugin{} }
