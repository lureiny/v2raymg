// Package xray provides Xray container implementation.
package xray

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/lureiny/v2raymg/pkg/log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/containers/xray/profilegen"
	"github.com/lureiny/v2raymg/pkg/proxy/core/chain"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	errs "github.com/lureiny/v2raymg/pkg/proxy/errors"
	"github.com/lureiny/v2raymg/pkg/proxy/tools/process"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/store"
)

// Executor runs Xray process.
// It embeds BaseContainer for generic lifecycle management and adds xray-specific functionality.
type Executor struct {
	*container.BaseContainer // embedded base container for lifecycle management
	*process.Runner          // embedded generic process runner
	config                   ExecutorConfig
	updater                  *Updater
	grpcAPIAddress           string
	inbounds                 map[string]*XrayInbound // in-memory inbound storage
	inboundsMu               sync.RWMutex
	renderer                 *Renderer

	// UserManager integration
	userMgr         *usermanager.UserManager
	userEventCh     chan usermanager.UserEvent
	userEventStopCh chan struct{}
	userEventWg     sync.WaitGroup
	userMgrSubCh    <-chan usermanager.UserEvent
	forwardStopCh   chan struct{}
	forwardWg       sync.WaitGroup
	lifecycleMu     sync.Mutex
	closeOnce       sync.Once

	// certManager provides certificate lookup for TLS inbounds.
	// Optional: only required when FastAddInbound is called with a domain parameter.
	certManager CertManagerGetter

	// storeMgr provides unified access to persistence stores.
	storeMgr *store.StoreManager

	// portClaimer is the process-wide port authority. Every inbound listen
	// port goes through it so no forward rule can be handed a port an xray
	// inbound already owns, and vice versa. The ports claimed here are for
	// xray's own 127.0.0.1 listeners; the public relay in front of one is
	// separate and goes through UserManager.GetBindPort -> forward.AddRule.
	// nil in unit tests, which then take ports at face value.
	portClaimer container.PortClaimer

	// Reconcile loop for periodic user sync
	reconcileStopCh chan struct{}
	reconcileWg     sync.WaitGroup

	// routingRunner overrides subprocess execution for xray api routing commands in tests.
	routingRunner cliRunner
}

func (e *Executor) routingCLI() *routingCLIClient {
	return newRoutingCLIClient(e.config.BinaryPath, e.grpcAPIAddress, e.routingRunner)
}

// ExecutorConfig holds executor configuration.
type ExecutorConfig struct {
	BinaryPath     string
	ConfigFilePath string
	ContainerType  container.ContainerType
	VersionRegex   string
	GRPCAPIAddress string // e.g., "127.0.0.1:62789"
	UpdateConfig   UpdaterConfig
	AutoDownload   bool // auto download binary if not exists

	// Debug enables debug logging for gRPC operations
	Debug bool

	// PortClaimer is the process-wide port authority. Inbound listen ports are
	// claimed there so no other component — another container, or the forward
	// layer — can be handed the same port. nil disables claiming, which is only
	// correct for tests that construct an Executor in isolation.
	PortClaimer container.PortClaimer

	// StoreMgr provides unified access to persistence stores (UserStore + InboundStore).
	// When set, inbounds are persisted and Restore() can reload them after restart.
	StoreMgr *store.StoreManager

	// UserManager provides user manager for event handling and periodic sync.
	// When set, the executor subscribes to user events on creation.
	UserManager *usermanager.UserManager

	// CertManager provides certificate lookup for TLS inbounds.
	// Optional: only required when FastAddInbound is called with a domain parameter.
	CertManager CertManagerGetter

	// ReconcileInterval specifies how often to sync users from UserManager to inbounds.
	// Default: 30 seconds. Set to 0 to disable.
	ReconcileInterval time.Duration
}

// executorHooks implements container.Hooks for the Executor.
type executorHooks struct {
	executor *Executor
}

// GetRunFunc returns the run and stop functions for the Xray process.
// This is the key integration point with BaseContainer.
func (h *executorHooks) GetRunFunc() (run func() error, stop func() error) {
	run = func() error {
		// Start the xray process via embedded Runner
		return h.executor.Runner.Start()
	}
	stop = func() error {
		// Stop the xray process via embedded Runner
		return h.executor.Runner.Stop()
	}
	return run, stop
}

// NewExecutor creates a new Xray executor.
func NewExecutor(cfg ExecutorConfig) (*Executor, error) {
	if cfg.BinaryPath == "" {
		return nil, fmt.Errorf("binary path required")
	}
	if cfg.ContainerType == "" {
		cfg.ContainerType = container.ContainerXray
	}
	if cfg.GRPCAPIAddress == "" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			port := ln.Addr().(*net.TCPAddr).Port
			_ = ln.Close()
			cfg.GRPCAPIAddress = fmt.Sprintf("127.0.0.1:%d", port)
		} else {
			cfg.GRPCAPIAddress = "127.0.0.1:62789"
		}
	}

	// Create generic process runner with xray-specific config
	runner, err := process.NewRunner(process.RunnerConfig{
		BinaryPath: cfg.BinaryPath,
		Args:       []string{"run"}, // xray specific: "run" command
		ConfigFile: cfg.ConfigFilePath,
		Stdout:     log.NewPrefixWriter(os.Stdout, string(cfg.ContainerType)),
		Stderr:     log.NewPrefixWriter(os.Stderr, string(cfg.ContainerType)),
	})
	if err != nil {
		return nil, err
	}

	e := &Executor{
		Runner:         runner,
		config:         cfg,
		grpcAPIAddress: cfg.GRPCAPIAddress,
		inbounds:       make(map[string]*XrayInbound),
		renderer:       NewRenderer(),
		storeMgr:       cfg.StoreMgr,
		portClaimer:    cfg.PortClaimer,
	}

	// Wire dependencies from config
	if cfg.UserManager != nil {
		e.userMgr = cfg.UserManager
		e.userEventCh = make(chan usermanager.UserEvent, 100)
		e.forwardStopCh = make(chan struct{})
	}
	if cfg.CertManager != nil {
		e.certManager = cfg.CertManager
	}

	// Initialize BaseContainer with hooks
	hooks := &executorHooks{executor: e}
	e.BaseContainer = container.NewBaseContainer(contracts.ContainerXray, hooks)

	// Initialize updater if:
	// 1. Explicit update config is provided (UpdateConfig.BinaryPath != "")
	// 2. OR AutoDownload is enabled (need default updater for auto-download)
	if cfg.UpdateConfig.BinaryPath != "" || cfg.AutoDownload {
		// Build updater config: use explicit config or create default
		updaterConfig := cfg.UpdateConfig
		if updaterConfig.BinaryPath == "" {
			// Create default updater config based on BinaryPath
			// Default config source:
			// - BinaryPath: uses the executor's BinaryPath
			// - DownloadDir: /tmp (or same directory as binary path)
			// - Repo: XTLS/Xray-core
			// - AssetName: Xray-linux-64.zip
			binaryDir := filepath.Dir(cfg.BinaryPath)
			if binaryDir == "." {
				binaryDir = "/tmp"
			}
			updaterConfig = UpdaterConfig{
				BinaryPath:  cfg.BinaryPath,
				DownloadDir: binaryDir,
				Owner:       "XTLS",
				Repo:        "Xray-core",
				AssetName:   "Xray-linux-64.zip",
				BinaryName:  "xray",
			}
		}

		updater, err := NewUpdater(updaterConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create updater: %w", err)
		}
		updater.SetProcessController(e)
		e.updater = updater
	}

	if cfg.UserManager != nil {
		sub := cfg.UserManager.Subscribe()
		e.userMgrSubCh = sub
		e.forwardWg.Add(1)
		go e.forwardUserEvents(sub, e.forwardStopCh)
	}

	return e, nil
}

// EnsureBinary checks if the xray binary exists.
// If not found and AutoDownload is enabled, it will try to download the binary.
// Returns error if:
// - Binary not found and AutoDownload is false
// - AutoDownload is true but updater is not available (should not happen after NewExecutor fix)
func (e *Executor) EnsureBinary(ctx context.Context) error {
	// Check if binary exists
	cmd := exec.Command(e.config.BinaryPath, "version")
	if err := cmd.Run(); err == nil {
		return nil // Binary exists
	}

	// Binary not found
	// Ensure DownloadDir exists before auto-download
	downloadDir := filepath.Dir(e.config.BinaryPath)
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return fmt.Errorf("create download dir %s: %w", downloadDir, err)
	}

	// Binary not found
	if e.config.AutoDownload {
		// Strong validation: AutoDownload=true must have updater
		if e.updater == nil {
			return fmt.Errorf("auto-download is enabled but updater is not available; please configure UpdateConfig or use NewExecutor with AutoDownload=true")
		}

		// Auto-download binary
		targetTag := e.config.VersionRegex
		if targetTag == "" {
			targetTag = "latest"
		}

		log.Info("[Xray Executor] binary not found, starting auto-download", "path", e.config.BinaryPath, "target", targetTag)

		_, err := e.updater.Update(ctx, container.UpdateRequest{
			TargetTag:     targetTag,
			RestartPolicy: container.RestartPolicyNever, // Don't start process during auto-download
		})
		if err != nil {
			log.Error("[Xray Executor] auto-download failed", "err", err)
			return fmt.Errorf("failed to auto-download xray: %w", err)
		}

		log.Info("[Xray Executor] auto-download successful", "path", e.config.BinaryPath)
		return nil
	}

	return fmt.Errorf("xray binary not found at %s", e.config.BinaryPath)
}

// EnsureConfig checks if a valid config file exists.
// If present on disk, ensures api.services contains "RoutingService" (updating
// the file if missing) and emits warnings if outbounds[0] is not "direct" or
// routing.rules ends with a catch-all rule that would shadow appended chain rules.
// If not provided or missing, generates a default config with API enabled.
func (e *Executor) EnsureConfig() error {
	if e.config.ConfigFilePath != "" {
		if info, err := os.Stat(e.config.ConfigFilePath); err == nil {
			return e.prepareExistingConfig(e.config.ConfigFilePath, info.Mode().Perm())
		}
	}

	// Generate default config
	return e.generateDefaultConfig()
}

func (e *Executor) prepareExistingConfig(configPath string, perm os.FileMode) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read existing xray config %s: %w", configPath, err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("failed to parse existing xray config %s: %w", configPath, err)
	}
	if raw == nil {
		raw = map[string]interface{}{}
	}

	needsWrite := false
	apiMap, ok := raw["api"].(map[string]interface{})
	if !ok || apiMap == nil {
		raw["api"] = map[string]interface{}{
			"tag":      APIInboundTag,
			"services": []string{"HandlerService", "LoggerService", "StatsService", "RoutingService"},
		}
		needsWrite = true
	} else {
		if _, hasTag := apiMap["tag"]; !hasTag {
			apiMap["tag"] = APIInboundTag
			needsWrite = true
		}
		rawServices, hasServices := apiMap["services"].([]interface{})
		if !hasServices {
			apiMap["services"] = []string{"HandlerService", "LoggerService", "StatsService", "RoutingService"}
			needsWrite = true
		} else {
			hasRouting := false
			for _, svc := range rawServices {
				if s, _ := svc.(string); s == "RoutingService" {
					hasRouting = true
					break
				}
			}
			if !hasRouting {
				apiMap["services"] = append(rawServices, "RoutingService")
				needsWrite = true
			}
		}
	}

	if needsWrite {
		updated, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal updated xray config %s: %w", configPath, err)
		}
		updated = append(updated, '\n')
		if perm == 0 {
			perm = 0644
		}
		if err := os.WriteFile(configPath, updated, perm); err != nil {
			return fmt.Errorf("failed to write updated xray config %s: %w", configPath, err)
		}
	}

	// Warn if outbounds[0] is not "direct" (default fallback could misroute).
	warnOutbound0 := false
	if outbounds, ok := raw["outbounds"].([]interface{}); !ok || len(outbounds) == 0 {
		warnOutbound0 = true
	} else if first, ok := outbounds[0].(map[string]interface{}); !ok {
		warnOutbound0 = true
	} else if tag, _ := first["tag"].(string); tag != "direct" {
		warnOutbound0 = true
	}
	if warnOutbound0 {
		log.Warn("[EnsureConfig] xray config outbounds[0] is not tagged \"direct\"; direct traffic without matching routing rules may misroute", "config", configPath)
	}

	// Warn if routing.rules ends with a catch-all rule that shadows -append chain rules.
	if routing, ok := raw["routing"].(map[string]interface{}); ok {
		if rules, ok := routing["rules"].([]interface{}); ok && len(rules) > 0 {
			if lastRule, ok := rules[len(rules)-1].(map[string]interface{}); ok && isCatchAllRoutingRule(lastRule) {
				log.Warn("[EnsureConfig] xray config routing.rules ends with a catch-all rule that may shadow dynamically appended chain rules", "config", configPath)
			}
		}
	}

	return nil
}

func isCatchAllRoutingRule(rule map[string]interface{}) bool {
	if rule == nil {
		return false
	}
	outTag, _ := rule["outboundTag"].(string)
	balTag, _ := rule["balancerTag"].(string)
	if strings.TrimSpace(outTag) == "" && strings.TrimSpace(balTag) == "" {
		return false
	}

	for _, key := range []string{"inboundTag", "domain", "source", "sourceIP", "user", "protocol", "attrs", "sourcePort"} {
		if hasNonEmptyRuleSelector(rule[key]) {
			return false
		}
	}

	if ipVal, exists := rule["ip"]; exists && hasNonEmptyRuleSelector(ipVal) {
		if !isAllIPSelector(ipVal) {
			return false
		}
	}

	if portVal, exists := rule["port"]; exists && portVal != nil {
		portStr := strings.TrimSpace(fmt.Sprint(portVal))
		if portStr != "" && portStr != "0-65535" && portStr != "1-65535" {
			return false
		}
	}

	if netVal, exists := rule["network"]; exists && netVal != nil {
		netStr := strings.ToLower(strings.TrimSpace(fmt.Sprint(netVal)))
		if netStr != "" {
			hasTCP := strings.Contains(netStr, "tcp")
			hasUDP := strings.Contains(netStr, "udp")
			if !(hasTCP && hasUDP) {
				return false
			}
		}
	}

	return true
}

func hasNonEmptyRuleSelector(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case string:
		return strings.TrimSpace(val) != ""
	case []string:
		return len(val) > 0
	case []interface{}:
		return len(val) > 0
	case map[string]interface{}:
		return len(val) > 0
	default:
		return true
	}
}

func isAllIPSelector(v interface{}) bool {
	items, ok := v.([]interface{})
	if !ok {
		return false
	}
	hasV4, hasV6 := false, false
	for _, item := range items {
		s, _ := item.(string)
		switch strings.TrimSpace(s) {
		case "0.0.0.0/0":
			hasV4 = true
		case "::/0":
			hasV6 = true
		}
	}
	return hasV4 && hasV6
}

