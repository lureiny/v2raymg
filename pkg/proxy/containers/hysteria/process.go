package hysteria

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/tools/process"
	"gopkg.in/yaml.v3"
)

// hysteriaServerConfig is the top-level hysteria2 server configuration.
type hysteriaServerConfig struct {
	Listen       string               `yaml:"listen"`
	TLS          hysteriaTLS          `yaml:"tls"`
	Auth         hysteriaAuth         `yaml:"auth"`
	TrafficStats hysteriaTrafficStats `yaml:"trafficStats"`
}

type hysteriaTLS struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type hysteriaAuth struct {
	Type string           `yaml:"type"`
	HTTP hysteriaAuthHTTP `yaml:"http"`
}

type hysteriaAuthHTTP struct {
	URL string `yaml:"url"`
}

type hysteriaTrafficStats struct {
	Listen string `yaml:"listen"`
	Secret string `yaml:"secret"`
}

// startProcess ensures the binary exists, generates config, and starts the hysteria process.
func (hc *HysteriaContainer) startProcess() error {
	// Ensure binary exists, download if missing
	if _, err := os.Stat(hc.cfg.BinaryPath); os.IsNotExist(err) {
		log.Infof("hysteria: binary not found at %s, downloading %s", hc.cfg.BinaryPath, hc.cfg.Version)
		if err := downloadHysteria(hc.cfg.Version, hc.cfg.BinaryPath); err != nil {
			return fmt.Errorf("hysteria: auto-download failed: %w", err)
		}
		log.Infof("hysteria: downloaded hysteria %s to %s", hc.cfg.Version, hc.cfg.BinaryPath)
	}

	// Get cert files:
	// 1. direct config path (cert_file/key_file) takes priority — for local/test setups
	// 2. certReader (injected store-backed reader)
	// 3. certMgr (ACME manager)
	var certFile, keyFile string
	if hc.cfg.CertFile != "" && hc.cfg.KeyFile != "" {
		certFile = hc.cfg.CertFile
		keyFile = hc.cfg.KeyFile
	} else if hc.certReader != nil {
		var ok bool
		certFile, keyFile, ok = hc.certReader.GetCertFiles(hc.cfg.Domain)
		if !ok {
			return fmt.Errorf("hysteria: certificate not available for domain %s", hc.cfg.Domain)
		}
	} else if hc.certMgr != nil {
		var ok bool
		certFile, keyFile, ok = hc.certMgr.GetCertFiles(hc.cfg.Domain)
		if !ok {
			return fmt.Errorf("hysteria: certificate not available for domain %s", hc.cfg.Domain)
		}
	} else {
		return fmt.Errorf("hysteria: no certificate source: certReader/certMgr not configured and cert_file/key_file not set in config")
	}

	// Generate config file
	if err := hc.generateConfigFile(certFile, keyFile); err != nil {
		return fmt.Errorf("hysteria: generate config: %w", err)
	}

	// Create process runner
	runner, err := process.NewRunner(process.RunnerConfig{
		BinaryPath: hc.cfg.BinaryPath,
		Args:       []string{"server", "--config", hc.cfg.ConfigFilePath},
		Stdout:     log.NewPrefixWriter(os.Stdout, string(contracts.ContainerHysteria)),
		Stderr:     log.NewPrefixWriter(os.Stderr, string(contracts.ContainerHysteria)),
	})
	if err != nil {
		return fmt.Errorf("hysteria: create runner: %w", err)
	}
	hc.runner = runner

	// Start process
	if err := runner.Start(); err != nil {
		return fmt.Errorf("hysteria: start process: %w", err)
	}

	log.Infof("hysteria: started (pid=%d)", runner.PID())
	return nil
}

// stopProcess stops the running hysteria process.
func (hc *HysteriaContainer) stopProcess() error {
	if hc.runner == nil {
		return nil
	}
	log.Infof("hysteria: stopping process, pid=%d", hc.runner.PID())
	if err := hc.runner.Stop(); err != nil {
		return fmt.Errorf("hysteria: stop process: %w", err)
	}
	log.Infof("hysteria: stopped")
	hc.runner = nil
	return nil
}

// generateConfigFile writes the hysteria2 server YAML config to cfg.ConfigFilePath.
func (hc *HysteriaContainer) generateConfigFile(certFile, keyFile string) error {
	cfg := hysteriaServerConfig{
		// Bind loopback only: hysteria is reached via UDP forward rules
		// allocated per-user by UserManager.GetBindPort(Network="udp"); binding
		// a public interface here would let clients bypass the forward layer.
		Listen: fmt.Sprintf("127.0.0.1:%d", hc.cfg.Port),
		TLS: hysteriaTLS{
			Cert: certFile,
			Key:  keyFile,
		},
		Auth: hysteriaAuth{
			Type: "http",
			HTTP: hysteriaAuthHTTP{
				URL: fmt.Sprintf("http://127.0.0.1:%d/api/authHysteria2", hc.httpPort),
			},
		},
		TrafficStats: hysteriaTrafficStats{
			Listen: fmt.Sprintf("127.0.0.1:%d", hc.cfg.TrafficStatsPort),
			Secret: hc.cfg.TrafficStatsSecret,
		},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("hysteria: marshal config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(hc.cfg.ConfigFilePath), 0755); err != nil {
		return fmt.Errorf("hysteria: create config dir: %w", err)
	}
	if err := os.WriteFile(hc.cfg.ConfigFilePath, data, 0644); err != nil {
		return fmt.Errorf("hysteria: write config: %w", err)
	}
	return nil
}
