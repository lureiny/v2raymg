package snell

import (
	"fmt"
	"os"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/tools/process"
)

// startProcess ensures the binary exists, generates config, and starts the snell-server process.
func (sc *SnellContainer) startProcess() error {
	// Ensure binary exists, download if missing
	if _, err := os.Stat(sc.cfg.BinaryPath); os.IsNotExist(err) {
		log.Infof("snell: binary not found at %s, downloading %s", sc.cfg.BinaryPath, sc.cfg.Version)
		if err := downloadSnellServer(sc.cfg.Version, sc.cfg.BinaryPath); err != nil {
			return fmt.Errorf("snell: auto-download failed: %w", err)
		}
		log.Infof("snell: downloaded snell-server %s to %s", sc.cfg.Version, sc.cfg.BinaryPath)
	}

	// Generate config file
	if err := sc.generateConfigFile(); err != nil {
		return fmt.Errorf("snell: generate config: %w", err)
	}

	// Create process runner
	runner, err := process.NewRunner(process.RunnerConfig{
		BinaryPath: sc.cfg.BinaryPath,
		Args:       nil,
		ConfigFile: sc.cfg.ConfigFilePath,
	})
	if err != nil {
		return fmt.Errorf("snell: create runner: %w", err)
	}
	sc.runner = runner

	// Start process
	if err := runner.Start(); err != nil {
		return fmt.Errorf("snell: start process: %w", err)
	}

	log.Infof("snell: started (pid=%d)", runner.PID())
	return nil
}

// stopProcess stops the running snell-server process.
func (sc *SnellContainer) stopProcess() error {
	if sc.runner == nil {
		return nil
	}
	log.Infof("snell: stopping process, pid=%d", sc.runner.PID())
	if err := sc.runner.Stop(); err != nil {
		return fmt.Errorf("snell: stop process: %w", err)
	}
	log.Infof("snell: stopped")
	sc.runner = nil
	return nil
}