// killProcessOnPort kills any process listening on the given TCP port.
// Uses /proc/net/tcp (Linux) to find the PID, then sends SIGKILL.
// Returns nil if no process is found or if kill succeeds.
func killProcessOnPort(port int) error {
	// Read /proc/net/tcp for listening sockets
	data, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		// Not Linux or no access — skip silently
		return nil
	}

	// Format: sl  local_address rem_address st tx_queue:rx_queue ...
	// local_address is hex "IPADDR:PORT" in little-endian
	hexPort := fmt.Sprintf("%04X", port)
	var inodes []string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		local := fields[1] // e.g. "0100007F:F5B5"
		parts := strings.Split(local, ":")
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(parts[1], hexPort) {
			inodes = append(inodes, fields[9]) // inode field
		}
	}

	if len(inodes) == 0 {
		return nil // nothing listening on that port
	}

	// Walk /proc/*/fd to find PID owning the inode
	inodeSet := make(map[string]bool)
	for _, ino := range inodes {
		inodeSet[ino] = true
	}

	procDirs, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	killed := false
	for _, fdPath := range procDirs {
		target, err := os.Readlink(fdPath)
		if err != nil {
			continue
		}
		// target looks like "socket:[12345]"
		if !strings.HasPrefix(target, "socket:[") {
			continue
		}
		ino := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
		if !inodeSet[ino] {
			continue
		}
		// Extract PID from path /proc/<pid>/fd/<n>
		parts := strings.Split(fdPath, "/")
		if len(parts) < 3 {
			continue
		}
		pidInt := 0
		if _, err := fmt.Sscanf(parts[2], "%d", &pidInt); err != nil || pidInt <= 1 {
			continue
		}
		proc, err := os.FindProcess(pidInt)
		if err != nil {
			continue
		}
		if err := proc.Kill(); err != nil {
			log.Warn("[killProcessOnPort] kill failed", "pid", pidInt, "err", err)
		} else {
			log.Info("[killProcessOnPort] killed stale process", "pid", pidInt, "port", port)
			killed = true
		}
	}

	if killed {
		// Brief pause to let the kernel release the port
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

// parsePortFromAddr extracts the port number from a "host:port" address.
// Falls back to 62789 if parsing fails.
func parsePortFromAddr(addr string) int {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return 62789
	}
	port, err := strconv.Atoi(strings.TrimSpace(addr[idx+1:]))
	if err != nil {
		return 62789
	}
	return port
}

