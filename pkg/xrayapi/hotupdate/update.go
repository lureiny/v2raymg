// Package hotupdate provides hot-update orchestration for xray containers.
// This package handles zero/low-downtime updates by starting a new instance,
// switching forward rules, then stopping the old instance.
package hotupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/containers/xray"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

// Config holds hot-update configuration.
type Config struct {
	// OldConfigPath is the path to the current xray config
	OldConfigPath string
	// NewConfigPath is the path for the new xray config
	NewConfigPath string
	// OldBinaryPath is the path to the current xray binary
	OldBinaryPath string
	// NewBinaryPath is the path to the new xray binary
	NewBinaryPath string
	// GRPCAddress is the gRPC API address for old instance
	GRPCAddress string
	// PortOffset is the offset for new instance ports
	PortOffset uint32
}

// Result represents the result of a hot-update operation.
type Result struct {
	Success           bool
	FromVersion       string
	ToVersion         string
	Error             error
	RollbackPerformed bool
	FailedStage       string
}

// Execute performs a hot-update with the following stages:
// 1. prepare - Prepare new config
// 2. start-new - Start new instance with new binary
// 3. health-check - Verify new instance is healthy
// 4. switch-forward - Switch forward rules to new ports
// 5. drain-old - Wait for connections to drain
// 6. stop-old - Stop old instance
// 7. done - Update complete
func Execute(
	ctx context.Context,
	cfg Config,
	forwardMgr *forward.DefaultForwardManager,
	oldExecutor *xray.Executor,
) *Result {
	result := &Result{
		FromVersion: getVersion(cfg.OldBinaryPath),
		ToVersion:   "new",
	}

	logStage := func(stage string) {
		fmt.Printf("[hot-update] Stage: %s\n", stage)
	}

	// Stage 1: prepare
	logStage("prepare")
	newConfigPath, err := prepareConfig(cfg)
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("prepare failed: %w", err)
		result.FailedStage = "prepare"
		return result
	}

	// Get port mapping
	portOffset := cfg.PortOffset
	oldPorts, err := getInboundPorts(cfg.OldConfigPath)
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("get old ports failed: %w", err)
		result.FailedStage = "prepare"
		return result
	}

	// Build port mappings
	var mappings []portMapping
	for tag, oldPort := range oldPorts {
		mappings = append(mappings, portMapping{
			Tag:     tag,
			OldPort: oldPort,
			NewPort: oldPort + portOffset,
		})
	}

	// Stage 2: start-new
	logStage("start-new")
	newExecutor, err := createNewExecutor(cfg.NewBinaryPath, newConfigPath, cfg.GRPCAddress, portOffset)
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("create new executor failed: %w", err)
		result.FailedStage = "start-new"
		return result
	}

	if err := newExecutor.Start(); err != nil {
		newExecutor.Stop()
		result.Success = false
		result.Error = fmt.Errorf("start new instance failed: %w", err)
		result.FailedStage = "start-new"
		return result
	}

	// Stage 3: health-check
	logStage("health-check")
	time.Sleep(500 * time.Millisecond) // Wait for process to start
	if !newExecutor.IsRunning() {
		newExecutor.Stop()
		result.Success = false
		result.Error = fmt.Errorf("health check failed: new instance not running")
		result.FailedStage = "health-check"
		return result
	}

	// Stage 4: switch-forward
	logStage("switch-forward")
	if err := switchForwardRules(forwardMgr, mappings); err != nil {
		result.Success = false
		result.Error = fmt.Errorf("switch forward rules failed: %w", err)
		result.FailedStage = "switch-forward"
		// Try to rollback
		newExecutor.Stop()
		result.RollbackPerformed = true
		return result
	}

	// Stage 5: drain-old
	logStage("drain-old")
	time.Sleep(500 * time.Millisecond) // Grace period

	// Stage 6: stop-old
	logStage("stop-old")
	if err := oldExecutor.Stop(); err != nil {
		fmt.Printf("[hot-update] Warning: failed to stop old instance: %v\n", err)
	}

	result.Success = true
	logStage("done")
	return result
}

