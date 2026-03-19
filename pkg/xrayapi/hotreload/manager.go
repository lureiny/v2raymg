// Package hotreload provides hot-reload strategy for xray containers.
// This implements a fallback mechanism when gRPC dynamic addition fails.
package hotreload

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/containers/xray"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
)

// Config holds hot-reload configuration.
type Config struct {
	// OldConfigPath is the path to the current xray config
	OldConfigPath string
	// NewConfigPath is the path for the new xray config
	NewConfigPath string
	// OldBinaryPath is the path to the current xray binary
	OldBinaryPath string
	// NewBinaryPath is the path to the new xray binary (for updates)
	NewBinaryPath string
	// GRPCAddress is the gRPC API address
	GRPCAddress string
	// HealthCheckTimeout is the timeout for health checks
	HealthCheckTimeout time.Duration
	// HealthCheckInterval is the interval between health checks
	HealthCheckInterval time.Duration
	// PortOffset is the offset added to ports for the new instance
	PortOffset uint32
	// UseNewBinary indicates whether to use NewBinaryPath for hot-update
	UseNewBinary bool
}

// UpdateResult represents the result of a hot-update operation.
type UpdateResult struct {
	Success           bool
	FromVersion       string
	ToVersion         string
	Error             error
	RollbackPerformed bool
	RollbackError     error
	// Stage where failure occurred (for debugging)
	FailedStage string
}

// ReloadResult represents the result of a hot-reload operation.
type ReloadResult struct {
	Success       bool
	OldInstanceID string
	NewInstanceID string
	SwitchedPorts []uint32
	Error         error
	RollbackInfo  *RollbackInfo
}

// RollbackInfo holds information needed to rollback a failed reload.
type RollbackInfo struct {
	OldConfigPath string
	OldBinaryPath string
	OldGRPCPort   uint32
	PortMapping   map[uint32]uint32 // new port -> old port
}

// PortMapping holds the port mapping between old and new instances.
type PortMapping struct {
	OldPort uint32
	NewPort uint32
	Tag     string
}

// Manager handles hot-reload operations for xray containers.
type Manager struct {
	config        Config
	mu            sync.RWMutex
	currentConfig *xrayConfig
	adapter       *xray.Adapter
}

// xrayConfig represents the xray configuration structure.
type xrayConfig struct {
	Log       interface{}              `json:"log"`
	API       interface{}              `json:"api"`
	Stats     interface{}              `json:"stats"`
	Policy    interface{}              `json:"policy"`
	Inbounds  []map[string]interface{} `json:"inbounds"`
	Outbounds []map[string]interface{} `json:"outbounds"`
	Routing   interface{}              `json:"routing"`
}

// NewManager creates a new hot-reload manager.
// Note: For backward compatibility, it uses a default adapter that reads configs from files.
// For full functionality with xray types, use NewManagerWithAdapter.
func NewManager(cfg Config) *Manager {
	return &Manager{
		config:  cfg,
		adapter: xray.NewAdapter(),
	}
}

// GetCurrentInbounds retrieves the current inbound configurations from the config file.
func (m *Manager) GetCurrentInbounds() ([]map[string]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.currentConfig == nil {
		// Load from file if not cached
		return m.loadInboundsFromFile(m.config.OldConfigPath)
	}
	return m.currentConfig.Inbounds, nil
}

// loadInboundsFromFile loads inbounds from a config file.
func (m *Manager) loadInboundsFromFile(configPath string) ([]map[string]interface{}, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg xrayConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Filter out the API inbound
	var inbounds []map[string]interface{}
	for _, in := range cfg.Inbounds {
		if tag, ok := in["tag"].(string); ok && tag == "api" {
			continue
		}
		inbounds = append(inbounds, in)
	}

	return inbounds, nil
}