// generateDefaultConfig creates a minimal xray config with API enabled.
func (e *Executor) generateDefaultConfig() error {
	// Create a temp config file
	tmpDir := os.TempDir()
	configPath := filepath.Join(tmpDir, "xray-"+time.Now().Format("20060102150405")+".json")

	// Generate minimal config with API
	model := contracts.ContainerModel{
		Type:     contracts.ContainerXray,
		APIPort:  62789,                     // 固定端口，与 GRPCAPIAddress 默认值保持一致
		Inbounds: []contracts.InboundSpec{}, // 空，不创建默认 inbound
	}

	nativeConfig, err := e.renderer.ToProvider(model)
	if err != nil {
		return fmt.Errorf("failed to generate default config: %w", err)
	}

	if err := os.WriteFile(configPath, nativeConfig.JSON, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	e.config.ConfigFilePath = configPath
	e.Runner.SetConfig(process.RunnerConfig{
		BinaryPath: e.config.BinaryPath,
		Args:       []string{"run"},
		ConfigFile: configPath,
		Stdout:     log.NewPrefixWriter(os.Stdout, string(e.config.ContainerType)),
		Stderr:     log.NewPrefixWriter(os.Stderr, string(e.config.ContainerType)),
	})

	return nil
}

// Start runs the full startup sequence:
// 0. Check usermanager is set (required)
// 1. Start user event handler and reconcile loop
// 2. Ensure binary exists
// 3. Ensure config exists
// 4. Start the process
func (e *Executor) Start() error {
	// Step 0: Check usermanager is set (required for user management)
	if e.userMgr == nil {
		return errs.New(errs.ErrUserManagerRequired,
			"usermanager is required to start container; call SetUserManager before Start")
	}

	// Step 0b: Kill any stale xray process occupying the gRPC port.
	// This can happen when the previous demo run was killed (SIGKILL) and the
	// xray child process became an orphan. If we don't kill it, the new xray
	// process will bind the same config port and the stale one's in-memory state
	// (including inbound tags) will be visible on the gRPC API.
	if e.grpcAPIAddress != "" {
		if err := killProcessOnPort(parsePortFromAddr(e.grpcAPIAddress)); err != nil {
			log.Warn("[Start] failed to kill stale process on port",
				"port", parsePortFromAddr(e.grpcAPIAddress), "err", err)
		}
	}

	// Step 1: Start user event handler (for checkUser sync)
	// This must be done before starting to process user events
	startedHandler := e.startUserEventHandler()

	// Step 1b: Start periodic user reconciliation
	startedReconcile := e.startReconcileLoop()

	started := false
	defer func() {
		if !started {
			if startedReconcile {
				e.stopReconcileLoop()
			}
			if startedHandler {
				e.stopUserEventHandler()
			}
		}
	}()

	// Step 2: Ensure binary exists
	if err := e.EnsureBinary(context.Background()); err != nil {
		return fmt.Errorf("binary check failed: %w", err)
	}

	// Step 3: Ensure config exists
	if err := e.EnsureConfig(); err != nil {
		return fmt.Errorf("config check failed: %w", err)
	}

	// Step 4: Start the process
	if err := e.Runner.Start(); err != nil {
		return err
	}

	// Give the OS a moment to schedule the xray process before returning.
	// Without this delay, subsequent gRPC calls may fail because xray hasn't
	// begun execution yet.
	time.Sleep(500 * time.Millisecond)
	started = true
	return nil
}

// Reload triggers config reload via restart and restores persisted inbounds.
func (e *Executor) Reload() error {
	if err := e.BaseContainer.Restart(); err != nil {
		return err
	}
	e.waitForAPIReadyBeforeRestore()
	return e.Restore(context.Background())
}

// IsRunning checks if the Xray process is running.
// This is needed to satisfy ProcessController interface used by Updater.
func (e *Executor) IsRunning() bool {
	return e.Runner.IsRunning()
}

// Stop stops the Xray process directly (for Updater).
func (e *Executor) Stop() error {
	// Stop background loops before stopping the process.
	e.stopReconcileLoop()
	e.stopUserEventHandler()
	return e.Runner.Stop()
}

// Close tears down background loops and the UserManager event subscription
// started at construction. Safe to call multiple times and concurrently.
func (e *Executor) Close() {
	e.closeOnce.Do(func() {
		e.stopReconcileLoop()
		e.stopUserEventHandler()

		e.lifecycleMu.Lock()
		stopCh := e.forwardStopCh
		sub := e.userMgrSubCh
		e.userMgrSubCh = nil
		e.lifecycleMu.Unlock()

		if stopCh != nil {
			close(stopCh)
		}
		if e.userMgr != nil && sub != nil {
			e.userMgr.Unsubscribe(sub)
		}
		e.forwardWg.Wait()
	})
}

// Restart restarts the Xray process directly (for Container interface)
// and replays persisted inbounds and chain routing state.
func (e *Executor) Restart() error {
	if err := e.Runner.Restart(); err != nil {
		return err
	}
	e.waitForAPIReadyBeforeRestore()
	return e.Restore(context.Background())
}

func (e *Executor) waitForAPIReadyBeforeRestore() {
	if e.storeMgr == nil || e.grpcAPIAddress == "" {
		return
	}
	recs, err := e.storeMgr.InboundStore().Load()
	if err != nil {
		return
	}
	hasXray := false
	for _, r := range recs {
		if r.ContainerType == string(contracts.ContainerXray) && r.Tag != APIInboundTag {
			hasXray = true
			break
		}
	}
	if !hasXray {
		return
	}
	if err := NewXrayAPI(e.grpcAPIAddress, e.config.Debug).WaitForReady(5 * time.Second); err != nil {
		log.Warn("[Restart] xray gRPC API not ready before restore", "addr", e.grpcAPIAddress, "err", err)
	}
}

// Type returns the container type.
func (e *Executor) Type() container.ContainerType {
	return e.config.ContainerType
}

// ConfigFile returns the config file path.
func (e *Executor) ConfigFile() string {
	return e.config.ConfigFilePath
}

// GRPCAddress returns the gRPC API address.
func (e *Executor) GRPCAddress() string {
	return e.grpcAPIAddress
}

// Version returns the Xray version.
// This is xray-specific as different proxies have different version commands.
func (e *Executor) Version() string {
	if !e.BaseContainer.IsRunning() {
		return ""
	}
	cmd := exec.Command(e.config.BinaryPath, "version")
	out, _ := cmd.Output()
	return string(out)
}

// Update updates the Xray binary.
func (e *Executor) Update(ctx context.Context, req container.UpdateRequest) (*container.UpdateResult, error) {
	if e.updater != nil {
		return e.updater.Update(ctx, req)
	}
	return &container.UpdateResult{
		FromVersion: e.Version(),
		ToVersion:   req.TargetTag,
	}, nil
}

// Inbound management implementation

// RemoveInboundConfig removes an inbound from the container.
// It first releases all user ports associated with this inbound (synchronous).
// Uses two-phase lock: read inbound, release outside lock, then delete.
func (e *Executor) RemoveInboundConfig(tag string) error {
	// Phase 1: First lock - read and get inbound reference
	e.inboundsMu.Lock()
	xrayIn, exists := e.inbounds[tag]
	e.inboundsMu.Unlock()

	if !exists {
		return fmt.Errorf("inbound %s not found", tag)
	}

	// Phase 2: Release all user ports (outside lock)
	_ = xrayIn.ReleaseAllUserPorts()

	// Phase 3: Delete persistent store first — ensures restart won't resurrect the inbound
	if e.storeMgr != nil {
		if err := e.storeMgr.InboundStore().Delete(tag); err != nil {
			log.Warn("[RemoveInboundConfig] failed to delete inbound from store", "tag", tag, "err", err)
		}
	}

	// Phase 4: Remove from xray runtime via gRPC (idempotent)
	api := NewXrayAPI(e.grpcAPIAddress)
	if err := api.RemoveInbound(tag); err != nil {
		log.Warn("[RemoveInboundConfig] failed to remove inbound via gRPC, continuing with local cleanup", "tag", tag, "err", err)
	}

	// Phase 4b: Remove chain routing rule and outbound after inbound removal
	if xrayIn.GetInboundType() == contracts.InboundTypeChain {
		ctx := context.Background()
		cli := e.routingCLI()
		if err := cli.RemoveRule(ctx, chainRuleTag(tag)); err != nil {
			log.Warn("[RemoveInboundConfig] failed to remove chain rule, continuing cleanup", "tag", tag, "ruleTag", chainRuleTag(tag), "err", err)
		}
		if err := cli.RemoveOutbound(ctx, chainOutboundTag(tag)); err != nil {
			log.Warn("[RemoveInboundConfig] failed to remove chain outbound, continuing cleanup", "tag", tag, "outboundTag", chainOutboundTag(tag), "err", err)
		}
	}

	// Phase 5: Delete from memory
	e.inboundsMu.Lock()
	defer e.inboundsMu.Unlock()

	if _, exists := e.inbounds[tag]; !exists {
		return nil
	}

	// Clean up temporary cert/key files
	for _, f := range xrayIn.tempCertFiles {
		if f != "" {
			_ = os.Remove(f)
		}
	}

	delete(e.inbounds, tag)

	// Give the listen port back to the shared allocator. After the store delete
	// (phase 3) on purpose: a claim outlives everything except the record
	// itself, so releasing while the record still existed would let a forward
	// rule take a port this inbound would reclaim on the next restart.
	container.ReleaseInboundPort(e.portClaimer, xrayIn.Port())

	return nil
}

// GetInboundConfig returns an inbound by tag.
func (e *Executor) GetInboundConfig(tag string) (inbound.Inbound, error) {
	e.inboundsMu.RLock()
	defer e.inboundsMu.RUnlock()

	in, exists := e.inbounds[tag]
	if !exists {
		return nil, fmt.Errorf("inbound %s not found", tag)
	}
	return in, nil
}

// ListInboundConfigs returns all inbounds.
func (e *Executor) ListInboundConfigs() []inbound.Inbound {
	e.inboundsMu.RLock()
	defer e.inboundsMu.RUnlock()

	result := make([]inbound.Inbound, 0, len(e.inbounds))
	for _, in := range e.inbounds {
		result = append(result, in)
	}
	return result
}

type inboundAddOptions struct {
	meta          contracts.InboundMeta
	certSource    string
	certDomain    string
	tempCertFiles []string
	persist       bool
	reconcile     bool
}

// AddInbound adds an inbound via gRPC (for RuntimeAPI compatibility).
// After a successful gRPC call, the inbound is also registered in the in-memory
// map so that GetUserSubscriptions and other local queries can find it.
// If an InboundRecord already exists in the store for this tag, its metadata
// (inbound_type, access_tags, chain_outbound_uri, remark, cert_source, cert_domain)
// is preserved instead of being overwritten with blank defaults.
func (e *Executor) AddInboundNative(nativeInboundJSON []byte) error {
	opts := inboundAddOptions{
		meta: contracts.InboundMeta{
			InboundType: contracts.InboundTypeDirect,
		},
		certSource: "none",
		persist:    true,
		reconcile:  true,
	}

	if e.storeMgr != nil {
		var peek struct {
			Tag string `json:"tag"`
		}
		if err := json.Unmarshal(nativeInboundJSON, &peek); err == nil && peek.Tag != "" {
			if existingRec, err := e.storeMgr.InboundStore().Get(peek.Tag); err == nil && existingRec != nil {
				it, normErr := contracts.NormalizeInboundType(existingRec.InboundType)
				if normErr != nil {
					it = contracts.InboundTypeDirect
				}
				opts.meta = contracts.InboundMeta{
					InboundType:      it,
					AccessTags:       slices.Clone(existingRec.AccessTags),
					ChainOutboundURI: existingRec.ChainOutboundURI,
					Remark:           existingRec.Remark,
				}
				if existingRec.CertSource != "" {
					opts.certSource = existingRec.CertSource
				}
				opts.certDomain = existingRec.CertDomain
			}
		}
	}

	return e.addInboundNativeInternal(nativeInboundJSON, opts)
}

func (e *Executor) addInboundNativeInternal(nativeInboundJSON []byte, opts inboundAddOptions) error {
	api := NewXrayAPI(e.grpcAPIAddress, e.config.Debug)
	if err := api.AddInbound(nativeInboundJSON); err != nil {
		return err
	}

	// Parse native JSON to register inbound in the local map.
	var raw map[string]interface{}
	if err := json.Unmarshal(nativeInboundJSON, &raw); err != nil {
		// gRPC succeeded; log parse failure but don't fail the caller.
		log.Warn("[AddInboundNative] failed to parse native JSON for local registration", "err", err)
		return nil
	}

	tag, _ := raw["tag"].(string)
	if tag == "" {
		return nil
	}

	protocolStr, _ := raw["protocol"].(string)
	protocol := contracts.Protocol(protocolStr)

	var port uint32
	switch v := raw["port"].(type) {
	case string:
		fmt.Sscanf(v, "%d", &port)
	case float64:
		port = uint32(v)
	}

	listenAddr, _ := raw["listen"].(string)

	transport := contracts.TransportTCP
	security := contracts.SecurityNone
	if ss, ok := raw["streamSettings"].(map[string]interface{}); ok {
		if n, ok := ss["network"].(string); ok {
			// xray-core uses "splithttp" as the network name internally;
			// normalize to "xhttp" so our layer always uses a single canonical value.
			if n == "splithttp" {
				n = "xhttp"
			}
			transport = contracts.Transport(n)
		}
		if s, ok := ss["security"].(string); ok {
			security = contracts.Security(s)
		}
	}

	// Extract default client UUID from settings.clients[0].id
	// This UUID is used for VMess/VLESS subscription generation
	defaultClientUUID := ""
	// Extract default password from settings for Trojan/Shadowsocks
	// - Trojan: settings.clients[0].password
	// - Shadowsocks: settings.password
	defaultPassword := ""
	if settings, ok := raw["settings"].(map[string]interface{}); ok {
		// VMess/VLESS: extract UUID from clients[0].id
		if clients, ok := settings["clients"].([]interface{}); ok && len(clients) > 0 {
			if firstClient, ok := clients[0].(map[string]interface{}); ok {
				if uuid, ok := firstClient["id"].(string); ok {
					defaultClientUUID = uuid
				}
				// Trojan: extract password from clients[0].password
				if protocol == contracts.ProtocolTrojan {
					if password, ok := firstClient["password"].(string); ok {
						defaultPassword = password
					}
				}
			}
		}
		// Shadowsocks: extract password directly from settings.password
		if protocol == contracts.ProtocolShadowsocks {
			if password, ok := settings["password"].(string); ok {
				defaultPassword = password
			}
		}
	}

	// Surface (but do not reject on) a credential-less vmess/vless/trojan inbound
	// restored from the store: it predates the FastAddInbound guard and is a dead
	// inbound whose empty credential poisons subscriptions. We keep restoring it
	// so a batch restore isn't aborted, but operators should delete and recreate
	// it. Rejecting here could turn a subscription issue into a startup failure.
	if credentialLessInbound(protocol, defaultClientUUID, defaultPassword) {
		log.Warnf("xray: restored credential-less %s inbound %q (dead inbound; recreate it to fix subscriptions)", protocol, tag)
	}

	// Build extra map from native JSON for subscription URI generation.
	// Without extra, subscription URIs won't contain transport-specific params.
	extra := extractNativeExtra(raw, transport, security)

	inboundType := opts.meta.InboundType
	if inboundType == "" {
		inboundType = contracts.InboundTypeDirect
	}
	certSource := opts.certSource
	if certSource == "" {
		certSource = "none"
	}
	if certSource != "none" {
		extra["cert_source"] = certSource
	}

	xi := &XrayInbound{
		tag:               tag,
		protocol:          protocol,
		port:              port,
		listenAddr:        listenAddr,
		security:          security,
		transport:         transport,
		nativeJSON:        nativeInboundJSON,
		defaultClientUUID: defaultClientUUID,
		defaultPassword:   defaultPassword,
		extra:             extra,
		tempCertFiles:     slices.Clone(opts.tempCertFiles),
		certSource:        certSource,
		certDomain:        opts.certDomain,
		inboundType:       inboundType,
		accessTags:        slices.Clone(opts.meta.AccessTags),
		chainOutboundURI:  opts.meta.ChainOutboundURI,
		remark:            opts.meta.Remark,
		addedUsers:        make(map[string]struct{}),
	}

	// Inject user manager dependency
	if e.userMgr != nil {
		xi.SetUserManager(e.userMgr)
	}

	// Avoid duplicate ClaimInboundPort noise when Restore replaces an already-tracked
	// inbound on the same port (e.g. after Executor.Restart or binary upgrade).
	e.inboundsMu.RLock()
	existingIn, hadExisting := e.inbounds[tag]
	samePortAsExisting := hadExisting && existingIn.Port() == port
	e.inboundsMu.RUnlock()

	if port != 0 && !samePortAsExisting {
		if _, err := container.ClaimInboundPort(e.portClaimer, port); err != nil {
			log.Warn("[AddInboundNative] could not record inbound port with the shared allocator; "+
				"it may be handed to a forward rule later", "tag", tag, "port", port, "err", err)
		}
	}

	e.inboundsMu.Lock()
	replaced := e.inbounds[tag]
	if replaced != nil && replaced.Port() == port {
		for _, u := range replaced.listAddedUsers() {
			xi.addedUsers[u] = struct{}{}
		}
	}
	e.inbounds[tag] = xi
	e.inboundsMu.Unlock()

	// Clean up replaced inbound's temp cert files and changed port claim outside lock.
	if replaced != nil {
		newCertSet := make(map[string]struct{}, len(xi.tempCertFiles))
		for _, f := range xi.tempCertFiles {
			if f != "" {
				newCertSet[f] = struct{}{}
			}
		}
		for _, f := range replaced.tempCertFiles {
			if f == "" {
				continue
			}
			if _, kept := newCertSet[f]; !kept {
				_ = os.Remove(f)
			}
		}
		if replaced.Port() != 0 && replaced.Port() != port {
			container.ReleaseInboundPort(e.portClaimer, replaced.Port())
		}
	}

	// Persist to store (best effort — log only, no rollback on failure)
	if opts.persist && e.storeMgr != nil && tag != "" {
		rec := xi.toInboundRecord()
		if err := e.storeMgr.InboundStore().Save(rec); err != nil {
			log.Warn("[AddInboundNative] failed to persist inbound", "tag", tag, "err", err)
		}
	}

	// Sync existing users to this new inbound
	if opts.reconcile {
		e.reconcileUsersForInbound(tag)
	}

	return nil
}

// SupportsChainOutbound implements container.ChainCapable.
func (e *Executor) SupportsChainOutbound() bool {
	return true
}

// ValidateChainURI implements container.ChainCapable by parsing rawURI and verifying
// that Xray can render an outbound configuration for it.
func (e *Executor) ValidateChainURI(rawURI string) error {
	_, _, err := parseAndRenderXrayChainOutbound("__validate__", rawURI)
	return err
}

// SetInboundAccessTags implements container.InboundTagUpdater.
// It updates the inbound's access_tags in memory and persistent store without
// restarting or reloading xray, rolling back memory on store failure, and
// immediately runs bidirectional user reconciliation for the inbound.
func (e *Executor) SetInboundAccessTags(tag string, accessTags []string) error {
	if strings.TrimSpace(tag) == "" {
		return errs.Newf(errs.ErrInvalidInboundSpec, "inbound tag is required")
	}
	normalized, err := contracts.NormalizeAccessTags(accessTags)
	if err != nil {
		return errs.Wrap(errs.ErrInvalidInboundSpec, "invalid access_tags", err)
	}

	e.inboundsMu.Lock()
	xi, exists := e.inbounds[tag]
	if !exists {
		e.inboundsMu.Unlock()
		return errs.Newf(errs.ErrInboundNotFound, "inbound %s not found", tag)
	}

	prevTags := xi.GetAccessTags()
	xi.SetAccessTags(normalized)

	if e.storeMgr != nil {
		rec, getErr := e.storeMgr.InboundStore().Get(tag)
		if getErr != nil {
			xi.SetAccessTags(prevTags)
			e.inboundsMu.Unlock()
			return errs.Wrap(errs.ErrInternal, "failed to load inbound record for tag update", getErr)
		}
		if rec == nil {
			rec = xi.toInboundRecord()
		} else {
			rec.AccessTags = slices.Clone(normalized)
		}
		if saveErr := e.storeMgr.InboundStore().Save(rec); saveErr != nil {
			xi.SetAccessTags(prevTags)
			e.inboundsMu.Unlock()
			return errs.Wrap(errs.ErrInternal, "failed to persist inbound access_tags", saveErr)
		}
	}
	e.inboundsMu.Unlock()

	e.reconcileUsersForInbound(tag)
	return nil
}

// GetInboundMeta implements container.InboundMetaProvider.
func (e *Executor) GetInboundMeta(tag string) (contracts.InboundMeta, bool) {
	e.inboundsMu.RLock()
	xi, exists := e.inbounds[tag]
	e.inboundsMu.RUnlock()
	if !exists {
		return contracts.InboundMeta{}, false
	}
	return xi.GetMeta(), true
}

// extractNativeExtra parses the native xray JSON to extract subscription-relevant fields.
func extractNativeExtra(raw map[string]interface{}, transport contracts.Transport, security contracts.Security) map[string]interface{} {
	extra := map[string]interface{}{
		"transport": string(transport),
		"security":  string(security),
	}

	// Extract method from settings (for Shadowsocks)
	if settings, ok := raw["settings"].(map[string]interface{}); ok {
		if method, ok := settings["method"].(string); ok {
			extra["method"] = method
		}
		// Extract flow from VLESS clients (aligned with Xray-core issue #91)
		if clients, ok := settings["clients"].([]interface{}); ok && len(clients) > 0 {
			if client0, ok := clients[0].(map[string]interface{}); ok {
				if flow, ok := client0["flow"].(string); ok && flow != "" {
					extra["flow"] = flow
				}
			}
		}
	}

	ss, ok := raw["streamSettings"].(map[string]interface{})
	if !ok {
		return extra
	}

	// WebSocket settings
	if wsSettings, ok := ss["wsSettings"].(map[string]interface{}); ok {
		if path, ok := wsSettings["path"].(string); ok {
			extra["ws_path"] = path
		}
		if headers, ok := wsSettings["headers"].(map[string]interface{}); ok {
			if host, ok := headers["Host"].(string); ok {
				extra["ws_host"] = host
			}
		}
	}

	// gRPC settings
	if grpcSettings, ok := ss["grpcSettings"].(map[string]interface{}); ok {
		if sn, ok := grpcSettings["serviceName"].(string); ok {
			extra["grpc_service_name"] = sn
		}
	}

	// HTTPUpgrade settings
	if huSettings, ok := ss["httpupgradeSettings"].(map[string]interface{}); ok {
		if path, ok := huSettings["path"].(string); ok {
			extra["httpupgrade_path"] = path
		}
		if host, ok := huSettings["host"].(string); ok {
			extra["httpupgrade_host"] = host
		}
	}

	// SplitHTTP / XHTTP settings
	if shSettings, ok := ss["splithttpSettings"].(map[string]interface{}); ok {
		if path, ok := shSettings["path"].(string); ok {
			extra["xhttp_path"] = path
		}
		if host, ok := shSettings["host"].(string); ok {
			extra["xhttp_host"] = []string{host}
		}
		if mode, ok := shSettings["mode"].(string); ok {
			extra["xhttp_mode"] = mode
		}
	}

	// TLS settings
	if tlsSettings, ok := ss["tlsSettings"].(map[string]interface{}); ok {
		if sn, ok := tlsSettings["serverName"].(string); ok {
			extra["server_name"] = sn
		}
		if fp, ok := tlsSettings["fingerprint"].(string); ok {
			extra["utls_fingerprint"] = fp
		}
	}

	// Reality settings
	if realitySettings, ok := ss["realitySettings"].(map[string]interface{}); ok {
		if serverNames, ok := realitySettings["serverNames"].([]interface{}); ok && len(serverNames) > 0 {
			names := make([]string, 0, len(serverNames))
			for _, n := range serverNames {
				if s, ok := n.(string); ok {
					names = append(names, s)
				}
			}
			if len(names) > 0 {
				extra["server_name"] = names[0] // keep for backward compat
				extra["reality_server_names"] = names
			}
		}
		if shortIds, ok := realitySettings["shortIds"].([]interface{}); ok && len(shortIds) > 0 {
			sids := make([]string, 0, len(shortIds))
			for _, s := range shortIds {
				if sid, ok := s.(string); ok {
					sids = append(sids, sid)
				}
			}
			if len(sids) > 0 {
				extra["reality_short_ids"] = sids
			}
		}
		// Save private key for persistence (internal use only, not for subscription)
		if pk, ok := realitySettings["privateKey"].(string); ok && pk != "" {
			extra["reality_private_key"] = pk
			// Derive public key from private key for subscription URI
			if pubKey, err := DeriveRealityPublicKey(pk); err == nil {
				extra["reality_public_key"] = pubKey
			}
		}
	}

	return extra
}

// RemoveInboundNative removes an inbound via gRPC and cleans up local state.
// It releases all user ports associated with this inbound before removing.
// Uses two-phase lock: read inbound, release outside lock, then delete.
func (e *Executor) RemoveInboundNative(tag string) error {
	// Phase 1: First lock - read and get inbound reference
	e.inboundsMu.Lock()
	xrayIn, exists := e.inbounds[tag]
	e.inboundsMu.Unlock()

	if !exists {
		// Try to remove via gRPC anyway (idempotent)
		api := NewXrayAPI(e.grpcAPIAddress)
		_ = api.RemoveInbound(tag)
		return nil
	}

	// Phase 2: Release all user ports (outside lock)
	// This ensures synchronous completion before gRPC call
	_ = xrayIn.ReleaseAllUserPorts()

	// Remove via gRPC
	api := NewXrayAPI(e.grpcAPIAddress)
	if err := api.RemoveInbound(tag); err != nil {
		// Even if gRPC fails, we should clean up local state
		// Phase 3: Second lock - verify and delete
		e.inboundsMu.Lock()
		delete(e.inbounds, tag)
		e.inboundsMu.Unlock()
		return err
	}

	// Phase 3: Second lock - verify and delete
	e.inboundsMu.Lock()
	defer e.inboundsMu.Unlock()

	// Verify inbound still exists before deleting
	if _, exists := e.inbounds[tag]; !exists {
		return nil // Already deleted by another operation
	}

	delete(e.inbounds, tag)
	return nil
}

// AddUser adds a user to an inbound via gRPC.
func (e *Executor) AddUser(tag string, user contracts.UserSpec) error {
	api := NewXrayAPI(e.grpcAPIAddress)
	return api.AddUser(tag, user)
}

// RemoveUser removes a user from an inbound via gRPC.
func (e *Executor) RemoveUser(tag string, email string) error {
	api := NewXrayAPI(e.grpcAPIAddress)
	return api.RemoveUser(tag, email)
}

// QueryStats queries traffic statistics via gRPC.
func (e *Executor) QueryStats(pattern string, reset bool) (map[string]*contracts.Stats, error) {
	api := NewXrayAPI(e.grpcAPIAddress)
	return api.QueryStats(pattern, reset)
}

// UserEventChannel returns the container's channel for receiving user events.
// The container should distribute events to appropriate inbounds.
func (e *Executor) UserEventChannel() <-chan usermanager.UserEvent {
	return e.userEventCh
}

// forwardUserEvents forwards events from UserManager to local channel.
func (e *Executor) forwardUserEvents(source <-chan usermanager.UserEvent, stopCh <-chan struct{}) {
	defer e.forwardWg.Done()
	for {
		select {
		case event, ok := <-source:
			if !ok {
				return
			}
			if e.userEventCh != nil {
				select {
				case e.userEventCh <- event:
				default:
					// Channel full, event dropped
				}
			}
		case <-stopCh:
			return
		}
	}
}

// startUserEventHandler starts a goroutine to process user events from the channel.
// It is idempotent so repeated Start() calls cannot spawn duplicate consumers,
// returning true only when a new handler loop was started.
func (e *Executor) startUserEventHandler() bool {
	if e.userEventCh == nil {
		return false
	}

	e.lifecycleMu.Lock()
	if e.userEventStopCh != nil {
		e.lifecycleMu.Unlock()
		return false
	}
	stopCh := make(chan struct{})
	e.userEventStopCh = stopCh
	e.userEventWg.Add(1)
	e.lifecycleMu.Unlock()

	go func(stopCh <-chan struct{}) {
		defer e.userEventWg.Done()
		for {
			select {
			case <-stopCh:
				return
			default:
			}
			select {
			case <-stopCh:
				return
			case event, ok := <-e.userEventCh:
				if !ok {
					return
				}
				e.handleUserEvent(event)
			}
		}
	}(stopCh)
	return true
}

// stopUserEventHandler stops the user event handler goroutine and waits for it to exit.
func (e *Executor) stopUserEventHandler() {
	e.lifecycleMu.Lock()
	stopCh := e.userEventStopCh
	e.userEventStopCh = nil
	e.lifecycleMu.Unlock()

	if stopCh != nil {
		close(stopCh)
		e.userEventWg.Wait()
	}
}

// handleUserEvent processes a single user event from usermanager.
// This implements two-layer access control (Node group + Inbound access_tags):
// - Add/Update: evaluate CanUserAccessInbound per inbound -> AddUser if allowed, RemoveUser if denied
// - Remove/Expire: remove user from all inbounds -> release forward ports
func (e *Executor) handleUserEvent(event usermanager.UserEvent) error {
	if e.userMgr == nil {
		return errs.New(errs.ErrUserManagerRequired,
			"usermanager is required for user event handling; container must have usermanager set")
	}

	switch event.Type {
	case usermanager.UserEventAdd, usermanager.UserEventUpdate:
		e.syncUserToInbound(event.Username, event.User)

	case usermanager.UserEventRemove, usermanager.UserEventExpire:
		e.removeUserFromInbounds(event.Username)
	}

	return nil
}

// syncUserToInbound evaluates two-layer access control for a user across all
// inbounds, adding forward ports where allowed and removing them where denied.
func (e *Executor) syncUserToInbound(username string, user *contracts.User) {
	if user == nil {
		return
	}

	e.inboundsMu.RLock()
	inbounds := make([]*XrayInbound, 0, len(e.inbounds))
	for _, in := range e.inbounds {
		inbounds = append(inbounds, in)
	}
	e.inboundsMu.RUnlock()

	for _, inbound := range inbounds {
		if e.userMgr != nil && !e.userMgr.CanUserAccessInbound(user, inbound.GetAccessTags()) {
			if err := inbound.RemoveUser(username); err != nil {
				log.Warn("[syncUserToInbound] failed to remove denied user from inbound", "user", username, "inbound", inbound.Tag(), "err", err)
			}
			continue
		}
		if _, err := inbound.AddUser(username, user); err != nil {
			log.Warn("[syncUserToInbound] failed to add user to inbound", "user", username, "inbound", inbound.Tag(), "err", err)
		}
	}
}

// removeUserFromInbounds removes a user from all inbounds.
func (e *Executor) removeUserFromInbounds(username string) {
	// Get inbounds
	e.inboundsMu.RLock()
	inbounds := make([]*XrayInbound, 0, len(e.inbounds))
	for _, in := range e.inbounds {
		inbounds = append(inbounds, in)
	}
	e.inboundsMu.RUnlock()

	// For each inbound, call the inbound's RemoveUser method
	// This handles inbound user removal + forward rule removal + mapping cleanup
	// This is idempotent - calling multiple times has no additional effect
	for _, inbound := range inbounds {
		if err := inbound.RemoveUser(username); err != nil {
			// Log but continue - other inbounds should still be processed
			log.Warn("[removeUserFromInbounds] failed to remove user from inbound",
				"user", username, "inbound", inbound.Tag(), "err", err)
		}
	}
}

// reconcileUsers syncs all users from UserManager to all inbounds using two-layer
// access control. Both adds newly authorized users and removes unauthorized/stale users.
func (e *Executor) reconcileUsers() {
	if e.userMgr == nil {
		return
	}

	users := e.userMgr.ListUsers()
	userByName := make(map[string]*contracts.User, len(users))
	for _, u := range users {
		userByName[u.Username] = u
	}

	// Sync to all inbounds
	e.inboundsMu.RLock()
	inbounds := make([]*XrayInbound, 0, len(e.inbounds))
	for _, in := range e.inbounds {
		inbounds = append(inbounds, in)
	}
	e.inboundsMu.RUnlock()

	for _, inbound := range inbounds {
		accessTags := inbound.GetAccessTags()

		// Remove users that are tracked but no longer visible or no longer authorized.
		for _, username := range inbound.listAddedUsers() {
			u, visible := userByName[username]
			if !visible || !e.userMgr.CanUserAccessInbound(u, accessTags) {
				if err := inbound.RemoveUser(username); err != nil {
					log.Warn("[reconcileUsers] failed to remove stale/unauthorized user from inbound",
						"user", username, "inbound", inbound.Tag(), "err", err)
				}
			}
		}

		// Add users that pass both layers and are not yet tracked.
		for _, user := range users {
			if !e.userMgr.CanUserAccessInbound(user, accessTags) {
				continue
			}
			if inbound.hasAddedUser(user.Username) {
				continue
			}
			if _, err := inbound.AddUser(user.Username, user); err != nil {
				log.Warn("[reconcileUsers] failed to add user to inbound", "user", user.Username, "inbound", inbound.Tag(), "err", err)
			}
		}
	}
}

// reconcileUsersForInbound performs bidirectional user reconciliation for a specific inbound:
// removes tracked users who fail CanUserAccessInbound and adds visible users who pass it.
// inbound must already be registered in e.inbounds before calling this.
// This function does NOT acquire inboundsMu across AddUser/RemoveUser to avoid deadlock.
func (e *Executor) reconcileUsersForInbound(tag string) {
	if e.userMgr == nil {
		return
	}

	e.inboundsMu.RLock()
	inbound, exists := e.inbounds[tag]
	e.inboundsMu.RUnlock()

	if !exists {
		return
	}

	accessTags := inbound.GetAccessTags()
	users := e.userMgr.ListUsers()
	userByName := make(map[string]*contracts.User, len(users))
	for _, u := range users {
		userByName[u.Username] = u
	}

	// Remove tracked users who are no longer visible or fail CanUserAccessInbound.
	for _, username := range inbound.listAddedUsers() {
		u, visible := userByName[username]
		if !visible || !e.userMgr.CanUserAccessInbound(u, accessTags) {
			if err := inbound.RemoveUser(username); err != nil {
				log.Warn("[reconcileUsersForInbound] failed to remove stale/unauthorized user from inbound",
					"user", username, "inbound", tag, "err", err)
			}
		}
	}

	// Add visible users who pass CanUserAccessInbound and are not yet tracked.
	for _, user := range users {
		if !e.userMgr.CanUserAccessInbound(user, accessTags) {
			continue
		}
		if inbound.hasAddedUser(user.Username) {
			continue
		}
		if _, err := inbound.AddUser(user.Username, user); err != nil {
			log.Warn("[reconcileUsersForInbound] failed to add user to inbound", "user", user.Username, "inbound", tag, "err", err)
		}
	}
}

// startReconcileLoop starts the periodic user reconciliation goroutine.
// It is idempotent and returns true only when a new reconcile loop was started.
func (e *Executor) startReconcileLoop() bool {
	// Idempotent: a non-nil reconcileStopCh means a loop is already running.
	// Now that IsRunning reflects real liveness, the updater can skip Stop and
	// call Start on a crashed executor; without this guard that would spawn a
	// second reconcile goroutine and race on reconcileStopCh.
	e.lifecycleMu.Lock()
	if e.reconcileStopCh != nil {
		e.lifecycleMu.Unlock()
		return false
	}
	interval := e.config.ReconcileInterval
	if interval <= 0 {
		interval = 30 * time.Second // default
	}

	stopCh := make(chan struct{})
	e.reconcileStopCh = stopCh
	e.reconcileWg.Add(1)
	e.lifecycleMu.Unlock()

	go func(stopCh <-chan struct{}) {
		defer e.reconcileWg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				e.reconcileUsers()
			case <-stopCh:
				return
			}
		}
	}(stopCh)
	return true
}

