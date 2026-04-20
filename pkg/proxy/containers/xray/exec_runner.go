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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/containers/xray/profilegen"
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
	userMgr     *usermanager.UserManager
	userEventCh chan usermanager.UserEvent

	// certManager provides certificate lookup for TLS inbounds.
	// Optional: only required when FastAddInbound is called with a domain parameter.
	certManager CertManagerGetter

	// storeMgr provides unified access to persistence stores.
	storeMgr *store.StoreManager

	// Reconcile loop for periodic user sync
	reconcileStopCh chan struct{}
	reconcileWg     sync.WaitGroup
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

	// PortAllocator is used to allocate unique ports for users.
	// If not provided, a default allocator will be created.
	PortAllocator PortAllocatorGetter

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

// PortAllocatorGetter interface for port allocation.
type PortAllocatorGetter interface {
	Allocate() (uint32, error)
	Release(port uint32) error
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
		Runner:          runner,
		config:          cfg,
		grpcAPIAddress:  cfg.GRPCAPIAddress,
		inbounds:        make(map[string]*XrayInbound),
		renderer:        NewRenderer(),
		storeMgr:        cfg.StoreMgr,
	}

	// Wire dependencies from config
	if cfg.UserManager != nil {
		e.userMgr = cfg.UserManager
		e.userEventCh = make(chan usermanager.UserEvent, 100)
		go e.forwardUserEvents(cfg.UserManager.Subscribe())
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
				BinaryPath:   cfg.BinaryPath,
				DownloadDir: binaryDir,
				Owner:        "XTLS",
				Repo:         "Xray-core",
				AssetName:    "Xray-linux-64.zip",
				BinaryName:   "xray",
			}
		}

		updater, err := NewUpdater(updaterConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create updater: %w", err)
		}
		updater.SetProcessController(e)
		e.updater = updater
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
// If not provided, generates a default config with API enabled.
func (e *Executor) EnsureConfig() error {
	if e.config.ConfigFilePath != "" {
		// Check if config file exists
		if _, err := os.Stat(e.config.ConfigFilePath); err == nil {
			return nil // Config exists
		}
	}

	// Generate default config
	return e.generateDefaultConfig()
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
		APIPort:  62789, // 固定端口，与 GRPCAPIAddress 默认值保持一致
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
	e.startUserEventHandler()

	// Step 1b: Start periodic user reconciliation
	e.startReconcileLoop()

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
	return nil
}

// Reload triggers config reload via restart.
func (e *Executor) Reload() error {
	return e.BaseContainer.Restart()
}

// IsRunning checks if the Xray process is running.
// This is needed to satisfy ProcessController interface used by Updater.
func (e *Executor) IsRunning() bool {
	return e.Runner.IsRunning()
}

// Stop stops the Xray process directly (for Updater).
func (e *Executor) Stop() error {
	// Stop the reconcile loop first
	e.stopReconcileLoop()
	return e.Runner.Stop()
}

