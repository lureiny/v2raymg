package mihomo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/tools/process"
)

// mihomoInitialConfig is the minimal config.yaml written at startup.
//
// v2raymg uses mihomo exclusively for its inbound listeners — the outbound
// side is kept inert (empty proxies/proxy-groups, single MATCH,DIRECT rule).
// Listeners start empty; per-user listeners are appended in later stages
// via PUT /configs.
type mihomoInitialConfig struct {
	MixedPort          int      `yaml:"mixed-port"`
	AllowLan           bool     `yaml:"allow-lan"`
	ExternalController string   `yaml:"external-controller"`
	Secret             string   `yaml:"secret"`
	LogLevel           string   `yaml:"log-level"`
	Mode               string   `yaml:"mode"`
	Proxies            []any    `yaml:"proxies"`
	ProxyGroups        []any    `yaml:"proxy-groups"`
	Rules              []string `yaml:"rules"`
	Listeners          []any    `yaml:"listeners"`
}

const (
	// downloadTimeout is generous: first run may fetch ~30MB over a slow link,
	// and the Alpha path adds a second request for checksums.txt. 5 minutes
	// absorbs a noticeably slow link without masking a genuinely stuck
	// download. Prior value was 2m, bumped in stage-9 Batch-5 cleanup.
	downloadTimeout = 5 * time.Minute
	// readinessTimeout matches docs/mihomo-container-implementation-plan.md stage 2.
	readinessTimeout = 10 * time.Second
)

// startProcess ensures the binary exists (downloading if needed), writes the
// initial config, spawns the mihomo process, then polls GET /version until
// the REST API is responsive. On readiness failure the process is stopped
// to avoid a zombie.
//
// Auto-download path: when the binary is missing and AutoDownload is true,
// we delegate to the updater with RestartPolicyNever (we're in the middle of
// Start, so the updater must not try to restart us). AutoDownload=false with
// a missing binary fails fast — the operator pre-installs the binary and
// manages its version out-of-band.
func (c *MihomoContainer) startProcess() error {
	if _, err := os.Stat(c.cfg.BinaryPath); os.IsNotExist(err) {
		if !c.cfg.AutoDownload {
			return fmt.Errorf("mihomo: binary not found at %s and auto_download is disabled", c.cfg.BinaryPath)
		}
		if c.updater == nil {
			return fmt.Errorf("mihomo: binary not found at %s and no updater is configured", c.cfg.BinaryPath)
		}
		log.Infof("mihomo: binary not found at %s, auto-downloading release %q", c.cfg.BinaryPath, c.cfg.ReleaseTag)
		dlCtx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
		_, dlErr := c.updater.Update(dlCtx, container.UpdateRequest{
			TargetTag:     c.cfg.ReleaseTag,
			RestartPolicy: container.RestartPolicyNever,
		})
		cancel()
		if dlErr != nil {
			return fmt.Errorf("mihomo: auto-download failed: %w", dlErr)
		}
		log.Infof("mihomo: auto-download complete, binary at %s", c.cfg.BinaryPath)
	}

	if err := os.MkdirAll(c.cfg.DataDir, 0755); err != nil {
		return fmt.Errorf("mihomo: create data dir: %w", err)
	}

	if err := c.generateConfigFile(); err != nil {
		return fmt.Errorf("mihomo: generate config: %w", err)
	}

	runner, err := process.NewRunner(process.RunnerConfig{
		BinaryPath: c.cfg.BinaryPath,
		Args:       []string{"-d", c.cfg.DataDir, "-f", c.cfg.ConfigFilePath},
		Stdout:     log.NewPrefixWriter(os.Stdout, string(contracts.ContainerMihomo)),
		Stderr:     log.NewPrefixWriter(os.Stderr, string(contracts.ContainerMihomo)),
	})
	if err != nil {
		return fmt.Errorf("mihomo: create runner: %w", err)
	}

	if err := runner.Start(); err != nil {
		return fmt.Errorf("mihomo: start process: %w", err)
	}
	restClient := NewRESTClient("http://"+c.cfg.ExternalController, c.cfg.Secret)

	c.mu.Lock()
	c.runner = runner
	c.restClient = restClient
	c.mu.Unlock()
	log.Infof("mihomo: started (pid=%d)", runner.PID())

	readyCtx, cancel := context.WithTimeout(context.Background(), readinessTimeout)
	defer cancel()
	version, err := waitForVersion(readyCtx, restClient)
	if err != nil {
		_ = runner.Stop()
		c.mu.Lock()
		c.runner = nil
		c.restClient = nil
		c.mu.Unlock()
		return fmt.Errorf("mihomo: readiness probe: %w", err)
	}

	c.mu.Lock()
	c.cachedVersion = version
	c.mu.Unlock()
	log.Infof("mihomo: ready (version=%s)", version)

	return nil
}

// stopProcess stops the mihomo process gracefully. Safe to call when the
// process is not running.
func (c *MihomoContainer) stopProcess() error {
	// Snapshot the runner under the lock, then release before calling Stop —
	// Stop can block for up to the SIGINT+SIGKILL window and we don't want
	// Version() readers to wait for it.
	c.mu.RLock()
	runner := c.runner
	c.mu.RUnlock()
	if runner == nil {
		return nil
	}

	log.Infof("mihomo: stopping process, pid=%d", runner.PID())
	if err := runner.Stop(); err != nil {
		return fmt.Errorf("mihomo: stop process: %w", err)
	}

	c.mu.Lock()
	c.runner = nil
	c.restClient = nil
	// cachedVersion is retained: Version() should still return the last-known
	// value after a normal stop, until the next start replaces it.
	c.mu.Unlock()

	log.Infof("mihomo: stopped")
	return nil
}

// generateConfigFile writes the initial on-disk config with empty listeners.
// The authoritative listener set is pushed via REST by restoreAndPushInbounds
// after the process is ready, so this file only has to be valid enough for
// mihomo to boot.
func (c *MihomoContainer) generateConfigFile() error {
	data, err := BuildConfig(c.buildBaseConfig(), nil)
	if err != nil {
		return fmt.Errorf("mihomo: build initial config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(c.cfg.ConfigFilePath), 0755); err != nil {
		return fmt.Errorf("mihomo: create config dir: %w", err)
	}
	// 0600 because the file may embed the REST API bearer token (Secret).
	if err := os.WriteFile(c.cfg.ConfigFilePath, data, 0600); err != nil {
		return fmt.Errorf("mihomo: write config: %w", err)
	}
	return nil
}

// waitForVersion polls GET /version with exponential backoff (50ms→200ms cap)
// until it succeeds or the ctx deadline fires. The caller is responsible for
// setting the ctx timeout.
func waitForVersion(ctx context.Context, rc *RESTClient) (string, error) {
	backoff := 50 * time.Millisecond
	const maxBackoff = 200 * time.Millisecond

	for {
		version, err := rc.GetVersion(ctx)
		if err == nil {
			return version, nil
		}

		if ctx.Err() != nil {
			return "", fmt.Errorf("timed out waiting for /version: last error: %w", err)
		}

		// time.NewTimer + explicit Stop (instead of time.After) so the
		// underlying timer is released immediately when ctx fires. Matters
		// because this template will be reused by stage 6's reconcile loop
		// which polls far more frequently.
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return "", fmt.Errorf("timed out waiting for /version: last error: %w", err)
		case <-timer.C:
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