// stopReconcileLoop stops the periodic user reconciliation goroutine gracefully.
func (e *Executor) stopReconcileLoop() {
	e.lifecycleMu.Lock()
	stopCh := e.reconcileStopCh
	e.reconcileStopCh = nil
	e.lifecycleMu.Unlock()

	if stopCh != nil {
		close(stopCh)
		e.reconcileWg.Wait()
	}
}

// XrayInbound is the xray-specific implementation of inbound.Inbound.
type XrayInbound struct {
	tag        string
	protocol   contracts.Protocol
	port       uint32
	listenAddr string
	security   contracts.Security
	transport  contracts.Transport
	config     *inbound.Config
	extra      map[string]interface{}
	nativeJSON []byte

	// defaultClientUUID is the UUID from the first client in the inbound's client list.
	// This is used for VMess/VLESS subscription generation - all users share this UUID.
	defaultClientUUID string

	// defaultPassword is the password from the inbound's settings.
	// This is used for Trojan/Shadowsocks subscription generation - all users share this password.
	// For Trojan: extracted from settings.clients[0].password
	// For Shadowsocks: extracted from settings.password
	defaultPassword string

	// tempCertFiles holds paths to temporary cert/key files created by FastAddInbound
	// (cases: PEM content written to temp files, or self-signed cert).
	// These are deleted when the inbound is removed via RemoveInboundConfig.
	tempCertFiles []string

	// forwardPort is the frontend port used for port forwarding.
	// If set, subscription should use this port instead of the internal port.
	// This is used when multiple users share a single xray inbound via forward layer.
	forwardPort uint32

	// userEmail is the user email associated with this inbound.
	// Used in the forward layer for port mapping lookups.
	userEmail string

	// userMgr provides access to user management capabilities.
	// This is injected by the container (Executor) and used for AddUser/RemoveUser.
	userMgr *usermanager.UserManager

	// certSource / certDomain preserve certificate metadata across tag updates.
	certSource string
	certDomain string

	// Chain proxy & two-layer visibility metadata. accessTags can be updated at
	// runtime via SetInboundAccessTags while concurrent readers inspect it.
	metaMu           sync.RWMutex
	inboundType      contracts.InboundType
	accessTags       []string
	chainOutboundURI string
	remark           string

	// addedUsers tracks users that have been successfully added to this inbound.
	// Reconcile uses this to skip users that are already wired up, avoiding
	// redundant GetBindPort calls every cycle.
	//
	// All access MUST go through the helper methods (hasAddedUser, listAddedUsers,
	// markAddedUser, unmarkAddedUser) which serialize access via addedUsersMu.
	// Direct map access is not permitted because multiple goroutines (user event
	// handler, periodic reconcile loop, new inbound registration) touch this map
	// concurrently.
	addedUsersMu sync.Mutex
	addedUsers   map[string]struct{}
}

// hasAddedUser reports whether the given user is already tracked on this inbound.
// Thread-safe.
func (in *XrayInbound) hasAddedUser(email string) bool {
	in.addedUsersMu.Lock()
	defer in.addedUsersMu.Unlock()
	_, ok := in.addedUsers[email]
	return ok
}

// listAddedUsers returns a snapshot of the currently tracked user names.
// Thread-safe. Callers may iterate over the returned slice freely.
func (in *XrayInbound) listAddedUsers() []string {
	in.addedUsersMu.Lock()
	defer in.addedUsersMu.Unlock()
	out := make([]string, 0, len(in.addedUsers))
	for u := range in.addedUsers {
		out = append(out, u)
	}
	return out
}

// markAddedUser records that the given user has been wired up on this inbound.
// Thread-safe.
func (in *XrayInbound) markAddedUser(email string) {
	in.addedUsersMu.Lock()
	defer in.addedUsersMu.Unlock()
	in.addedUsers[email] = struct{}{}
}

// unmarkAddedUser removes the given user from the tracking set.
// Thread-safe. Idempotent.
func (in *XrayInbound) unmarkAddedUser(email string) {
	in.addedUsersMu.Lock()
	defer in.addedUsersMu.Unlock()
	delete(in.addedUsers, email)
}

// GetInboundType returns the inbound type ("direct" by default).
func (in *XrayInbound) GetInboundType() contracts.InboundType {
	in.metaMu.RLock()
	defer in.metaMu.RUnlock()
	if in.inboundType == "" {
		return contracts.InboundTypeDirect
	}
	return in.inboundType
}

// GetAccessTags returns a cloned slice of this inbound's Layer-2 access tags.
func (in *XrayInbound) GetAccessTags() []string {
	in.metaMu.RLock()
	defer in.metaMu.RUnlock()
	return slices.Clone(in.accessTags)
}

// SetAccessTags replaces this inbound's Layer-2 access tags safely.
func (in *XrayInbound) SetAccessTags(tags []string) {
	in.metaMu.Lock()
	defer in.metaMu.Unlock()
	in.accessTags = slices.Clone(tags)
}

// GetChainOutboundURI returns the raw backend URI for a chain inbound.
func (in *XrayInbound) GetChainOutboundURI() string {
	in.metaMu.RLock()
	defer in.metaMu.RUnlock()
	return in.chainOutboundURI
}

// GetRemark returns the inbound remark.
func (in *XrayInbound) GetRemark() string {
	in.metaMu.RLock()
	defer in.metaMu.RUnlock()
	return in.remark
}

// GetMeta returns a snapshot of the inbound's chain and visibility metadata.
func (in *XrayInbound) GetMeta() contracts.InboundMeta {
	in.metaMu.RLock()
	defer in.metaMu.RUnlock()
	it := in.inboundType
	if it == "" {
		it = contracts.InboundTypeDirect
	}
	return contracts.InboundMeta{
		InboundType:      it,
		AccessTags:       slices.Clone(in.accessTags),
		ChainOutboundURI: in.chainOutboundURI,
		Remark:           in.remark,
	}
}

func (in *XrayInbound) toInboundRecord() *InboundRecord {
	meta := in.GetMeta()
	certSource := in.certSource
	if certSource == "" {
		certSource = "none"
	}
	nativeJSON := in.nativeJSON
	if len(nativeJSON) == 0 {
		nativeJSON, _ = in.ToNative()
	}
	return &InboundRecord{
		Tag:              in.tag,
		ContainerType:    string(contracts.ContainerXray),
		CertSource:       certSource,
		CertDomain:       in.certDomain,
		NativeJSON:       nativeJSON,
		InboundType:      string(meta.InboundType),
		AccessTags:       slices.Clone(meta.AccessTags),
		ChainOutboundURI: meta.ChainOutboundURI,
		Remark:           meta.Remark,
	}
}

// Tag returns the inbound tag.
func (in *XrayInbound) Tag() string { return in.tag }

// Protocol returns the protocol.
func (in *XrayInbound) Protocol() contracts.Protocol { return in.protocol }

// Port returns the port.
func (in *XrayInbound) Port() uint32 { return in.port }

// ListenAddr returns the listen address.
func (in *XrayInbound) ListenAddr() string { return in.listenAddr }

// Security returns the security mode.
func (in *XrayInbound) Security() contracts.Security { return in.security }

// Transport returns the network transport.
func (in *XrayInbound) Transport() contracts.Transport { return in.transport }

// Config returns the generic inbound configuration.
func (in *XrayInbound) Config() *inbound.Config {
	if in.config != nil {
		return in.config
	}
	extensions := in.extra
	if extensions == nil {
		extensions = make(map[string]any)
	}
	// Store xray-specific fields in extensions for portability
	extensions["security"] = in.security
	extensions["transport"] = in.transport
	return &inbound.Config{
		Tag:        in.tag,
		ListenAddr: in.listenAddr,
		Port:       in.port,
		Protocol:   in.protocol,
		Extensions: extensions,
	}
}

// Extra returns the extra config.
func (in *XrayInbound) Extra() map[string]interface{} { return in.extra }

// ToNative returns the native xray config JSON.
func (in *XrayInbound) ToNative() ([]byte, error) {
	if in.nativeJSON != nil {
		return in.nativeJSON, nil
	}
	// Generate from config
	return json.Marshal(map[string]interface{}{
		"tag":      in.tag,
		"protocol": in.protocol.String(),
		"port":     in.port,
		"listen":   in.listenAddr,
	})
}

// Validate validates the inbound.
func (in *XrayInbound) Validate() error {
	if in.tag == "" {
		return fmt.Errorf("tag is required")
	}
	if in.port < 100 || in.port > 65535 {
		return fmt.Errorf("port must be between 100 and 65535")
	}
	return nil
}