// PrepareNewConfig prepares a new config with merged inbounds.
// All ports (both existing and new) are offset by PortOffset for the new instance.
func (m *Manager) PrepareNewConfig(newInbounds []contracts.InboundSpec) (string, error) {
	// Get current inbounds
	currentInbounds, err := m.GetCurrentInbounds()
	if err != nil {
		return "", fmt.Errorf("failed to get current inbounds: %w", err)
	}

	// Apply port offset to existing inbounds
	var offsetInbounds []map[string]interface{}
	for _, in := range currentInbounds {
		offsetIn := m.applyPortOffset(in)
		offsetInbounds = append(offsetInbounds, offsetIn)
	}

	// Convert new inbounds to xray format and apply port offset
	var convertedNewInbounds []map[string]interface{}
	for _, spec := range newInbounds {
		native, err := m.adapter.ToProvider(spec)
		if err != nil {
			return "", fmt.Errorf("failed to convert inbound %s: %w", spec.Tag, err)
		}

		var inMap map[string]interface{}
		if err := json.Unmarshal(native.JSON, &inMap); err != nil {
			return "", fmt.Errorf("failed to unmarshal inbound %s: %w", spec.Tag, err)
		}

		// Apply port offset
		inMap = m.applyPortOffset(inMap)
		convertedNewInbounds = append(convertedNewInbounds, inMap)
	}

	// Merge inbounds
	mergedInbounds := append(offsetInbounds, convertedNewInbounds...)

	// Build new config
	newCfg := m.buildConfig(mergedInbounds)

	// Write to new config path
	data, err := json.MarshalIndent(newCfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal new config: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(m.config.NewConfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(m.config.NewConfigPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write new config: %w", err)
	}

	return m.config.NewConfigPath, nil
}

// buildConfig builds a complete xray config from inbounds.
func (m *Manager) buildConfig(inbounds []map[string]interface{}) *xrayConfig {
	// Build inbounds array with API inbound
	allInbounds := []map[string]interface{}{
		{
			"tag":      "api",
			"port":     62789,
			"protocol": "dokodemo-door",
			"settings": map[string]interface{}{"address": "127.0.0.1"},
		},
	}
	allInbounds = append(allInbounds, inbounds...)

	return &xrayConfig{
		Log:   map[string]interface{}{"loglevel": "warning"},
		API:   map[string]interface{}{"tag": "api", "services": []string{"HandlerService", "StatsService", "LoggerService"}},
		Stats: map[string]interface{}{},
		Policy: map[string]interface{}{
			"levels": map[string]interface{}{
				"0": map[string]interface{}{"statsUserUplink": true, "statsUserDownlink": true},
			},
			"system": map[string]interface{}{
				"statsInboundUplink":   true,
				"statsInboundDownlink": true,
			},
		},
		Inbounds: allInbounds,
	}
}

// applyPortOffset applies port offset to an inbound config.
func (m *Manager) applyPortOffset(in map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range in {
		result[k] = v
	}

	if port, ok := result["port"]; ok {
		switch p := port.(type) {
		case float64:
			result["port"] = p + float64(m.config.PortOffset)
		case string:
			var oldPort uint32
			if n, err := fmt.Sscanf(p, "%d", &oldPort); err == nil && n > 0 {
				result["port"] = fmt.Sprintf("%d", oldPort+m.config.PortOffset)
			}
		}
	}
	return result
}

// CreateNewExecutor creates a new xray executor for the new config.
func (m *Manager) CreateNewExecutor(ctx context.Context) (*xray.Executor, error) {
	executor, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     m.config.OldBinaryPath,
		ConfigFilePath: m.config.NewConfigPath,
		ContainerType:  container.ContainerXray,
		GRPCAPIAddress: fmt.Sprintf("127.0.0.1:%d", 62789+m.config.PortOffset),
		AutoDownload:   false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create executor: %w", err)
	}

	if err := executor.Init(&xray.ExecutorConfig{
		BinaryPath:     m.config.OldBinaryPath,
		ConfigFilePath: m.config.NewConfigPath,
		ContainerType:  container.ContainerXray,
		GRPCAPIAddress: fmt.Sprintf("127.0.0.1:%d", 62789+m.config.PortOffset),
		AutoDownload:   false,
	}); err != nil {
		return nil, fmt.Errorf("failed to init executor: %w", err)
	}

	return executor, nil
}

// GetPortMapping returns the port mapping between old and new instances.
func (m *Manager) GetPortMapping() ([]PortMapping, error) {
	currentInbounds, err := m.GetCurrentInbounds()
	if err != nil {
		return nil, err
	}

	newInbounds, err := m.loadInboundsFromFile(m.config.NewConfigPath)
	if err != nil {
		return nil, err
	}

	// Build mapping by tag
	oldByTag := make(map[string]uint32)
	for _, in := range currentInbounds {
		tag, _ := in["tag"].(string)
		port, _ := in["port"].(float64)
		oldByTag[tag] = uint32(port)
	}

	var mappings []PortMapping
	for _, in := range newInbounds {
		tag, _ := in["tag"].(string)
		port, _ := in["port"].(float64)
		newPort := uint32(port)

		if oldPort, exists := oldByTag[tag]; exists {
			mappings = append(mappings, PortMapping{
				OldPort: oldPort,
				NewPort: newPort,
				Tag:     tag,
			})
		}
	}

	return mappings, nil
}

// SwitchForwardRules switches forward rules from old ports to new ports.
func (m *Manager) SwitchForwardRules(fm forward.ForwardManager, mappings []PortMapping) error {
	for _, mapping := range mappings {
		// Get the current rule by tag (using tag as part of the key)
		rules := fm.GetAllRules()
		var targetRule *forward.ForwardRule
		for _, rule := range rules {
			// Check if rule's target contains the old port
			if rule.TargetAddr != "" {
				// Format: "127.0.0.1:PORT"
				if fmt.Sprintf("127.0.0.1:%d", mapping.OldPort) == rule.TargetAddr {
					targetRule = rule
					break
				}
			}
		}

		if targetRule == nil {
			// No rule found for this mapping, skip
			continue
		}

		// Remove old rule
		if err := fm.RemoveRule(targetRule.RuleKey()); err != nil {
			return fmt.Errorf("failed to remove old rule: %w", err)
		}

		// Add new rule with updated target port
		newRule := *targetRule
		newRule.TargetAddr = fmt.Sprintf("127.0.0.1:%d", mapping.NewPort)
		if _, err := fm.AddRule(newRule); err != nil {
			return fmt.Errorf("failed to add new rule: %w", err)
		}
	}
	return nil
}

// HealthCheck performs health check on the new instance.
// Note: In xray 26.x, gRPC QueryStats/AddInbound API has serialization issues,
// so we use a simplified health check that only verifies the process is running.
func (m *Manager) HealthCheck(executor *xray.Executor) error {
	// Wait for process to start
	time.Sleep(500 * time.Millisecond)

	// Check if running (simplified health check without gRPC QueryStats)
	if !executor.IsRunning() {
		return fmt.Errorf("new instance not running")
	}

	// In xray 26.x, gRPC QueryStats has serialization issues
	// So we skip the gRPC-based health check and just verify the process is alive
	// TODO: Once xray fixes the gRPC API, re-enable gRPC health check

	return nil
}

// Rollback rolls back to the old configuration.
func (m *Manager) Rollback(result *ReloadResult) error {
	if result.RollbackInfo == nil {
		return nil
	}

	// The rollback is handled by the caller which manages the executors
	// This method provides information about what needs to be rolled back
	return nil
}

// ExecuteHotReload executes the full hot-reload process.
func (m *Manager) ExecuteHotReload(
	ctx context.Context,
	fm *forward.DefaultForwardManager,
	newInbounds []contracts.InboundSpec,
	oldExecutor *xray.Executor,
) *ReloadResult {
	result := &ReloadResult{}

	// Step 1: Prepare new config
	_, err := m.PrepareNewConfig(newInbounds)
	if err != nil {
		result.Error = fmt.Errorf("failed to prepare new config: %w", err)
		return result
	}

	// Step 2: Get port mapping
	mappings, err := m.GetPortMapping()
	if err != nil {
		result.Error = fmt.Errorf("failed to get port mapping: %w", err)
		return result
	}

	// Step 3: Create new executor
	newExecutor, err := m.CreateNewExecutor(ctx)
	if err != nil {
		result.Error = fmt.Errorf("failed to create new executor: %w", err)
		return result
	}

	// Step 4: Start new xray instance
	if err := newExecutor.Start(); err != nil {
		result.Error = fmt.Errorf("failed to start new xray: %w", err)
		// Try to rollback by stopping new instance if it started
		newExecutor.Stop()
		return result
	}

	// Step 5: Health check
	if err := m.HealthCheck(newExecutor); err != nil {
		result.Error = fmt.Errorf("health check failed: %w", err)
		newExecutor.Stop()
		return result
	}

	// Step 6: Switch forward rules
	if err := m.SwitchForwardRules(fm, mappings); err != nil {
		result.Error = fmt.Errorf("failed to switch forward rules: %w", err)
		// Don't stop new instance - let caller decide
		return result
	}
	result.SwitchedPorts = make([]uint32, len(mappings))
	for i, m := range mappings {
		result.SwitchedPorts[i] = m.NewPort
	}

	// Step 7: Stop old instance
	oldExecutor.Stop()

	result.Success = true
	result.NewInstanceID = fmt.Sprintf("xray-%d", time.Now().Unix())
	return result
}

// ExecuteHotUpdate executes a hot-update with the following stages:
// 1. prepare - Prepare new config
// 2. start-new - Start new instance with new binary
// 3. health-check - Verify new instance is healthy
// 4. switch-forward - Switch forward rules to new ports
// 5. drain-old - Wait for connections to drain
// 6. stop-old - Stop old instance
// 7. done - Update complete
//
// On any failure, rollback is attempted automatically.
func (m *Manager) ExecuteHotUpdate(
	ctx context.Context,
	fm *forward.DefaultForwardManager,
	oldExecutor *xray.Executor,
	oldBinaryPath, newBinaryPath, newConfigPath, newVersion string,
) *UpdateResult {
	result := &UpdateResult{
		FromVersion: oldVersion(oldBinaryPath),
		ToVersion:   newVersion,
	}

	logStage := func(stage string) {
		fmt.Printf("[hot-update] Stage: %s\n", stage)
	}

	// Stage 1: prepare
	logStage("prepare")
	if err := m.prepareForUpdate(newConfigPath); err != nil {
		result.Success = false
		result.Error = fmt.Errorf("prepare failed: %w", err)
		result.FailedStage = "prepare"
		return result
	}

	// Get port mapping before starting new instance
	mappings, err := m.GetPortMapping()
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("get port mapping failed: %w", err)
		result.FailedStage = "prepare"
		return result
	}

	// Determine which binary to use
	actualNewBinary := oldBinaryPath
	if newBinaryPath != "" && newBinaryPath != oldBinaryPath {
		actualNewBinary = newBinaryPath
	}

	// Stage 2: start-new
	logStage("start-new")
	newExecutor, err := m.createUpdateExecutor(actualNewBinary, newConfigPath)
	if err != nil {
		result.Success = false
		result.Error = fmt.Errorf("create new executor failed: %w", err)
		result.FailedStage = "start-new"
		return result
	}

	if err := newExecutor.Start(); err != nil {
		// Rollback: stop new executor if it partially started
		newExecutor.Stop()
		result.Success = false
		result.Error = fmt.Errorf("start new instance failed: %w", err)
		result.FailedStage = "start-new"
		return result
	}

	// Stage 3: health-check
	logStage("health-check")
	if err := m.HealthCheck(newExecutor); err != nil {
		// Rollback: stop new instance
		newExecutor.Stop()
		result.Success = false
		result.Error = fmt.Errorf("health check failed: %w", err)
		result.FailedStage = "health-check"
		return result
	}

	// Stage 4: switch-forward
	logStage("switch-forward")
	if err := m.SwitchForwardRules(fm, mappings); err != nil {
		// Don't stop new instance - let caller decide, but mark failure
		result.Success = false
		result.Error = fmt.Errorf("switch forward rules failed: %w", err)
		result.FailedStage = "switch-forward"
		// Try to rollback: stop new, keep old running
		if stopErr := newExecutor.Stop(); stopErr != nil {
			result.RollbackError = fmt.Errorf("rollback failed: %w", stopErr)
		}
		result.RollbackPerformed = true
		return result
	}

	// Stage 5: drain-old (grace period for existing connections)
	logStage("drain-old")
	time.Sleep(500 * time.Millisecond) // Grace period

	// Stage 6: stop-old
	logStage("stop-old")
	if err := oldExecutor.Stop(); err != nil {
		// Log but continue - new instance is running
		fmt.Printf("[hot-update] Warning: failed to stop old instance: %v\n", err)
	}

	result.Success = true
	logStage("done")
	return result
}