// Rollback performs rollback from new to old instance.
func Rollback(
	forwardMgr *forward.DefaultForwardManager,
	newExecutor, oldExecutor *xray.Executor,
	mappings []portMapping,
) error {
	fmt.Println("[hot-update] Performing rollback...")

	// Stop new instance
	if newExecutor.IsRunning() {
		if err := newExecutor.Stop(); err != nil {
			return fmt.Errorf("failed to stop new instance: %w", err)
		}
	}

	// Switch forward rules back
	for _, mapping := range mappings {
		rules := forwardMgr.GetAllRules()
		for _, rule := range rules {
			if rule.TargetAddr == fmt.Sprintf("127.0.0.1:%d", mapping.NewPort) {
				// Update target back to old port
				if err := forwardMgr.RemoveRule(rule.RuleKey()); err != nil {
					return fmt.Errorf("failed to remove forward rule: %w", err)
				}
				rule.TargetAddr = fmt.Sprintf("127.0.0.1:%d", mapping.OldPort)
				if _, err := forwardMgr.AddRule(*rule); err != nil {
					return fmt.Errorf("failed to restore forward rule: %w", err)
				}
				break
			}
		}
	}

	// Start old instance
	if !oldExecutor.IsRunning() {
		if err := oldExecutor.Start(); err != nil {
			return fmt.Errorf("failed to restart old instance: %w", err)
		}
	}

	fmt.Println("[hot-update] Rollback complete")
	return nil
}

type portMapping struct {
	Tag     string
	OldPort uint32
	NewPort uint32
}

func prepareConfig(cfg Config) (string, error) {
	// Read current config
	data, err := os.ReadFile(cfg.OldConfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to read config: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("failed to parse config: %w", err)
	}

	// Write to new config path
	dir := filepath.Dir(cfg.NewConfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	newData, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal new config: %w", err)
	}

	if err := os.WriteFile(cfg.NewConfigPath, newData, 0644); err != nil {
		return "", fmt.Errorf("failed to write new config: %w", err)
	}

	return cfg.NewConfigPath, nil
}

func getInboundPorts(configPath string) (map[string]uint32, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	ports := make(map[string]uint32)
	inbounds, ok := config["inbounds"].([]interface{})
	if !ok {
		return ports, nil
	}

	for _, in := range inbounds {
		inMap, ok := in.(map[string]interface{})
		if !ok {
			continue
		}
		tag, _ := inMap["tag"].(string)
		if tag == "api" {
			continue
		}
		port, ok := inMap["port"].(float64)
		if !ok {
			continue
		}
		ports[tag] = uint32(port)
	}

	return ports, nil
}

func createNewExecutor(binaryPath, configPath, grpcAddress string, portOffset uint32) (*xray.Executor, error) {
	offsetGRPCPort := 62789 + portOffset
	newGRPCAddr := fmt.Sprintf("127.0.0.1:%d", offsetGRPCPort)

	executor, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     binaryPath,
		ConfigFilePath: configPath,
		ContainerType:  container.ContainerXray,
		GRPCAPIAddress: newGRPCAddr,
		AutoDownload:   false,
	})
	if err != nil {
		return nil, err
	}

	if err := executor.Init(&xray.ExecutorConfig{
		BinaryPath:     binaryPath,
		ConfigFilePath: configPath,
		ContainerType:  container.ContainerXray,
		GRPCAPIAddress: newGRPCAddr,
		AutoDownload:   false,
	}); err != nil {
		return nil, err
	}

	return executor, nil
}

func switchForwardRules(fm *forward.DefaultForwardManager, mappings []portMapping) error {
	for _, mapping := range mappings {
		rules := fm.GetAllRules()
		for _, rule := range rules {
			if rule.TargetAddr == fmt.Sprintf("127.0.0.1:%d", mapping.OldPort) {
				// Update target to new port
				if err := fm.RemoveRule(rule.RuleKey()); err != nil {
					return fmt.Errorf("failed to remove old rule: %w", err)
				}
				rule.TargetAddr = fmt.Sprintf("127.0.0.1:%d", mapping.NewPort)
				if _, err := fm.AddRule(*rule); err != nil {
					return fmt.Errorf("failed to add new rule: %w", err)
				}
				break
			}
		}
	}
	return nil
}

func getVersion(binaryPath string) string {
	if binaryPath == "" {
		return "unknown"
	}
	cmd := exec.Command(binaryPath, "version")
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return string(out)
}