// ForwardPort returns the forward port for this inbound.
// Returns 0 if no forward port is configured.
func (in *XrayInbound) ForwardPort() uint32 {
	return in.forwardPort
}

// SetForwardPort sets the forward port for this inbound.
func (in *XrayInbound) SetForwardPort(port uint32) {
	in.forwardPort = port
}

// Username returns the username associated with this inbound.
func (in *XrayInbound) Username() string {
	return in.userEmail
}

// SetUsername sets the username for this inbound.
func (in *XrayInbound) SetUsername(username string) {
	in.userEmail = username
}

// DefaultClientUUID returns the default client UUID for this inbound.
// Used for VMess/VLESS subscription generation.
func (in *XrayInbound) DefaultClientUUID() string {
	return in.defaultClientUUID
}

// SetDefaultClientUUID sets the default client UUID for this inbound.
func (in *XrayInbound) SetDefaultClientUUID(uuid string) {
	in.defaultClientUUID = uuid
}

// SetUserManager sets the user manager for this inbound.
// This is called by the container (Executor) when creating or updating the inbound.
func (in *XrayInbound) SetUserManager(userMgr *usermanager.UserManager) {
	in.userMgr = userMgr
}

// AddUser adds a user to this inbound.
// It allocates a forward port via usermanager.GetBindPort.
// Note: This does NOT call xray gRPC AddUser - users are distinguished by forward ports.
// Returns the allocated frontend port and error if any step fails.
func (in *XrayInbound) AddUser(email string, user *contracts.User) (uint32, error) {
	if in.userMgr == nil {
		return 0, fmt.Errorf("user manager not configured for inbound %s", in.tag)
	}

	// Fast path: user already tracked in memory
	if in.hasAddedUser(email) {
		if port, ok := in.userMgr.GetUserPortByDst(email, in.port); ok {
			return port, nil
		}
		// Stale tracking — forward rule gone, fall through to recreate
		in.unmarkAddedUser(email)
	}

	// Call GetBindPort to allocate forward rule
	bindPort, err := in.userMgr.GetBindPort(usermanager.GetBindPortRequest{
		Username:      email,
		TargetPort:    in.port,
		ContainerType: contracts.ContainerXray,
		InboundTag:    in.tag,
		Protocol:      in.protocol,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to get bind port: %w", err)
	}

	in.markAddedUser(email)
	return bindPort, nil
}

// RemoveUser removes a user from this inbound.
// It removes the forward rule via UserManager.
// Note: This does NOT call xray gRPC RemoveUser.
// This is idempotent - calling multiple times has no additional effect.
func (in *XrayInbound) RemoveUser(email string) error {
	// Always clean tracking, even if userMgr is nil
	in.unmarkAddedUser(email)

	if in.userMgr == nil {
		return nil
	}

	// Use ForCleanup variant so we can find port mappings even when the user
	// is already in "deleting" state. This is critical for two-phase deletion:
	// RemoveUser marks deleting first, then emits UserEventRemove; we must
	// still locate the port so ReleaseBindPort can finalize the deletion.
	port, exists := in.userMgr.GetUserPortByDstForCleanup(email, in.port)
	if !exists {
		return nil
	}

	// Release the bind port via UserManager
	_ = in.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
		Username: email,
		BindPort: port,
	})

	return nil
}

// ListUsers returns all users in this inbound.
// Returns a map of email -> frontend port by querying UserManager.
func (in *XrayInbound) ListUsers() map[string]uint32 {
	result := make(map[string]uint32)
	if in.userMgr == nil {
		return result
	}
	for _, user := range in.userMgr.ListUsers() {
		if port, ok := user.PortMappings[in.port]; ok {
			result[user.Username] = port
		}
	}
	return result
}

// ReleaseAllUserPorts releases all user ports for this inbound.
// This is called when the inbound is stopped/destroyed.
// Delegates to UserManager.ReleaseInboundPorts for cleanup.
func (in *XrayInbound) ReleaseAllUserPorts() error {
	if in.userMgr == nil {
		return nil
	}
	return in.userMgr.ReleaseInboundPorts(in.tag)
}

// GetUserPort returns the frontend port for a user in this inbound.
// Returns 0 and false if user not found.
func (in *XrayInbound) GetUserPort(email string) (uint32, bool) {
	if in.userMgr == nil {
		return 0, false
	}
	return in.userMgr.GetUserPortByDst(email, in.port)
}

// HasUser returns true if the user exists in this inbound.
func (in *XrayInbound) HasUser(email string) bool {
	_, exists := in.GetUserPort(email)
	return exists
}

// SetUserPortForTest sets a user port mapping for testing purposes.
// This is only for testing - not for production use.
// It delegates to UserManager.SetPortMappingForTest to set PortMappings[dstPort] = port.
func (in *XrayInbound) SetUserPortForTest(email string, port uint32) {
	if in.userMgr != nil {
		in.userMgr.SetPortMappingForTest(email, in.port, port)
	}
}

// GetSub generates a subscription spec for a user on this inbound.
// It enforces two-layer access control via UserManager.CanUserAccessInboundByUsername
// and uses UserManager.GetUserPortByDst as the authoritative source for the frontend port.
// Returns error if the user is denied or not found in UserManager's port mapping.
func (in *XrayInbound) GetSub(req contracts.SubscriptionRequest) (contracts.SubscriptionSpec, error) {
	meta := in.GetMeta()
	if in.userMgr != nil && !in.userMgr.CanUserAccessInboundByUsername(req.User.Username, meta.AccessTags) {
		return contracts.SubscriptionSpec{}, fmt.Errorf("user %s not found in inbound %s", req.User.Username, in.tag)
	}

	// Check if user exists in this inbound's mapping
	port, exists := in.GetUserPort(req.User.Username)
	if !exists {
		return contracts.SubscriptionSpec{}, fmt.Errorf("user %s not found in inbound %s", req.User.Username, in.tag)
	}

	// Port override: if req.Port is explicitly provided (non-zero), use it instead of internal mapping
	if req.Port != 0 {
		port = req.Port
	}

	// Get password/credential for this protocol
	password, err := in.getCredentialForUser(req.User)
	if err != nil {
		return contracts.SubscriptionSpec{}, err
	}

	// Build extensions from inbound extra
	extensions := in.buildSubscriptionExtensions(req.User)

	nodeName := resolveSubscriptionNodeName(req.NodeName, in.tag, meta)

	spec := contracts.SubscriptionSpec{
		Protocol:   in.protocol,
		Host:       req.Host,
		Port:       port,
		Password:   password,
		NodeName:   nodeName,
		InboundTag: in.tag,
		Username:   req.User.Username,
		Extensions: extensions,
	}

	// Generate URI based on protocol
	uri, err := in.generateSubscriptionURI(spec)
	if err != nil {
		return contracts.SubscriptionSpec{}, err
	}
	spec.URI = uri

	return spec, nil
}

// getCredentialForUser extracts the credential (password/uuid) for the given user and protocol.
// For VMess/VLESS, uuid comes from inbound's defaultClientUUID.
// For Trojan/SS, password comes from inbound's defaultPassword (not from user extensions).
// For SOCKS5, password still comes from user extensions (supports per-user auth).
func (in *XrayInbound) getCredentialForUser(user contracts.UserSpec) (string, error) {
	switch in.protocol {
	case contracts.ProtocolVMess, contracts.ProtocolVLess:
		// UUID comes from inbound's default client
		uuid := in.defaultClientUUID
		if uuid == "" {
			return "", fmt.Errorf("default client uuid not found for inbound %s", in.tag)
		}
		return uuid, nil
	case contracts.ProtocolTrojan, contracts.ProtocolShadowsocks:
		// Password MUST come from inbound's internal default password
		// This unifies credential source: inbound stores the password from native settings
		password := in.defaultPassword
		if password == "" {
			return "", fmt.Errorf("default password not found for inbound %s", in.tag)
		}
		return password, nil
	case contracts.ProtocolSOCKS5:
		// SOCKS5 supports per-user auth; auth token is used as the SOCKS5 password.
		if user.AuthToken != "" {
			return user.AuthToken, nil
		}
		return "", fmt.Errorf("password not found for user %s", user.Username)
	default:
		return "", fmt.Errorf("unsupported protocol for subscription: %s", in.protocol)
	}
}

// buildSubscriptionExtensions builds extension fields for subscription URI generation.
func (in *XrayInbound) buildSubscriptionExtensions(user contracts.UserSpec) map[string]any {
	ext := make(map[string]any)

	// Copy transport and security from inbound
	if in.transport != "" {
		ext["transport"] = string(in.transport)
	}
	if in.security != "" {
		ext["security"] = string(in.security)
	}

	// Copy from inbound extra
	if in.extra != nil {
		copyKeys := []string{
			"ws_path", "ws_host",
			"grpc_service_name", "grpc_mode",
			"httpupgrade_path", "httpupgrade_host",
			"server_name", "alpn", "utls_fingerprint",
			"mkcp_header_type", "mkcp_seed",
			"quic_security", "quic_key", "quic_header_type",
			"xhttp_mode", "xhttp_path", "xhttp_host",
			"method", "ss_plugin", "ss_plugin_opts",
			"flow",
			// Reality-specific fields for subscription URI
			"reality_public_key", "reality_short_ids", "reality_server_names",
			// Cert source for SNI logic
			"cert_source",
			// Client cert-verification hint — propagates insecure=1 into URI
			"skip_cert_verify",
		}
		for _, key := range copyKeys {
			if v, ok := in.extra[key]; ok {
				ext[key] = v
			}
		}
	}

	return ext
}

// generateSubscriptionURI generates the subscription URI based on the protocol.
// Delegates to the existing generateURI function from subscription.go.
func (in *XrayInbound) generateSubscriptionURI(spec contracts.SubscriptionSpec) (string, error) {
	return generateURI(spec)
}

// Init implements container.Container.Init.
// It initializes the executor with the given configuration.
// The config must be *ExecutorConfig, or nil for default initialization.
func (e *Executor) Init(config any) error {
	if config == nil {
		// No config provided, use existing configuration
		return nil
	}

	cfg, ok := config.(*ExecutorConfig)
	if !ok {
		return fmt.Errorf("invalid config type: expected *ExecutorConfig, got %T", config)
	}

	// Apply the new configuration
	e.config = *cfg

	// Reinitialize components with new config
	// Update process runner config
	e.Runner.SetConfig(process.RunnerConfig{
		BinaryPath: e.config.BinaryPath,
		Args:       []string{"run"},
		ConfigFile: e.config.ConfigFilePath,
		Stdout:     log.NewPrefixWriter(os.Stdout, string(cfg.ContainerType)),
		Stderr:     log.NewPrefixWriter(os.Stderr, string(cfg.ContainerType)),
	})

	// Reinitialize updater if:
	// 1. Explicit update config is provided (UpdateConfig.BinaryPath != "")
	// 2. OR AutoDownload is enabled (need default updater for auto-download)
	if e.config.UpdateConfig.BinaryPath != "" || e.config.AutoDownload {
		// Build updater config: use explicit config or create default
		updaterConfig := e.config.UpdateConfig
		if updaterConfig.BinaryPath == "" {
			// Create default updater config based on BinaryPath
			binaryDir := filepath.Dir(e.config.BinaryPath)
			if binaryDir == "." {
				binaryDir = "/tmp"
			}
			updaterConfig = UpdaterConfig{
				BinaryPath:  e.config.BinaryPath,
				DownloadDir: binaryDir,
				Owner:       "XTLS",
				Repo:        "Xray-core",
				AssetName:   "Xray-linux-64.zip",
				BinaryName:  "xray",
			}
		}

		updater, err := NewUpdater(updaterConfig)
		if err != nil {
			return fmt.Errorf("failed to reinitialize updater: %w", err)
		}
		updater.SetProcessController(e)
		e.updater = updater
	}

	return nil
}