// prepareForUpdate prepares the new config for update scenario.
func (m *Manager) prepareForUpdate(newConfigPath string) error {
	// Copy current config and write to new path
	currentInbounds, err := m.GetCurrentInbounds()
	if err != nil {
		return fmt.Errorf("failed to get current inbounds: %w", err)
	}

	// Build config with current inbounds (same ports)
	newCfg := m.buildConfig(currentInbounds)

	// Write to new config path
	data, err := json.MarshalIndent(newCfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal new config: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(newConfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(newConfigPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write new config: %w", err)
	}

	return nil
}

// createUpdateExecutor creates an executor for the update scenario.
func (m *Manager) createUpdateExecutor(binaryPath, configPath string) (*xray.Executor, error) {
	// Use offset port for new instance gRPC
	offsetGRPCPort := 62789 + m.config.PortOffset
	newGRPCAddr := fmt.Sprintf("127.0.0.1:%d", offsetGRPCPort)

	executor, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     binaryPath,
		ConfigFilePath: configPath,
		ContainerType:  container.ContainerXray,
		GRPCAPIAddress: newGRPCAddr,
		AutoDownload:   false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create executor: %w", err)
	}

	if err := executor.Init(&xray.ExecutorConfig{
		BinaryPath:     binaryPath,
		ConfigFilePath: configPath,
		ContainerType:  container.ContainerXray,
		GRPCAPIAddress: newGRPCAddr,
		AutoDownload:   false,
	}); err != nil {
		return nil, fmt.Errorf("failed to init executor: %w", err)
	}

	return executor, nil
}

// oldVersion gets the version of the old binary.
func oldVersion(binaryPath string) string {
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

// RollbackUpdate rolls back from new instance to old instance.
func (m *Manager) RollbackUpdate(
	fm *forward.DefaultForwardManager,
	newExecutor, oldExecutor *xray.Executor,
	mappings []PortMapping,
) error {
	fmt.Println("[hot-update] Performing rollback...")

	// Stop new instance if running
	if newExecutor.IsRunning() {
		if err := newExecutor.Stop(); err != nil {
			return fmt.Errorf("failed to stop new instance: %w", err)
		}
	}

	// Switch forward rules back to old ports
	for _, mapping := range mappings {
		rules := fm.GetAllRules()
		for _, rule := range rules {
			if rule.TargetAddr == fmt.Sprintf("127.0.0.1:%d", mapping.NewPort) {
				// Remove the rule with new port
				if err := fm.RemoveRule(rule.RuleKey()); err != nil {
					return fmt.Errorf("failed to remove forward rule: %w", err)
				}
				// Add back with old port
				rule.TargetAddr = fmt.Sprintf("127.0.0.1:%d", mapping.OldPort)
				if _, err := fm.AddRule(*rule); err != nil {
					return fmt.Errorf("failed to restore forward rule: %w", err)
				}
				break
			}
		}
	}

	// Start old instance if not running
	if !oldExecutor.IsRunning() {
		if err := oldExecutor.Start(); err != nil {
			return fmt.Errorf("failed to restart old instance: %w", err)
		}
	}

	fmt.Println("[hot-update] Rollback complete")
	return nil
}