// Restart restarts the Xray process directly (for Container interface).
func (e *Executor) Restart() error {
	return e.Runner.Restart()
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

// AddInbound adds an inbound via gRPC (for RuntimeAPI compatibility).
// After a successful gRPC call, the inbound is also registered in the in-memory
// map so that GetUserSubscriptions and other local queries can find it.
func (e *Executor) AddInboundNative(nativeInboundJSON []byte) error {
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

	// Build extra map from native JSON for subscription URI generation.
	// Without extra, subscription URIs won't contain transport-specific params.
	extra := extractNativeExtra(raw, transport, security)

	xi := &XrayInbound{
		tag:               tag,
		protocol:          protocol,
		port:              port,
		listenAddr:        listenAddr,
		security:          security,
		transport:         transport,
		nativeJSON:        nativeInboundJSON,
		defaultClientUUID: defaultClientUUID,
		defaultPassword:  defaultPassword,
		extra:             extra,
		addedUsers:        make(map[string]struct{}),
	}

	// Inject user manager dependency
	if e.userMgr != nil {
		xi.SetUserManager(e.userMgr)
	}

	e.inboundsMu.Lock()
	e.inbounds[tag] = xi
	e.inboundsMu.Unlock()

	// Persist to store (best effort — log only, no rollback on failure)
	if e.storeMgr != nil && tag != "" {
		rec := &InboundRecord{
			Tag:           tag,
			ContainerType: string(contracts.ContainerXray),
			CertSource:    "none",
			NativeJSON:    nativeInboundJSON,
		}
		if err := e.storeMgr.InboundStore().Save(rec); err != nil {
			log.Warn("[AddInboundNative] failed to persist inbound", "tag", tag, "err", err)
		}
	}

	// Sync existing users to this new inbound
	e.reconcileUsersForInbound(tag)

	return nil
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
func (e *Executor) forwardUserEvents(source <-chan usermanager.UserEvent) {
	for event := range source {
		if e.userEventCh != nil {
			select {
			case e.userEventCh <- event:
			default:
				// Channel full, event dropped
			}
		}
	}
}

// startUserEventHandler starts a goroutine to process user events from the channel.
// This implements the checkUser logic: when a user is added, it allocates a port,
// creates a forward rule, and adds the user to the inbound. When removed, it reverses.
func (e *Executor) startUserEventHandler() {
	if e.userEventCh == nil {
		return
	}

	go func() {
		for event := range e.userEventCh {
			e.handleUserEvent(event)
		}
	}()
}

// handleUserEvent processes a single user event from usermanager.
// This implements the port mapping flow:
// - Add: allocate forward port -> create forward rule -> add user to inbound
// - Remove: remove user from inbound -> release forward port
func (e *Executor) handleUserEvent(event usermanager.UserEvent) error {
	if e.userMgr == nil {
		return errs.New(errs.ErrUserManagerRequired,
			"usermanager is required for user event handling; container must have usermanager set")
	}

	switch event.Type {
	case usermanager.UserEventAdd:
		// User added in usermanager - need to sync to inbounds
		e.syncUserToInbound(event.Username, event.User)

	case usermanager.UserEventRemove:
		// User removed from usermanager - need to remove from inbounds
		e.removeUserFromInbounds(event.Username)

	case usermanager.UserEventUpdate:
		// Group (or other visibility-affecting fields) may have changed.
		// Re-evaluate whether the user should have forwarding rules on this node.
		if event.User != nil && !e.userMgr.IsUserVisible(event.User) {
			e.removeUserFromInbounds(event.Username)
		} else if event.User != nil {
			e.syncUserToInbound(event.Username, event.User)
		}
	}

	return nil
}

// syncUserToInbound adds a user to all relevant inbounds.
// It delegates to each inbound's AddUser method which handles forward rule creation
// and internal mapping atomically.
func (e *Executor) syncUserToInbound(username string, user *contracts.User) {
	if user == nil {
		return
	}

	// Get inbounds
	e.inboundsMu.RLock()
	inbounds := make([]*XrayInbound, 0, len(e.inbounds))
	for _, in := range e.inbounds {
		inbounds = append(inbounds, in)
	}
	e.inboundsMu.RUnlock()

	// For each inbound, call the inbound's AddUser method
	// This handles forward rule + user addition + mapping atomically with rollback on failure
	for _, inbound := range inbounds {
		_, err := inbound.AddUser(username, user)
		if err != nil {
			// Log but continue - other inbounds may succeed
			// The inbound's AddUser already handles rollback on failure
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

// reconcileUsers syncs all users from UserManager to all inbounds.
// This ensures forward rules are rebuilt after restart/restore.
func (e *Executor) reconcileUsers() {
	if e.userMgr == nil {
		return
	}

	users := e.userMgr.ListUsers()

	// Build a set of visible usernames for fast lookup.
	visibleSet := make(map[string]struct{}, len(users))
	for _, u := range users {
		visibleSet[u.Username] = struct{}{}
	}

	// Sync to all inbounds
	e.inboundsMu.RLock()
	inbounds := make([]*XrayInbound, 0, len(e.inbounds))
	for _, in := range e.inbounds {
		inbounds = append(inbounds, in)
	}
	e.inboundsMu.RUnlock()

	for _, inbound := range inbounds {
		// Remove users that are tracked but no longer visible (e.g. group changed).
		// Snapshot the tracked user set first so we can iterate without holding the lock,
		// then call RemoveUser (which re-acquires the lock via unmarkAddedUser).
		for _, username := range inbound.listAddedUsers() {
			if _, visible := visibleSet[username]; !visible {
				if err := inbound.RemoveUser(username); err != nil {
					log.Warn("[reconcileUsers] failed to remove stale user from inbound",
						"user", username, "inbound", inbound.Tag(), "err", err)
				}
			}
		}

		// Add users that are visible but not yet tracked.
		for _, user := range users {
			// Skip users already tracked in this inbound's memory
			if inbound.hasAddedUser(user.Username) {
				continue
			}
			if _, err := inbound.AddUser(user.Username, user); err != nil {
				log.Warn("[reconcileUsers] failed to add user to inbound", "user", user.Username, "inbound", inbound.Tag(), "err", err)
			}
		}
	}
}

// reconcileUsersForInbound syncs all users to a specific inbound.
// This is called after a new inbound is added to ensure existing users are mapped.
// inbound must already be registered in e.inbounds before calling this.
// This function does NOT acquire inboundsMu to avoid deadlock with callers that
// already hold the lock — it looks up the inbound itself under RLock.
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

	users := e.userMgr.ListUsers()
	for _, user := range users {
		if inbound.hasAddedUser(user.Username) {
			continue
		}
		if _, err := inbound.AddUser(user.Username, user); err != nil {
			log.Warn("[reconcileUsersForInbound] failed to add user to inbound", "user", user.Username, "inbound", tag, "err", err)
		}
	}
}

// startReconcileLoop starts the periodic user reconciliation goroutine.
func (e *Executor) startReconcileLoop() {
	interval := e.config.ReconcileInterval
	if interval <= 0 {
		interval = 30 * time.Second // default
	}

	e.reconcileStopCh = make(chan struct{})
	e.reconcileWg.Add(1)

	go func() {
		defer e.reconcileWg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				e.reconcileUsers()
			case <-e.reconcileStopCh:
				return
			}
		}
	}()
}

// stopReconcileLoop stops the periodic user reconciliation goroutine gracefully.
func (e *Executor) stopReconcileLoop() {
	if e.reconcileStopCh != nil {
		close(e.reconcileStopCh)
		e.reconcileWg.Wait()
		e.reconcileStopCh = nil
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
// It uses UserManager.GetUserPortByDst as the authoritative source for the frontend port.
// Returns error if the user is not found in UserManager's port mapping.
func (in *XrayInbound) GetSub(req contracts.SubscriptionRequest) (contracts.SubscriptionSpec, error) {
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

	nodeName := req.NodeName
	if nodeName == "" {
		nodeName = in.tag
	}

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
				BinaryPath:   e.config.BinaryPath,
				DownloadDir:  binaryDir,
				Owner:        "XTLS",
				Repo:         "Xray-core",
				AssetName:    "Xray-linux-64.zip",
				BinaryName:   "xray",
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
//   - "port": listen port (defaults to random available port in 4000-5000)
//   - "listen": listen address (defaults to "0.0.0.0")
//   - "security": security mode (defaults to "tls" for non-ss protocols, "none" for ss)
//   - "transport": network transport (defaults to "tcp")
//   - "server_name": SNI name for TLS (defaults to "localhost")
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
//   - tag is empty
//   - protocol is missing or invalid
//   - TLS required but no certificate provided
//   - shadowsocks protocol missing method or password
func (e *Executor) FastAddInbound(tag string, params map[string]any) error {
	// Validate tag
	if tag == "" {
		return errs.Newf(errs.ErrFastAddInboundFailed, "tag is required")
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

	// Resolve port
	port := fastGetUint32(params, "port", 0)
	if port == 0 {
		randomPort, err := generateRandomPort(4000, 5000)
		if err != nil {
			return errs.Wrap(errs.ErrFastAddInboundFailed, "failed to generate random port", err)
		}
		port = randomPort
	}
	if port < 100 || port > 65535 {
		return errs.Newf(errs.ErrFastAddInboundFailed, "port must be between 100 and 65535, got %d", port)
	}

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
	if security == contracts.SecurityTLS {
		var certErr error
		certFile, keyFile, certSource, certDomain, certShouldCleanup, certErr = e.resolveFastAddCert(params)
		if certErr != nil {
			return certErr
		}
	}

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
				ListenAddr: fastGetString(params, "listen", "0.0.0.0"),
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
			p := profilegen.GenerateVLessParams{
				Host:               fastGetString(params, "server_name", "localhost"),
				Transport:          fastGetString(params, "transport", "tcp"),
				Security:           string(security),
				Port:               port,
				Tag:                tag,
				ListenAddr:         fastGetString(params, "listen", "0.0.0.0"),
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
				SniffingEnabled:      fastGetBool(params, "sniffing_enabled"),
				SniffingDestOverride: fastGetStringSlice(params, "sniffing_dest_override"),
				SniffingRouteOnly:    fastGetBool(params, "sniffing_route_only"),
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
				ListenAddr:      fastGetString(params, "listen", "0.0.0.0"),
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
			ListenAddr: fastGetString(params, "listen", "0.0.0.0"),
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
				"listen_addr": fastGetString(params, "listen", "0.0.0.0"),
			},
		}

	default:
		return errs.Newf(errs.ErrFastAddInboundFailed, "unsupported protocol: %s", protocol)
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
	specListenAddr := fastGetStringFromMap(spec.Extensions, "listen_addr", "0.0.0.0")

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

	// Store cert_source in extensions for subscription SNI logic
	if certSource != "none" {
		if spec.Extensions == nil {
			spec.Extensions = make(map[string]any)
		}
		spec.Extensions["cert_source"] = certSource
	}

	xrayInbound := &XrayInbound{
		tag:               tag,
		protocol:          protocol,
		port:              spec.Port,
		listenAddr:        specListenAddr,
		security:          specSecurity,
		transport:         specTransport,
		config:            inbound.NewConfig(tag, protocol, spec.Port),
		extra:             spec.Extensions,
		nativeJSON:        nativeInbound.JSON,
		defaultClientUUID: defaultClientUUID,
		defaultPassword:   defaultPassword,
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

	// Step 4: Add to running xray process via gRPC (direct API call, not AddInboundNative)
	// We don't use AddInboundNative here because it would create and store its own XrayInbound,
	// losing the tempCertFiles and other info we already set up.
	api := NewXrayAPI(e.grpcAPIAddress, e.config.Debug)
	if err := api.AddInbound(nativeInbound.JSON); err != nil {
		// Clean up temp cert files on gRPC failure
		if certShouldCleanup {
			for _, f := range xrayInbound.tempCertFiles {
				os.Remove(f)
			}
		}
		// Map xray's "existing tag" gRPC error to ErrInboundAlreadyExists
		if strings.Contains(err.Error(), "existing tag found") {
			return errs.Newf(errs.ErrInboundAlreadyExists, "inbound %s already exists in xray", tag)
		}
		return errs.Wrap(errs.ErrFastAddInboundFailed, "failed to add inbound to xray process", err)
	}

	// Store in map
	e.inboundsMu.Lock()

	if _, exists := e.inbounds[tag]; exists {
		e.inboundsMu.Unlock()
		return errs.Newf(errs.ErrInboundAlreadyExists, "inbound %s already exists", tag)
	}

	if e.storeMgr != nil {
		rec := &InboundRecord{
			Tag:           tag,
			ContainerType: string(contracts.ContainerXray),
			CertSource:    certSource,
			CertDomain:    certDomain,
			NativeJSON:    nativeInbound.JSON,
		}
		if err := e.storeMgr.InboundStore().Save(rec); err != nil {
			e.inboundsMu.Unlock()
			return errs.Wrap(errs.ErrFastAddInboundFailed, "failed to persist inbound", err)
		}
	}

	e.inbounds[tag] = xrayInbound
	e.inboundsMu.Unlock()

	// Sync existing users to this new inbound (must be outside lock to avoid deadlock,
	// since reconcileUsersForInbound acquires inboundsMu.RLock internally).
	e.reconcileUsersForInbound(tag)

	return nil
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

// buildFastAddSimpleSpec builds a minimal InboundSpec for security=none protocols.
func buildFastAddSimpleSpec(tag string, port uint32, protocol contracts.Protocol, params map[string]any) (contracts.InboundSpec, error) {
	extensions := map[string]any{
		"security":    "none",
		"transport":   fastGetString(params, "transport", "tcp"),
		"listen_addr": fastGetString(params, "listen", "0.0.0.0"),
	}
	switch protocol {
	case contracts.ProtocolVMess, contracts.ProtocolVLess:
		uuidStr := fastGetString(params, "uuid", "")
		if uuidStr == "" {
			uuidStr = generateUUID()
		}
		extensions["uuid"] = uuidStr
	case contracts.ProtocolTrojan:
		password := fastGetString(params, "password", "")
		if password == "" {
			password = generateTrojanPassword()
		}
		extensions["password"] = password
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

// generateRandomPort generates a random port in the given range.
func generateRandomPort(min, max int) (uint32, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if err != nil {
		return 0, err
	}
	return uint32(min) + uint32(n.Int64()), nil
}

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
	if e.storeMgr == nil {
		return nil
	}

	records, err := e.storeMgr.InboundStore().Load()
	if err != nil {
		return fmt.Errorf("restore: load inbounds: %w", err)
	}

	for _, rec := range records {
		// Only restore inbounds belonging to this container type
		if rec.ContainerType != string(contracts.ContainerXray) {
			continue
		}
		// Skip API inbound — already in xray's config, re-adding causes panic
		if rec.Tag == APIInboundTag {
			continue
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

		if err := e.AddInboundNative(nativeJSON); err != nil {
			log.Error("[Restore] failed to restore inbound", "inbound", rec.Tag, "err", err)
			// Clean up any temp cert files we created if AddInboundNative failed
			for _, f := range tempCertFiles {
				_ = os.Remove(f)
			}
			continue
		}

		// For self_signed: record temp cert files in the in-memory inbound for cleanup on removal
		if len(tempCertFiles) > 0 {
			e.inboundsMu.Lock()
			if xi, exists := e.inbounds[rec.Tag]; exists {
				xi.tempCertFiles = tempCertFiles
			}
			e.inboundsMu.Unlock()
		}

		// Re-save the original InboundRecord to DB to preserve cert_source/cert_domain.
		// AddInboundNative internally saves with cert_source="none", which would overwrite
		// the correct cert_source (e.g. "self_signed") we loaded from DB.
		if e.storeMgr != nil {
			if saveErr := e.storeMgr.InboundStore().Save(rec); saveErr != nil {
				log.Warn("[Restore] failed to re-persist cert_source", "inbound", rec.Tag, "err", saveErr)
			}
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