// FastAddInbound quickly adds an inbound with minimal parameters.
// Required params:
//   - "protocol": proxy protocol (vmess/vless/trojan/shadowsocks/socks5/http)
//
// Optional params with defaults:
//   - "port": listen port (defaults to random available port from PortClaimer)
//   - "listen": listen address (defaults to "127.0.0.1"; chain inbounds require "127.0.0.1")
//   - "security": security mode (defaults to "tls" for non-ss protocols, "none" for ss)
//   - "transport": network transport (defaults to "tcp")
//   - "server_name": SNI name for TLS (defaults to "localhost")
//   - "inbound_type": "direct" (default) or "chain"
//   - "chain_uri": backend proxy URI (required when inbound_type=="chain", forbidden when "direct")
//   - "access_tags": []string Layer-2 visibility tags
//   - "remark": display remark (for chain inbounds defaults to URI #fragment)
//
// TLS Certificate handling (priority order):
//   - certificateFile + keyFile: explicit file paths
//   - certificate + key: PEM content (written to temp files)
//   - domain: lookup from certManager
//   - self_signed=true: auto-generate self-signed cert
//
// Protocol-specific required params:
//   - shadowsocks: "method", "password"
//
// Returns error if:
//   - tag is empty or invalid
//   - protocol is missing or invalid
//   - inbound_type / chain_uri / listen / access_tags invalid
//   - TLS required but no certificate provided
//   - shadowsocks protocol missing method or password
func (e *Executor) FastAddInbound(tag string, params map[string]any) error {
	// Validate tag
	if tag == "" {
		return errs.Newf(errs.ErrFastAddInboundFailed, "tag is required")
	}
	if err := contracts.ValidateInboundTag(tag); err != nil {
		return errs.Wrap(errs.ErrFastAddInboundFailed, "invalid inbound tag", err)
	}

	// Get protocol (required)
	protocolStr, ok := params["protocol"].(string)
	if !ok || protocolStr == "" {
		return errs.Newf(errs.ErrFastAddInboundProtocolRequired, "protocol is required")
	}

	protocol := contracts.Protocol(protocolStr)
	if !protocol.IsValid() {
		return errs.Newf(errs.ErrFastAddInboundFailed, "invalid protocol: %s", protocolStr)
	}

	// Fail fast on duplicate tag before claiming resources or touching live chain routing.
	e.inboundsMu.RLock()
	_, alreadyExists := e.inbounds[tag]
	e.inboundsMu.RUnlock()
	if alreadyExists {
		return errs.Newf(errs.ErrInboundAlreadyExists, "inbound %s already exists", tag)
	}

	// Parse & validate chain / visibility metadata before claiming ports or generating certs.
	inboundType, chainURI, err := parseInboundTypeAndChainParams(params)
	if err != nil {
		return errs.Wrap(errs.ErrFastAddInboundFailed, "invalid inbound chain parameters", err)
	}
	accessTags, err := parseAccessTagsParam(params)
	if err != nil {
		return errs.Wrap(errs.ErrFastAddInboundFailed, "invalid access_tags", err)
	}
	listenAddr, err := parseListenParam(params, inboundType)
	if err != nil {
		return errs.Wrap(errs.ErrFastAddInboundFailed, "invalid listen address", err)
	}

	remark := strings.TrimSpace(fastGetString(params, "remark", ""))
	var chainOutboundJSON []byte
	if inboundType == contracts.InboundTypeChain {
		specChain, rendered, chainErr := parseAndRenderXrayChainOutbound(chainOutboundTag(tag), chainURI)
		if chainErr != nil {
			return errs.Wrap(errs.ErrFastAddInboundFailed, "invalid chain_uri", chainErr)
		}
		chainOutboundJSON = rendered
		if remark == "" {
			remark = strings.TrimSpace(specChain.Remark)
		}
	}

	// Resolve port through the shared allocator — the single authority over
	// every port this process binds. An explicitly requested port is claimed
	// verbatim or rejected, never substituted.
	requestedPort := fastGetUint32(params, "port", 0)
	if requestedPort != 0 && (requestedPort < 100 || requestedPort > 65535) {
		return errs.Newf(errs.ErrFastAddInboundFailed, "port must be between 100 and 65535, got %d", requestedPort)
	}
	port, err := container.ClaimInboundPort(e.portClaimer, requestedPort)
	if err != nil {
		return errs.Wrap(errs.ErrFastAddInboundFailed, "failed to claim inbound listen port", err)
	}
	// The claim is provisional until the inbound is committed to the map and
	// the store. Any early return below must give the port back, or a failed
	// add leaks it for the lifetime of the process.
	portCommitted := false
	defer func() {
		if !portCommitted {
			container.ReleaseInboundPort(e.portClaimer, port)
		}
	}()

	// Normalize: domain implies server_name for TLS SNI
	// If domain is set, server_name must match for certificate validation
	if domain, ok := params["domain"].(string); ok && domain != "" {
		params["server_name"] = domain
	}

	// Determine security
	security := contracts.SecurityTLS
	if protocol == contracts.ProtocolShadowsocks {
		security = contracts.SecurityNone
	}
	if s, ok := params["security"].(string); ok && s != "" {
		// Legacy xtls → tls (xray-core removed xtls security type)
		if s == "xtls" {
			s = "tls"
		}
		security = contracts.Security(s)
		if !security.IsValid() {
			return errs.Newf(errs.ErrFastAddInboundFailed, "invalid security: %s", s)
		}
	}

	// Bug 4: SOCKS5 and HTTP do not support TLS/Reality
	if (protocol == contracts.ProtocolSOCKS5 || protocol == contracts.ProtocolHTTP) &&
		security != contracts.SecurityNone {
		return errs.Newf(errs.ErrFastAddInboundFailed,
			"protocol %s does not support security=%s; only security=none is allowed", protocol, security)
	}

	// Step 1: Cert resolution (only for TLS)
	var certFile, keyFile, certDomain string
	certSource := "none"
	var certShouldCleanup bool
	certCommitted := false
	if security == contracts.SecurityTLS {
		var certErr error
		certFile, keyFile, certSource, certDomain, certShouldCleanup, certErr = e.resolveFastAddCert(params)
		if certErr != nil {
			return certErr
		}
	}
	defer func() {
		if !certCommitted && certShouldCleanup {
			if certFile != "" {
				_ = os.Remove(certFile)
			}
			if keyFile != "" {
				_ = os.Remove(keyFile)
			}
		}
	}()

	// Step 2: Route to profilegen or simple path
	var spec contracts.InboundSpec
	switch protocol {
	case contracts.ProtocolVMess:
		if security == contracts.SecurityNone {
			var simpleErr error
			spec, simpleErr = buildFastAddSimpleSpec(tag, port, protocol, params)
			if simpleErr != nil {
				return simpleErr
			}
		} else {
			p := profilegen.GenerateVMessTLSParams{
				Host:       fastGetString(params, "server_name", "localhost"),
				Transport:  fastGetString(params, "transport", "tcp"),
				Port:       port,
				Tag:        tag,
				ListenAddr: listenAddr,
				WSPath:     fastGetString(params, "ws_path", ""),
				HTTPPath:   fastGetString(params, "http_path", ""),
				HTTPHost:   fastGetStringSlice(params, "http_host"),
				CertFile:   certFile,
				KeyFile:    keyFile,
			}
			genSpec, genErr := profilegen.GenerateVMessTLSInboundSpec(p)
			if genErr != nil {
				return errs.Wrap(errs.ErrFastAddInboundFailed, "vmess spec generation failed", genErr)
			}
			spec = genSpec
		}

	case contracts.ProtocolVLess:
		if security == contracts.SecurityNone {
			var simpleErr error
			spec, simpleErr = buildFastAddSimpleSpec(tag, port, protocol, params)
			if simpleErr != nil {
				return simpleErr
			}
		} else {
			sniffEnabled := fastGetBool(params, "sniffing_enabled")
			sniffRouteOnly := fastGetBool(params, "sniffing_route_only")
			if inboundType == contracts.InboundTypeChain && sniffEnabled {
				sniffRouteOnly = true
			}
			p := profilegen.GenerateVLessParams{
				Host:               fastGetString(params, "server_name", "localhost"),
				Transport:          fastGetString(params, "transport", "tcp"),
				Security:           string(security),
				Port:               port,
				Tag:                tag,
				ListenAddr:         listenAddr,
				UUID:               fastGetString(params, "uuid", ""),
				Flow:               fastGetString(params, "flow", ""),
				WSPath:             fastGetString(params, "ws_path", ""),
				GRPCServiceName:    fastGetString(params, "grpc_service_name", ""),
				HTTPUpgradePath:    fastGetString(params, "httpupgrade_path", ""),
				HTTPUpgradeHost:    fastGetString(params, "httpupgrade_host", ""),
				XHTTPMode:          fastGetString(params, "xhttp_mode", ""),
				XHTTPPath:          fastGetString(params, "xhttp_path", ""),
				XHTTPHost:          fastGetStringSlice(params, "xhttp_host"),
				RealityPrivateKey:  fastGetString(params, "reality_private_key", ""),
				RealityPublicKey:   fastGetString(params, "reality_public_key", ""),
				RealityTarget:      fastGetString(params, "reality_target", ""),
				RealityServerNames: fastGetStringSlice(params, "reality_server_names"),
				RealityShortIDs:    fastGetStringSlice(params, "reality_short_ids"),
				CertFile:           certFile,
				KeyFile:            keyFile,
				// Sniffing
				SniffingEnabled:      sniffEnabled,
				SniffingDestOverride: fastGetStringSlice(params, "sniffing_dest_override"),
				SniffingRouteOnly:    sniffRouteOnly,
				// TLS advanced
				TLSRejectUnknownSNI: fastGetBool(params, "tls_reject_unknown_sni"),
				TLSMinVersion:       fastGetString(params, "tls_min_version", ""),
				ALPN:                fastGetStringSlice(params, "alpn"),
				OCSPStapling:        int(fastGetUint32(params, "ocsp_stapling", 0)),
			}
			genSpec, genErr := profilegen.GenerateVLessInboundSpec(p)
			if genErr != nil {
				return errs.Wrap(errs.ErrFastAddInboundFailed, "vless spec generation failed", genErr)
			}
			spec = genSpec
		}

	case contracts.ProtocolTrojan:
		if security == contracts.SecurityNone {
			var simpleErr error
			spec, simpleErr = buildFastAddSimpleSpec(tag, port, protocol, params)
			if simpleErr != nil {
				return simpleErr
			}
		} else {
			p := profilegen.GenerateTrojanTLSParams{
				Host:            fastGetString(params, "server_name", "localhost"),
				Transport:       fastGetString(params, "transport", "tcp"),
				Port:            port,
				Tag:             tag,
				ListenAddr:      listenAddr,
				Password:        fastGetString(params, "password", ""),
				GRPCServiceName: fastGetString(params, "grpc_service_name", ""),
				CertFile:        certFile,
				KeyFile:         keyFile,
			}
			genSpec, genErr := profilegen.GenerateTrojanTLSInboundSpec(p)
			if genErr != nil {
				return errs.Wrap(errs.ErrFastAddInboundFailed, "trojan spec generation failed", genErr)
			}
			spec = genSpec
		}

	case contracts.ProtocolShadowsocks:
		method := fastGetString(params, "method", "2022-blake3-aes-256-gcm")
		if !isValidShadowsocksMethod(method, true) {
			return errs.Newf(errs.ErrFastAddInboundFailed, "invalid shadowsocks method: %s", method)
		}
		password := fastGetString(params, "password", "")
		if password == "" {
			// Auto-generate password based on method
			var genErr error
			password, genErr = generateSSPassword(method)
			if genErr != nil {
				return errs.Wrap(errs.ErrFastAddInboundFailed, "failed to generate SS password", genErr)
			}
		}
		p := profilegen.GenerateShadowsocksParams{
			Host:       fastGetString(params, "server_name", "localhost"),
			Transport:  fastGetString(params, "transport", "tcp"),
			Port:       port,
			Tag:        tag,
			ListenAddr: listenAddr,
			Method:     method,
			Password:   password,
		}
		genSpec, genErr := profilegen.GenerateShadowsocksInboundSpec(p)
		if genErr != nil {
			return errs.Wrap(errs.ErrFastAddInboundFailed, "shadowsocks spec generation failed", genErr)
		}
		spec = genSpec

	case contracts.ProtocolSOCKS5, contracts.ProtocolHTTP:
		spec = contracts.InboundSpec{
			Tag:      tag,
			Port:     port,
			Protocol: protocol,
			Extensions: map[string]any{
				"security":    "none",
				"transport":   fastGetString(params, "transport", "tcp"),
				"listen_addr": listenAddr,
			},
		}

	default:
		return errs.Newf(errs.ErrFastAddInboundFailed, "unsupported protocol: %s", protocol)
	}

	if spec.Extensions == nil {
		spec.Extensions = make(map[string]any)
	}
	spec.Extensions["listen_addr"] = listenAddr

	// For chain inbounds, sniffing MUST set routeOnly: true so local DNS
	// sniffing never rewrites the destination domain into a local-resolved IP.
	if fastGetBool(params, "sniffing_enabled") {
		spec.Extensions["sniffing_enabled"] = true
		if dest := fastGetStringSlice(params, "sniffing_dest_override"); len(dest) > 0 {
			spec.Extensions["sniffing_dest_override"] = dest
		}
		if fastGetBool(params, "sniffing_route_only") {
			spec.Extensions["sniffing_route_only"] = true
		}
	}
	if inboundType == contracts.InboundTypeChain {
		if enabled, _ := spec.Extensions["sniffing_enabled"].(bool); enabled {
			spec.Extensions["sniffing_route_only"] = true
		}
	}

	// Step 3: Convert to native config using adapter
	adapter := NewAdapter()
	nativeInbound, nativeErr := adapter.ToProvider(spec)
	if nativeErr != nil {
		return errs.Wrap(errs.ErrFastAddInboundFailed, "failed to convert inbound to native config", nativeErr)
	}

	// Read security/transport/listenAddr from spec.Extensions (set by profilegen or simple path)
	specSecurity := contracts.Security(fastGetStringFromMap(spec.Extensions, "security", "none"))
	specTransport := contracts.Transport(fastGetStringFromMap(spec.Extensions, "transport", "tcp"))
	specListenAddr := fastGetStringFromMap(spec.Extensions, "listen_addr", listenAddr)

	// Extract default credentials from native JSON for subscription support
	// This mirrors the logic in AddInbound
	defaultClientUUID := ""
	defaultPassword := ""
	if nativeInbound.JSON != nil {
		var raw map[string]interface{}
		if err := json.Unmarshal(nativeInbound.JSON, &raw); err == nil {
			if settings, ok := raw["settings"].(map[string]interface{}); ok {
				// VMess/VLESS: extract UUID from clients[0].id
				if clients, ok := settings["clients"].([]interface{}); ok && len(clients) > 0 {
					if firstClient, ok := clients[0].(map[string]interface{}); ok {
						if uuid, ok := firstClient["id"].(string); ok {
							defaultClientUUID = uuid
						}
						// Trojan: extract password from clients[0].password
						if protocol == contracts.ProtocolTrojan {
							if password, ok := firstClient["password"].(string); ok {
								defaultPassword = password
							}
						}
					}
				}
				// Shadowsocks: extract password directly from settings.password
				if protocol == contracts.ProtocolShadowsocks {
					if password, ok := settings["password"].(string); ok {
						defaultPassword = password
					}
				}
			}
		}
	}

	// Refuse to persist a credential-less vmess/vless/trojan inbound: it is a
	// dead inbound (no client can authenticate) and, worse, its empty
	// credential makes subscription generation fail-fast, killing the user's
	// whole subscription. Catch it here at the entry point rather than at
	// subscription time.
	if credentialLessInbound(protocol, defaultClientUUID, defaultPassword) {
		return errs.Newf(errs.ErrFastAddInboundFailed,
			"refusing to add credential-less %s inbound %q", protocol, tag)
	}

	// Store cert_source in extensions for subscription SNI logic
	if certSource != "none" {
		spec.Extensions["cert_source"] = certSource
	}
	// Propagate skip_cert_verify from caller params into extensions so the
	// subscription URI can emit insecure=1 (self-signed cert hint for clients).
	if fastGetBool(params, "skip_cert_verify") {
		spec.Extensions["skip_cert_verify"] = true
	}

	cfgObj := inbound.NewConfig(tag, protocol, spec.Port)
	cfgObj.ListenAddr = specListenAddr

	xrayInbound := &XrayInbound{
		tag:               tag,
		protocol:          protocol,
		port:              spec.Port,
		listenAddr:        specListenAddr,
		security:          specSecurity,
		transport:         specTransport,
		config:            cfgObj,
		extra:             spec.Extensions,
		nativeJSON:        nativeInbound.JSON,
		defaultClientUUID: defaultClientUUID,
		defaultPassword:   defaultPassword,
		certSource:        certSource,
		certDomain:        certDomain,
		inboundType:       inboundType,
		accessTags:        slices.Clone(accessTags),
		chainOutboundURI:  chainURI,
		remark:            remark,
		addedUsers:        make(map[string]struct{}),
	}
	// Track temp cert files so they are cleaned up when the inbound is removed (Bug 2)
	if certShouldCleanup && certFile != "" && keyFile != "" {
		xrayInbound.tempCertFiles = []string{certFile, keyFile}
	}

	// Inject user manager dependency
	if e.userMgr != nil {
		xrayInbound.SetUserManager(e.userMgr)
	}

	// Step 4 (zero-leak chain setup): cleanup -> AddOutbound -> AddRule before AddInbound.
	ctx := context.Background()
	cli := e.routingCLI()
	outTag := chainOutboundTag(tag)
	rTag := chainRuleTag(tag)
	if inboundType == contracts.InboundTypeChain {
		_ = cli.RemoveRule(ctx, rTag)
		_ = cli.RemoveOutbound(ctx, outTag)

		if err := cli.AddOutbound(ctx, chainOutboundJSON); err != nil {
			return errs.Wrap(errs.ErrFastAddInboundFailed,
				fmt.Sprintf("failed to add chain outbound for %s (%s)", tag, chain.RedactURI(chainURI)), err)
		}
		if err := cli.AddRule(ctx, rTag, tag, outTag); err != nil {
			if rbErr := cli.RemoveOutbound(ctx, outTag); rbErr != nil {
				log.Warn("[FastAddInbound] rollback RemoveOutbound failed", "tag", tag, "outboundTag", outTag, "err", rbErr)
			}
			return errs.Wrap(errs.ErrFastAddInboundFailed,
				fmt.Sprintf("failed to add chain routing rule for %s", tag), err)
		}
	}

	// Step 5: Add inbound to running xray process via gRPC
	api := NewXrayAPI(e.grpcAPIAddress, e.config.Debug)
	if err := api.AddInbound(nativeInbound.JSON); err != nil {
		if inboundType == contracts.InboundTypeChain {
			if rbErr := cli.RemoveRule(ctx, rTag); rbErr != nil {
				log.Warn("[FastAddInbound] rollback RemoveRule failed", "tag", tag, "ruleTag", rTag, "err", rbErr)
			}
			if rbErr := cli.RemoveOutbound(ctx, outTag); rbErr != nil {
				log.Warn("[FastAddInbound] rollback RemoveOutbound failed", "tag", tag, "outboundTag", outTag, "err", rbErr)
			}
		}
		// Map xray's "existing tag" gRPC error to ErrInboundAlreadyExists
		if strings.Contains(err.Error(), "existing tag found") {
			return errs.Newf(errs.ErrInboundAlreadyExists, "inbound %s already exists in xray", tag)
		}
		return errs.Wrap(errs.ErrFastAddInboundFailed, "failed to add inbound to xray process", err)
	}

	// Store in map & persist atomically with rollback if persistence fails.
	e.inboundsMu.Lock()

	if _, exists := e.inbounds[tag]; exists {
		e.inboundsMu.Unlock()
		return errs.Newf(errs.ErrInboundAlreadyExists, "inbound %s already exists", tag)
	}

	if e.storeMgr != nil {
		rec := xrayInbound.toInboundRecord()
		if err := e.storeMgr.InboundStore().Save(rec); err != nil {
			e.inboundsMu.Unlock()
			if rbErr := api.RemoveInbound(tag); rbErr != nil {
				log.Warn("[FastAddInbound] rollback RemoveInbound failed after store error", "tag", tag, "err", rbErr)
			}
			if inboundType == contracts.InboundTypeChain {
				if rbErr := cli.RemoveRule(ctx, rTag); rbErr != nil {
					log.Warn("[FastAddInbound] rollback RemoveRule failed after store error", "tag", tag, "ruleTag", rTag, "err", rbErr)
				}
				if rbErr := cli.RemoveOutbound(ctx, outTag); rbErr != nil {
					log.Warn("[FastAddInbound] rollback RemoveOutbound failed after store error", "tag", tag, "outboundTag", outTag, "err", rbErr)
				}
			}
			return errs.Wrap(errs.ErrFastAddInboundFailed, "failed to persist inbound", err)
		}
	}

	e.inbounds[tag] = xrayInbound
	e.inboundsMu.Unlock()
	portCommitted = true
	certCommitted = true

	// Sync existing users to this new inbound (must be outside lock to avoid deadlock,
	// since reconcileUsersForInbound acquires inboundsMu.RLock internally).
	e.reconcileUsersForInbound(tag)

	return nil
}

func parseInboundTypeAndChainParams(params map[string]any) (contracts.InboundType, string, error) {
	var rawType string
	if v, exists := params["inbound_type"]; exists && v != nil {
		switch t := v.(type) {
		case string:
			rawType = t
		case contracts.InboundType:
			rawType = string(t)
		default:
			return "", "", fmt.Errorf("inbound_type must be a string, got %T", v)
		}
	}

	var chainURI string
	if v, exists := params["chain_uri"]; exists && v != nil {
		s, ok := v.(string)
		if !ok {
			return "", "", fmt.Errorf("chain_uri must be a string, got %T", v)
		}
		chainURI = strings.TrimSpace(s)
	}

	var inboundType contracts.InboundType
	if strings.TrimSpace(rawType) == "" && chainURI != "" {
		inboundType = contracts.InboundTypeChain
	} else {
		var err error
		inboundType, err = contracts.NormalizeInboundType(rawType)
		if err != nil {
			return "", "", err
		}
	}

	if inboundType == contracts.InboundTypeChain && chainURI == "" {
		return "", "", fmt.Errorf("inbound_type=chain requires non-empty chain_uri")
	}
	if inboundType == contracts.InboundTypeDirect && chainURI != "" {
		return "", "", fmt.Errorf("inbound_type=direct forbids non-empty chain_uri")
	}
	return inboundType, chainURI, nil
}

func parseAccessTagsParam(params map[string]any) ([]string, error) {
	raw, exists := params["access_tags"]
	if !exists || raw == nil {
		return nil, nil
	}
	var items []string
	switch v := raw.(type) {
	case []string:
		items = v
	case []interface{}:
		items = make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("access_tags[%d] must be a string, got %T", i, item)
			}
			items = append(items, s)
		}
	default:
		return nil, fmt.Errorf("access_tags must be a string slice, got %T", raw)
	}
	return contracts.NormalizeAccessTags(items)
}

func parseListenParam(params map[string]any, inboundType contracts.InboundType) (string, error) {
	var rawListen string
	if v, exists := params["listen"]; exists && v != nil {
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("listen must be a string, got %T", v)
		}
		rawListen = strings.TrimSpace(s)
	}
	if inboundType == contracts.InboundTypeChain {
		if rawListen != "" && rawListen != "127.0.0.1" {
			return "", fmt.Errorf("chain inbound requires listen=\"127.0.0.1\", got %q", rawListen)
		}
		return "127.0.0.1", nil
	}
	if rawListen == "" {
		return "127.0.0.1", nil
	}
	return rawListen, nil
}

func parseAndRenderXrayChainOutbound(outboundTag, rawURI string) (*chain.OutboundSpec, []byte, error) {
	spec, err := chain.ParseURI(rawURI)
	if err != nil {
		return nil, nil, fmt.Errorf("parse chain URI (%s): %w", chain.RedactURI(rawURI), err)
	}
	outboundJSON, err := chain.RenderXrayOutbound(outboundTag, spec)
	if err != nil {
		return nil, nil, fmt.Errorf("render xray chain outbound (%s): %w", chain.RedactURI(rawURI), err)
	}
	return spec, outboundJSON, nil
}

func resolveSubscriptionNodeName(reqNodeName, tag string, meta contracts.InboundMeta) string {
	if meta.Remark != "" {
		if reqNodeName == "" {
			return meta.Remark
		}
		return reqNodeName + "-" + meta.Remark
	}
	if meta.InboundType == contracts.InboundTypeChain {
		if reqNodeName == "" {
			return tag
		}
		return reqNodeName + "-" + tag
	}
	if reqNodeName == "" {
		return tag
	}
	return reqNodeName
}

// resolveFastAddCert resolves TLS certificate paths from params.
// Priority: certificateFile/keyFile > certificate/key (PEM) > domain lookup > self_signed.
// Returns (certFile, keyFile, certSource, certDomain, shouldCleanup, error).
// certSource is one of: "file", "pem", "domain", "self_signed".
// shouldCleanup is true when the caller created temp files that must be deleted on inbound removal.
func (e *Executor) resolveFastAddCert(params map[string]any) (certFile, keyFile, certSource, certDomain string, shouldCleanup bool, err error) {
	cf, _ := params["certificateFile"].(string)
	kf, _ := params["keyFile"].(string)
	cert, _ := params["certificate"].(string)
	key, _ := params["key"].(string)
	domain, _ := params["domain"].(string)
	selfSigned, _ := params["self_signed"].(bool)

	switch {
	case cf != "" && kf != "":
		// Case 1: explicit file paths — caller manages lifecycle
		return cf, kf, "file", "", false, nil

	case cert != "" && key != "":
		// Case 2: PEM content — write to temp files, must clean up on removal
		cf2, kf2, tmpErr := writeCertToTempFiles(cert, key)
		return cf2, kf2, "pem", "", true, tmpErr

	case domain != "":
		// Case 3: lookup from certManager — files managed externally
		if e.certManager == nil {
			return "", "", "", "", false, errs.Newf(errs.ErrCertRequired,
				"domain provided but no certManager configured; call SetCertManager first")
		}
		record := e.certManager.GetCert(domain)
		if record == nil {
			return "", "", "", "", false, errs.Newf(errs.ErrCertNotFound,
				"no certificate found for domain %q; issue a certificate first", domain)
		}
		return record.CertFile, record.KeyFile, "domain", domain, false, nil

	case selfSigned:
		// Case 4: self-signed cert — write to temp files, must clean up on removal
		serverName := fastGetString(params, "server_name", "localhost")
		certPEM, keyPEM, genErr := GenerateSelfSignedCert(serverName)
		if genErr != nil {
			return "", "", "", "", false, errs.Wrap(errs.ErrFastAddInboundFailed, "failed to generate TLS certificate", genErr)
		}
		cf2, kf2, tmpErr := writeCertToTempFiles(certPEM, keyPEM)
		return cf2, kf2, "self_signed", "", true, tmpErr

	default:
		return "", "", "", "", false, errs.Newf(errs.ErrCertRequired,
			"TLS requires a certificate; provide certificateFile/keyFile, domain, or set self_signed=true")
	}
}

// writeCertToTempFiles writes PEM content to temp files and returns their paths.
func writeCertToTempFiles(certPEM, keyPEM string) (certFile, keyFile string, err error) {
	certF, err := os.CreateTemp("", "xray-fastadd-cert-*.pem")
	if err != nil {
		return "", "", errs.Wrap(errs.ErrFastAddInboundFailed, "failed to create certificate temp file", err)
	}
	keyF, keyErr := os.CreateTemp("", "xray-fastadd-key-*.pem")
	if keyErr != nil {
		certF.Close()
		os.Remove(certF.Name())
		return "", "", errs.Wrap(errs.ErrFastAddInboundFailed, "failed to create key temp file", keyErr)
	}
	if _, writeErr := certF.WriteString(certPEM); writeErr != nil {
		certF.Close()
		keyF.Close()
		os.Remove(certF.Name())
		os.Remove(keyF.Name())
		return "", "", errs.Wrap(errs.ErrFastAddInboundFailed, "failed to write certificate file", writeErr)
	}
	if _, writeErr := keyF.WriteString(keyPEM); writeErr != nil {
		certF.Close()
		keyF.Close()
		os.Remove(certF.Name())
		os.Remove(keyF.Name())
		return "", "", errs.Wrap(errs.ErrFastAddInboundFailed, "failed to write key file", writeErr)
	}
	certF.Close()
	keyF.Close()
	return certF.Name(), keyF.Name(), nil
}

// credentialLessInbound reports whether a vmess/vless/trojan inbound has no
// usable default credential (empty settings.clients), which makes it a dead
// inbound nobody can authenticate to and which poisons subscription generation.
// socks5/http (per-user AuthToken) and shadowsocks (settings-level password)
// are intentionally excluded.
func credentialLessInbound(protocol contracts.Protocol, defaultClientUUID, defaultPassword string) bool {
	switch protocol {
	case contracts.ProtocolVMess, contracts.ProtocolVLess:
		return defaultClientUUID == ""
	case contracts.ProtocolTrojan:
		return defaultPassword == ""
	default:
		return false
	}
}

// buildFastAddSimpleSpec builds a minimal InboundSpec for security=none protocols.
func buildFastAddSimpleSpec(tag string, port uint32, protocol contracts.Protocol, params map[string]any) (contracts.InboundSpec, error) {
	extensions := map[string]any{
		"security":    "none",
		"transport":   fastGetString(params, "transport", "tcp"),
		"listen_addr": fastGetString(params, "listen", "127.0.0.1"),
	}
	// Credentials must go into extensions["users"] — the only key Adapter.ToProvider
	// reads to build settings.clients. The old extensions["uuid"]/["password"]
	// keys were never read, so the adapter produced clients:[] and the inbound was
	// unusable (no user could authenticate), which then failed the credential
	// lookup at subscription time and fail-fast killed the user's whole
	// subscription. Field names mirror MapUsers / profilegen exactly.
	switch protocol {
	case contracts.ProtocolVMess:
		uuidStr := fastGetString(params, "uuid", "")
		if uuidStr == "" {
			uuidStr = generateUUID()
		}
		extensions["users"] = []map[string]any{{
			"email":    "auto@vmess.local",
			"uuid":     uuidStr,
			"alter_id": uint32(0),
		}}
	case contracts.ProtocolVLess:
		uuidStr := fastGetString(params, "uuid", "")
		if uuidStr == "" {
			uuidStr = generateUUID()
		}
		user := map[string]any{
			"email": "auto@vless.local",
			"uuid":  uuidStr,
		}
		// security=none is plaintext; flow (xtls-vision) requires TLS/reality, so
		// only carry it through when explicitly provided.
		if flow := fastGetString(params, "flow", ""); flow != "" {
			user["flow"] = flow
		}
		extensions["users"] = []map[string]any{user}
	case contracts.ProtocolTrojan:
		password := fastGetString(params, "password", "")
		if password == "" {
			password = generateTrojanPassword()
		}
		extensions["users"] = []map[string]any{{
			"email":    "auto@trojan.local",
			"password": password,
		}}
	case contracts.ProtocolShadowsocks:
		method := fastGetString(params, "method", "")
		// Simple path only supports 2022 methods
		if !isValidShadowsocksMethod(method, false) {
			return contracts.InboundSpec{}, errs.Newf(errs.ErrFastAddInboundFailed,
				"shadowsocks simple path only allows 2022 methods, got: %s", method)
		}
		extensions["method"] = method
	}
	return contracts.InboundSpec{
		Tag:        tag,
		Port:       port,
		Protocol:   protocol,
		Extensions: extensions,
	}, nil
}

// fastGetString extracts a string value from params with a default.
func fastGetString(params map[string]any, key, defaultVal string) string {
	if v, ok := params[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

// fastGetUint32 extracts a uint32 value from params with a default.
func fastGetUint32(params map[string]any, key string, defaultVal uint32) uint32 {
	if v, ok := params[key].(float64); ok {
		return uint32(v)
	}
	if v, ok := params[key].(uint32); ok {
		return v
	}
	if v, ok := params[key].(int); ok {
		return uint32(v)
	}
	return defaultVal
}

// fastGetStringSlice extracts a []string value from params.
// Handles both []string (native) and []interface{} (JSON deserialization).
func fastGetStringSlice(params map[string]any, key string) []string {
	if v, ok := params[key].([]string); ok {
		return v
	}
	if v, ok := params[key].([]interface{}); ok {
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// fastGetBool extracts a bool value from params.
func fastGetBool(params map[string]any, key string) bool {
	if v, ok := params[key].(bool); ok {
		return v
	}
	return false
}

// fastGetStringFromMap extracts a string from a map[string]any with a default.
func fastGetStringFromMap(m map[string]any, key, defaultVal string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

// isValidShadowsocksMethod checks if method is a supported Shadowsocks encryption method.
// When allowLegacy is false, only 2022-series methods are accepted.
func isValidShadowsocksMethod(method string, allowLegacy bool) bool {
	switch method {
	case "2022-blake3-aes-256-gcm", "2022-blake3-aes-128-gcm", "2022-blake3-chacha20-poly1305":
		return true
	case "aes-256-gcm", "aes-128-gcm", "chacha20-ietf-poly1305",
		"aes-256-cfb", "aes-128-cfb", "chacha20-ietf", "plain":
		return allowLegacy
	}
	return false
}

// Deliberately no local port generator here. Port selection is the shared
// allocator's job (container.ClaimInboundPort); a private random draw cannot
// see what the forward layer or the other containers already hold, which is
// exactly how identical ports used to be handed out twice.

// generateUUID generates a random UUID v4.
func generateUUID() string {
	// Simplified UUID v4 generation
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// generateTrojanPassword generates a random trojan password.
func generateTrojanPassword() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// generateSSPassword generates a base64-encoded password for Shadowsocks 2022 methods
// or a random password for legacy methods.
func generateSSPassword(method string) (string, error) {
	if strings.HasPrefix(method, "2022-") {
		// 2022 methods need 32-byte key for AES-256 or 24-byte for AES-192
		keySize := 32
		if strings.Contains(method, "aes-128") {
			keySize = 16
		} else if strings.Contains(method, "aes-192") {
			keySize = 24
		} else if strings.Contains(method, "chacha20") {
			keySize = 32
		}
		key := make([]byte, keySize)
		if _, err := rand.Read(key); err != nil {
			return "", err
		}
		return base64.StdEncoding.EncodeToString(key), nil
	}
	// Legacy methods: generate 16-char random password
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 16)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", err
		}
		b[i] = chars[n.Int64()]
	}
	return string(b), nil
}

// Restore reloads all persisted inbounds from the store after a restart.
// When StoreMgr is nil, Restore is a no-op and returns nil.
// For each record, cert handling follows the original cert_source:
//   - "self_signed": generates a fresh self-signed cert from the serverName in native_json
//   - "domain": looks up the cert via certManager (skips with warning if unavailable)
//   - "pem": skips with warning (PEM content is not re-persisted after restart)
//   - "none" / "file": uses native_json as-is
func (e *Executor) Restore(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if e.storeMgr == nil {
		return nil
	}

	records, err := e.storeMgr.InboundStore().Load()
	if err != nil {
		return fmt.Errorf("restore: load inbounds: %w", err)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Tag < records[j].Tag
	})

	cli := e.routingCLI()

	for _, rec := range records {
		// Only restore inbounds belonging to this container type
		if rec.ContainerType != string(contracts.ContainerXray) {
			continue
		}
		// Skip API inbound — already in xray's config, re-adding causes panic
		if rec.Tag == APIInboundTag {
			continue
		}

		inboundType, typeErr := contracts.NormalizeInboundType(rec.InboundType)
		if typeErr != nil {
			log.Error("[Restore] invalid inbound_type, skipping inbound",
				"inbound", rec.Tag, "inbound_type", rec.InboundType, "err", typeErr)
			continue
		}
		accessTags, tagErr := contracts.NormalizeAccessTags(rec.AccessTags)
		if tagErr != nil {
			// Fail-closed: never downgrade access control by clearing invalid tags.
			log.Error("[Restore] invalid access_tags, skipping inbound",
				"inbound", rec.Tag, "err", tagErr)
			continue
		}

		meta := contracts.InboundMeta{
			InboundType:      inboundType,
			AccessTags:       accessTags,
			ChainOutboundURI: rec.ChainOutboundURI,
			Remark:           rec.Remark,
		}

		var chainOutboundJSON []byte
		if inboundType == contracts.InboundTypeChain {
			if strings.TrimSpace(rec.ChainOutboundURI) == "" {
				log.Error("[Restore] chain inbound missing chain_outbound_uri, skipping", "inbound", rec.Tag)
				continue
			}
			_, rendered, chainErr := parseAndRenderXrayChainOutbound(chainOutboundTag(rec.Tag), rec.ChainOutboundURI)
			if chainErr != nil {
				log.Error("[Restore] failed to render chain outbound, skipping",
					"inbound", rec.Tag, "chain_uri", chain.RedactURI(rec.ChainOutboundURI), "err", chainErr)
				continue
			}
			chainOutboundJSON = rendered
		}

		nativeJSON := make([]byte, len(rec.NativeJSON))
		copy(nativeJSON, rec.NativeJSON)

		var tempCertFiles []string

		// If the inbound has no TLS settings but cert_source is set (stale/corrupt metadata),
		// skip cert handling and restore with the original native JSON as-is.
		certSource := rec.CertSource
		if certSource != "" && certSource != "none" && certSource != "file" && !nativeJSONHasTLSSettings(nativeJSON) {
			log.Warn("[Restore] cert_source set but inbound has no tlsSettings, restoring as-is",
				"inbound", rec.Tag, "cert_source", certSource)
			certSource = "none"
		}

		switch certSource {
		case "self_signed":
			// Re-generate a fresh self-signed cert (old temp files are gone after restart)
			serverName := extractTLSServerName(nativeJSON)
			if serverName == "" {
				serverName = "localhost"
			}
			certPEM, keyPEM, genErr := GenerateSelfSignedCert(serverName)
			if genErr != nil {
				log.Error("[Restore] failed to generate self-signed cert", "inbound", rec.Tag, "err", genErr)
				continue
			}
			certFile, keyFile, tmpErr := writeCertToTempFiles(certPEM, keyPEM)
			if tmpErr != nil {
				log.Error("[Restore] failed to write cert files", "inbound", rec.Tag, "err", tmpErr)
				continue
			}
			updated, updateErr := updateTLSCertPaths(nativeJSON, certFile, keyFile)
			if updateErr != nil {
				log.Error("[Restore] failed to update cert paths", "inbound", rec.Tag, "err", updateErr)
				_ = os.Remove(certFile)
				_ = os.Remove(keyFile)
				continue
			}
			nativeJSON = updated
			tempCertFiles = []string{certFile, keyFile}

		case "domain":
			if e.certManager == nil {
				log.Warn("[Restore] cert_source=domain but no certManager set, skipping", "inbound", rec.Tag)
				continue
			}
			certRecord := e.certManager.GetCert(rec.CertDomain)
			if certRecord == nil {
				log.Warn("[Restore] no cert found for domain, skipping", "domain", rec.CertDomain, "inbound", rec.Tag)
				continue
			}
			updated, updateErr := updateTLSCertPaths(nativeJSON, certRecord.CertFile, certRecord.KeyFile)
			if updateErr != nil {
				log.Error("[Restore] failed to update cert paths", "inbound", rec.Tag, "err", updateErr)
				continue
			}
			nativeJSON = updated

		case "pem":
			certPEM, keyPEM, pemErr := extractPEMFromNativeJSON(nativeJSON)
			if pemErr != nil {
				log.Error("[Restore] failed to extract PEM from native_json", "inbound", rec.Tag, "err", pemErr)
				continue
			}
			expired, expErr := isCertExpired(certPEM)
			if expErr != nil {
				log.Error("[Restore] failed to check cert expiry", "inbound", rec.Tag, "err", expErr)
				continue
			}
			if expired {
				log.Warn("[Restore] pem cert expired, skipping", "inbound", rec.Tag)
				continue
			}
			certFile, keyFile, tmpErr := writeCertToTempFiles(certPEM, keyPEM)
			if tmpErr != nil {
				log.Error("[Restore] failed to write pem cert files", "inbound", rec.Tag, "err", tmpErr)
				continue
			}
			updated, updateErr := updateTLSCertPaths(nativeJSON, certFile, keyFile)
			if updateErr != nil {
				log.Error("[Restore] failed to update cert paths for pem", "inbound", rec.Tag, "err", updateErr)
				_ = os.Remove(certFile)
				_ = os.Remove(keyFile)
				continue
			}
			nativeJSON = updated
			tempCertFiles = []string{certFile, keyFile}

		default:
			// "none" or "file": use native_json as-is
		}

		outTag := chainOutboundTag(rec.Tag)
		rTag := chainRuleTag(rec.Tag)
		if inboundType == contracts.InboundTypeChain {
			_ = cli.RemoveRule(ctx, rTag)
			_ = cli.RemoveOutbound(ctx, outTag)

			if err := cli.AddOutbound(ctx, chainOutboundJSON); err != nil {
				log.Error("[Restore] failed to replay chain outbound",
					"inbound", rec.Tag, "chain_uri", chain.RedactURI(rec.ChainOutboundURI), "err", err)
				for _, f := range tempCertFiles {
					_ = os.Remove(f)
				}
				continue
			}
			if err := cli.AddRule(ctx, rTag, rec.Tag, outTag); err != nil {
				log.Error("[Restore] failed to replay chain rule", "inbound", rec.Tag, "ruleTag", rTag, "err", err)
				_ = cli.RemoveOutbound(ctx, outTag)
				for _, f := range tempCertFiles {
					_ = os.Remove(f)
				}
				continue
			}
		}

		if err := e.addInboundNativeInternal(nativeJSON, inboundAddOptions{
			meta:          meta,
			certSource:    certSource,
			certDomain:    rec.CertDomain,
			tempCertFiles: tempCertFiles,
			persist:       false,
			reconcile:     false,
		}); err != nil {
			log.Error("[Restore] failed to restore inbound", "inbound", rec.Tag, "err", err)
			if inboundType == contracts.InboundTypeChain {
				_ = cli.RemoveRule(ctx, rTag)
				_ = cli.RemoveOutbound(ctx, outTag)
			}
			for _, f := range tempCertFiles {
				_ = os.Remove(f)
			}
			continue
		}
	}

	// Sync inbounds from running xray process (catches inbounds defined in config.json
	// that were never registered via FastAddInbound/AddInboundNative).
	e.syncInboundsFromXray()

	// Reconcile users after restore to rebuild forward rules
	e.reconcileUsers()

	return nil
}

// syncInboundsFromXray queries the running xray process via gRPC to discover all inbounds
// and registers any that are not yet in the in-memory map.
// This handles inbounds defined statically in xray's config.json.
func (e *Executor) syncInboundsFromXray() {
	api := NewXrayAPI(e.grpcAPIAddress, e.config.Debug)
	inbounds, err := api.ListInbounds()
	if err != nil {
		log.Warn("[syncInboundsFromXray] failed to list inbounds from xray", "err", err)
		return
	}

	for _, inboundCfg := range inbounds {
		tag := inboundCfg.GetTag()
		if tag == "" || tag == APIInboundTag {
			continue
		}

		// Skip if already registered
		e.inboundsMu.RLock()
		_, exists := e.inbounds[tag]
		e.inboundsMu.RUnlock()
		if exists {
			continue
		}

		// Convert InboundHandlerConfig back to JSON and register
		nativeJSON, err := json.Marshal(inboundCfg)
		if err != nil {
			log.Warn("[syncInboundsFromXray] marshal inbound failed", "tag", tag, "err", err)
			continue
		}

		if err := e.AddInboundNative(nativeJSON); err != nil {
			log.Warn("[syncInboundsFromXray] register inbound failed", "tag", tag, "err", err)
			continue
		}

		log.Info("[syncInboundsFromXray] registered inbound from xray config", "tag", tag)
	}
}

// nativeJSONHasTLSSettings returns true if the native xray inbound JSON contains
// streamSettings.tlsSettings (regardless of whether serverName is set).
// Used to guard cert-handling in Restore for inbounds that have stale cert_source metadata.
func nativeJSONHasTLSSettings(nativeJSON []byte) bool {
	var raw map[string]interface{}
	if err := json.Unmarshal(nativeJSON, &raw); err != nil {
		return false
	}
	ss, ok := raw["streamSettings"].(map[string]interface{})
	if !ok {
		return false
	}
	_, hasTLS := ss["tlsSettings"]
	return hasTLS
}

// extractTLSServerName parses streamSettings.tlsSettings.serverName from native xray inbound JSON.
// Returns "" if not found or on parse error.
func extractTLSServerName(nativeJSON []byte) string {
	var raw map[string]interface{}
	if err := json.Unmarshal(nativeJSON, &raw); err != nil {
		return ""
	}
	ss, ok := raw["streamSettings"].(map[string]interface{})
	if !ok {
		return ""
	}
	tls, ok := ss["tlsSettings"].(map[string]interface{})
	if !ok {
		return ""
	}
	sn, _ := tls["serverName"].(string)
	return sn
}

// extractPEMFromNativeJSON extracts certificate and key PEM strings from xray native inbound JSON.
// It reads streamSettings.tlsSettings.certificates[0].certificate and key (string arrays) and
// joins them into complete PEM strings.
func extractPEMFromNativeJSON(nativeJSON []byte) (certPEM, keyPEM string, err error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(nativeJSON, &raw); err != nil {
		return "", "", fmt.Errorf("extractPEMFromNativeJSON: unmarshal: %w", err)
	}

	ss, ok := raw["streamSettings"].(map[string]interface{})
	if !ok {
		return "", "", fmt.Errorf("extractPEMFromNativeJSON: streamSettings not found")
	}
	tls, ok := ss["tlsSettings"].(map[string]interface{})
	if !ok {
		return "", "", fmt.Errorf("extractPEMFromNativeJSON: tlsSettings not found")
	}
	certs, _ := tls["certificates"].([]interface{})
	if len(certs) == 0 {
		return "", "", fmt.Errorf("extractPEMFromNativeJSON: certificates empty")
	}
	cert0, ok := certs[0].(map[string]interface{})
	if !ok {
		return "", "", fmt.Errorf("extractPEMFromNativeJSON: certificates[0] invalid")
	}

	joinLines := func(field string) (string, error) {
		arr, ok := cert0[field].([]interface{})
		if !ok || len(arr) == 0 {
			return "", fmt.Errorf("extractPEMFromNativeJSON: %s not found or empty", field)
		}
		lines := make([]string, 0, len(arr))
		for _, v := range arr {
			s, ok := v.(string)
			if !ok {
				return "", fmt.Errorf("extractPEMFromNativeJSON: %s contains non-string element", field)
			}
			lines = append(lines, s)
		}
		return strings.Join(lines, "\n"), nil
	}

	certPEM, err = joinLines("certificate")
	if err != nil {
		return "", "", err
	}
	keyPEM, err = joinLines("key")
	if err != nil {
		return "", "", err
	}
	return certPEM, keyPEM, nil
}

// isCertExpired parses a PEM-encoded certificate and returns true if it has expired.
func isCertExpired(certPEM string) (bool, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return false, fmt.Errorf("isCertExpired: failed to decode PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, fmt.Errorf("isCertExpired: parse certificate: %w", err)
	}
	return time.Now().After(cert.NotAfter), nil
}

// updateTLSCertPaths updates certificateFile and keyFile in streamSettings.tlsSettings.certificates[0]
// of the native xray inbound JSON and returns the updated (pretty-printed) JSON.
func updateTLSCertPaths(nativeJSON []byte, certFile, keyFile string) ([]byte, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(nativeJSON, &raw); err != nil {
		return nil, fmt.Errorf("updateTLSCertPaths: unmarshal: %w", err)
	}

	ss, ok := raw["streamSettings"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("updateTLSCertPaths: streamSettings not found")
	}
	tls, ok := ss["tlsSettings"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("updateTLSCertPaths: tlsSettings not found")
	}

	certs, _ := tls["certificates"].([]interface{})
	if len(certs) == 0 {
		// Create a new certificate entry
		tls["certificates"] = []interface{}{
			map[string]interface{}{
				"certificateFile": certFile,
				"keyFile":         keyFile,
			},
		}
	} else {
		// Update the first certificate entry
		cert0, ok := certs[0].(map[string]interface{})
		if !ok {
			cert0 = make(map[string]interface{})
			certs[0] = cert0
		}
		cert0["certificateFile"] = certFile
		cert0["keyFile"] = keyFile
	}

	return json.MarshalIndent(raw, "", "  ")
}

// Verify interface compliance.
var _ container.Container = (*Executor)(nil)
var _ container.RuntimeAPI = (*Executor)(nil)
var _ container.ChainCapable = (*Executor)(nil)
var _ container.InboundTagUpdater = (*Executor)(nil)
var _ container.InboundMetaProvider = (*Executor)(nil)
var _ inbound.Inbound = (*XrayInbound)(nil)

// init registers the xray container to the global registry.
// This allows users to get xray containers via container.GetContainer(contracts.ContainerXray).
func init() {
	// Register a factory function that creates xray executors
	// Users can use container.NewContainer(contracts.ContainerXray) to create new instances
	// Or use container.GetContainer(contracts.ContainerXray) to get the singleton
	container.RegisterContainerFunc(contracts.ContainerXray, func() container.Container {
		// Create a default executor - caller should call Init with proper config
		executor, err := NewExecutor(ExecutorConfig{
			BinaryPath:     "/usr/local/bin/xray",
			ConfigFilePath: "/tmp/xray-default.json",
			ContainerType:  contracts.ContainerXray,
			GRPCAPIAddress: "127.0.0.1:62789",
		})
		if err != nil {
			// This should not happen in normal usage as we provide valid defaults
			panic(fmt.Sprintf("failed to create default xray executor: %v", err))
		}
		return executor
	})
}
