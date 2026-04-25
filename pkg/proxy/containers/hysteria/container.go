package hysteria

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	"github.com/lureiny/v2raymg/pkg/proxy/tools/process"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/store"
)

const defaultInboundTag = "hysteria-default"

// CertReader provides certificate file path lookup.
type CertReader interface {
	GetCertFiles(domain string) (certFile, keyFile string, ok bool)
}

// HysteriaContainer implements the Container interface for hysteria2.
type HysteriaContainer struct {
	*container.BaseContainer
	cfg         HysteriaConfig
	httpPort    int // v2raymg HTTP server port, used for auth callback
	inbound     inbound.Inbound
	runner      *process.Runner
	userMgr     *usermanager.UserManager
	userEventCh chan usermanager.UserEvent
	storeMgr    *store.StoreManager
	certReader  CertReader
	certMgr     certIssuer // triggers cert issuance if not found
	// mu guards addedUsers and inboundEnabled against concurrent access between
	// the user-event handler goroutine (handleUserEvent) and the reconcile-loop
	// goroutine (reconcileUsers). Held only around map/field reads/writes —
	// never while calling userMgr.GetBindPort / ReleaseBindPort.
	mu sync.Mutex
	// addedUsers tracks users for whom a forward UDP rule has been allocated.
	addedUsers map[string]struct{}
	// inboundEnabled controls whether per-user forward rules are active.
	// FastAddInbound sets it true; RemoveInboundConfig sets it false.
	inboundEnabled  bool
	reconcileStopCh chan struct{}
	reconcileWg     sync.WaitGroup
	certWaitStopCh  chan struct{}
}

// hysteriaHooks implements container.Hooks for HysteriaContainer.
type hysteriaHooks struct {
	c *HysteriaContainer
}

func (h *hysteriaHooks) GetRunFunc() (func() error, func() error) {
	run := func() error {
		// Restore persisted inbound config before starting
		if err := h.c.restoreInboundConfig(); err != nil {
			slog.Warn("hysteria: restore inbound config failed", "err", err)
		}
		if err := h.c.saveInboundConfig(); err != nil {
			slog.Warn("hysteria: save inbound config failed", "err", err)
		}

		// Start user event handler and periodic reconcile loop
		h.c.startUserEventHandler()
		h.c.startReconcileLoop()

		// Initialize cert wait stop channel
		h.c.certWaitStopCh = make(chan struct{})

		// Start cert wait and process in background
		go h.c.waitForCertAndStart()

		return nil
	}
	stop := func() error {
		// Stop reconcile loop first
		h.c.stopReconcileLoop()
		// Close cert wait stop channel
		h.c.closeCertWaitStopCh()
		// Close user event channel
		h.c.closeUserEventCh()
		// Stop the hysteria process
		return h.c.stopProcess()
	}
	return run, stop
}

// HysteriaOption configures optional dependencies for HysteriaContainer.
type HysteriaOption func(*HysteriaContainer)

// WithStoreMgr sets the store manager for inbound persistence.
func WithStoreMgr(sm *store.StoreManager) HysteriaOption {
	return func(hc *HysteriaContainer) {
		hc.storeMgr = sm
	}
}

// WithUserManager sets the user manager for user event handling.
func WithUserManager(um *usermanager.UserManager) HysteriaOption {
	return func(hc *HysteriaContainer) {
		hc.userMgr = um
	}
}

// WithCertReader sets the certificate reader for TLS.
func WithCertReader(cr CertReader) HysteriaOption {
	return func(hc *HysteriaContainer) {
		hc.certReader = cr
	}
}

// WithCertManager sets the certificate manager for TLS cert lookup and issuance.
func WithCertManager(ce certIssuer) HysteriaOption {
	return func(hc *HysteriaContainer) {
		hc.certMgr = ce
	}
}

// WithHTTPPort sets the v2raymg HTTP server port for auth callbacks.
func WithHTTPPort(port int) HysteriaOption {
	return func(hc *HysteriaContainer) {
		hc.httpPort = port
	}
}

// NewHysteriaContainer creates a new HysteriaContainer from the given config.
func NewHysteriaContainer(cfg HysteriaConfig, opts ...HysteriaOption) (*HysteriaContainer, error) {
	hc := &HysteriaContainer{
		cfg:            cfg,
		addedUsers:     make(map[string]struct{}),
		inboundEnabled: true,
	}
	for _, opt := range opts {
		opt(hc)
	}
	hc.BaseContainer = container.NewBaseContainer(contracts.ContainerHysteria, &hysteriaHooks{c: hc})

	hc.inbound = inbound.NewDefaultInbound(
		defaultInboundTag,
		contracts.ProtocolHysteria2,
		uint32(cfg.Port),
	)
	// The hysteria process binds loopback only (see process.go's generateConfigFile);
	// reflect that in the inbound metadata so upstream consumers don't get misled by
	// cfg.Listen, which is now just a persisted configuration hint.
	hc.inbound.(*inbound.DefaultInbound).SetListenAddr("127.0.0.1")

	// Subscribe to user events via pub/sub channel
	if hc.userMgr != nil {
		hc.userEventCh = make(chan usermanager.UserEvent, 100)
		go hc.forwardUserEvents(hc.userMgr.Subscribe())
	}

	return hc, nil
}

// waitForCertAndStart polls for certificate availability.
// If the certificate does not exist, triggers issuance via certMgr.Issue() in a
// background goroutine, then continues polling until the cert is ready.
// If cert_file and key_file are set in config (direct path), skip polling and start immediately.
func (hc *HysteriaContainer) waitForCertAndStart() {
	// Direct cert path configured — no polling needed, start immediately
	if hc.cfg.CertFile != "" && hc.cfg.KeyFile != "" {
		slog.Info("hysteria: using direct cert path, starting process",
			"cert", hc.cfg.CertFile, "key", hc.cfg.KeyFile)
		if err := hc.startProcess(); err != nil {
			slog.Error("hysteria: start process failed", "err", err)
		}
		return
	}

	if hc.certReader == nil && hc.certMgr == nil {
		slog.Error("hysteria: no certificate source: certReader/certMgr not configured and cert_file/key_file not set")
		return
	}

	hasCert := func() bool {
		if hc.certReader != nil {
			_, _, ok := hc.certReader.GetCertFiles(hc.cfg.Domain)
			return ok
		}
		if hc.certMgr != nil {
			_, _, ok := hc.certMgr.GetCertFiles(hc.cfg.Domain)
			return ok
		}
		return false
	}

	// Try immediately first
	if hasCert() {
		if err := hc.startProcess(); err != nil {
			slog.Error("hysteria: start process failed", "err", err)
		}
		return
	}

	// Cert not found — trigger issuance in background (only if certMgr available)
	slog.Info("hysteria: certificate not found, triggering issuance", "domain", hc.cfg.Domain)
	if hc.certMgr != nil {
		go func() {
			ctx := context.Background()
			if _, err := hc.certMgr.Issue(ctx, []string{hc.cfg.Domain}); err != nil {
				slog.Warn("hysteria: certificate issuance failed", "domain", hc.cfg.Domain, "err", err)
			} else {
				slog.Info("hysteria: certificate issuance completed", "domain", hc.cfg.Domain)
			}
		}()
	} else {
		slog.Warn("hysteria: certMgr not available, cannot trigger issuance; waiting for cert to appear", "domain", hc.cfg.Domain)
	}

	slog.Info("hysteria: waiting for certificate", "domain", hc.cfg.Domain, "interval", "30s")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-hc.certWaitStopCh:
			return
		case <-ticker.C:
			if hasCert() {
				slog.Info("hysteria: certificate ready, starting process", "domain", hc.cfg.Domain)
				if err := hc.startProcess(); err != nil {
					slog.Error("hysteria: start process failed", "err", err)
				}
				return
			}
			slog.Info("hysteria: still waiting for certificate", "domain", hc.cfg.Domain)
		}
	}
}

// closeCertWaitStopCh closes the cert wait stop channel if open.
func (hc *HysteriaContainer) closeCertWaitStopCh() {
	if hc.certWaitStopCh != nil {
		select {
		case <-hc.certWaitStopCh:
			// Already closed
		default:
			close(hc.certWaitStopCh)
		}
	}
}

// Init initializes the container with the given configuration.
func (hc *HysteriaContainer) Init(config any) error {
	cfg, ok := config.(*HysteriaConfig)
	if !ok || cfg == nil {
		return fmt.Errorf("hysteria: invalid config type, expected *HysteriaConfig")
	}
	hc.cfg = *cfg

	hc.inbound = inbound.NewDefaultInbound(
		defaultInboundTag,
		contracts.ProtocolHysteria2,
		uint32(cfg.Port),
	)
	// Actual listen is always 127.0.0.1 (see process.go); keep inbound metadata aligned.
	hc.inbound.(*inbound.DefaultInbound).SetListenAddr("127.0.0.1")

	return nil
}

// Reload is a no-op for hysteria.
func (hc *HysteriaContainer) Reload() error {
	return nil
}

// Version returns the configured version string.
func (hc *HysteriaContainer) Version() string {
	return hc.cfg.Version
}

// ConfigFile returns the config file path.
func (hc *HysteriaContainer) ConfigFile() string {
	return hc.cfg.ConfigFilePath
}

// Update downloads a new version, restarts the process, and returns the result.
func (hc *HysteriaContainer) Update(_ context.Context, req container.UpdateRequest) (*container.UpdateResult, error) {
	targetVersion := req.TargetTag
	if targetVersion == "" {
		targetVersion = hc.cfg.Version
	}

	fromVersion := hc.cfg.Version

	if req.DryRun {
		return &container.UpdateResult{
			FromVersion: fromVersion,
			ToVersion:   targetVersion,
		}, nil
	}

	// Download new version to a temp path
	tmpBinary := hc.cfg.BinaryPath + ".new"
	if err := downloadHysteria(targetVersion, tmpBinary); err != nil {
		return nil, fmt.Errorf("hysteria: download update: %w", err)
	}

	// Stop current process
	if err := hc.Stop(); err != nil {
		return nil, fmt.Errorf("hysteria: stop for update: %w", err)
	}

	// Backup current binary for rollback
	backupPath := hc.cfg.BinaryPath + ".bak"
	hasBackup := false
	if _, err := os.Stat(hc.cfg.BinaryPath); err == nil {
		if err := os.Rename(hc.cfg.BinaryPath, backupPath); err != nil {
			os.Remove(tmpBinary)
			_ = hc.Start()
			return nil, fmt.Errorf("hysteria: backup binary for rollback: %w", err)
		}
		hasBackup = true
	}

	// Atomic swap: move new binary into place
	if err := os.Rename(tmpBinary, hc.cfg.BinaryPath); err != nil {
		os.Remove(tmpBinary)
		if hasBackup {
			_ = os.Rename(backupPath, hc.cfg.BinaryPath)
		}
		_ = hc.Start()
		return nil, fmt.Errorf("hysteria: replace binary: %w", err)
	}

	hc.cfg.Version = targetVersion

	// Start new process
	if err := hc.Start(); err != nil {
		if hasBackup {
			_ = os.Rename(backupPath, hc.cfg.BinaryPath)
			_ = hc.Start()
		}
		return nil, fmt.Errorf("hysteria: start after update: %w", err)
	}

	if hasBackup {
		os.Remove(backupPath)
	}

	return &container.UpdateResult{
		FromVersion: fromVersion,
		ToVersion:   targetVersion,
		Restarted:   true,
	}, nil
}

// RemoveInboundConfig disables the default inbound: tears down all per-user
// forward rules and persists the disabled state. The hysteria process keeps running.
func (hc *HysteriaContainer) RemoveInboundConfig(tag string) error {
	if tag != defaultInboundTag {
		return fmt.Errorf("hysteria: inbound %q not found", tag)
	}
	hc.mu.Lock()
	hc.inboundEnabled = false
	hc.mu.Unlock()
	hc.releaseAllForwardRules()
	if err := hc.saveInboundConfig(); err != nil {
		slog.Warn("hysteria: save inbound config on disable failed", "err", err)
	}
	return nil
}

// releaseAllForwardRules tears down all per-user UDP forward rules and clears addedUsers.
func (hc *HysteriaContainer) releaseAllForwardRules() {
	if hc.userMgr == nil {
		return
	}
	hc.mu.Lock()
	users := make([]string, 0, len(hc.addedUsers))
	for u := range hc.addedUsers {
		users = append(users, u)
	}
	hc.mu.Unlock()

	for _, username := range users {
		port, ok := hc.userMgr.GetUserPortByDstForCleanup(username, uint32(hc.cfg.Port))
		if !ok {
			continue
		}
		if err := hc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
			Username: username,
			BindPort: port,
		}); err != nil {
			slog.Error("hysteria: release port on disable failed", "user", username, "err", err)
		}
	}
	hc.mu.Lock()
	hc.addedUsers = make(map[string]struct{})
	hc.mu.Unlock()
}

// GetInboundConfig returns the default inbound if the tag matches and the inbound is enabled.
func (hc *HysteriaContainer) GetInboundConfig(tag string) (inbound.Inbound, error) {
	if tag != defaultInboundTag {
		return nil, fmt.Errorf("hysteria: inbound %q not found", tag)
	}
	hc.mu.Lock()
	enabled := hc.inboundEnabled
	hc.mu.Unlock()
	if !enabled {
		return nil, fmt.Errorf("hysteria: inbound %q is disabled", tag)
	}
	return hc.inbound, nil
}

// ListInboundConfigs returns the single default inbound, or empty if disabled.
func (hc *HysteriaContainer) ListInboundConfigs() []inbound.Inbound {
	hc.mu.Lock()
	enabled := hc.inboundEnabled
	hc.mu.Unlock()
	if !enabled {
		return nil
	}
	return []inbound.Inbound{hc.inbound}
}

// FastAddInbound enables the default inbound: sets inboundEnabled true, persists
// the state, and reconciles forward rules for all current users.
// tag must equal defaultInboundTag; params are ignored.
func (hc *HysteriaContainer) FastAddInbound(tag string, _ map[string]any) error {
	if tag != defaultInboundTag {
		return fmt.Errorf("hysteria: only the default inbound %q is supported", defaultInboundTag)
	}
	hc.mu.Lock()
	hc.inboundEnabled = true
	hc.mu.Unlock()
	if err := hc.saveInboundConfig(); err != nil {
		slog.Warn("hysteria: save inbound config on enable failed", "err", err)
	}
	hc.reconcileUsers()
	return nil
}

// UserEventChannel returns the container's channel for receiving user events.
func (hc *HysteriaContainer) UserEventChannel() <-chan usermanager.UserEvent {
	return hc.userEventCh
}

// forwardUserEvents forwards events from UserManager to local channel.
func (hc *HysteriaContainer) forwardUserEvents(source <-chan usermanager.UserEvent) {
	for event := range source {
		if hc.userEventCh != nil {
			select {
			case hc.userEventCh <- event:
			default:
			}
		}
	}
}

// startUserEventHandler starts a goroutine to process user events from the channel.
func (hc *HysteriaContainer) startUserEventHandler() {
	if hc.userEventCh == nil {
		return
	}
	go func() {
		for event := range hc.userEventCh {
			hc.handleUserEvent(event)
		}
	}()
}

// handleUserEvent processes a single user event.
//
// Per-user UDP forward model (mirrors snell's TCP flow, see
// pkg/proxy/containers/snell/container.go:handleUserEvent):
//   - UserEventAdd: allocate a public UDP port via GetBindPort that relays
//     into hysteria's loopback cfg.Port.
//   - UserEventRemove: tear down the forward rule and release the port.
//   - UserEventUpdate: re-evaluate visibility (group changes etc.) and
//     add/remove the rule accordingly.
func (hc *HysteriaContainer) handleUserEvent(event usermanager.UserEvent) {
	if hc.userMgr == nil {
		return
	}
	switch event.Type {
	case usermanager.UserEventAdd:
		hc.mu.Lock()
		enabled := hc.inboundEnabled
		_, exists := hc.addedUsers[event.Username]
		hc.mu.Unlock()
		if !enabled || exists {
			return
		}
		// GetBindPort is idempotent, so a concurrent Add winning the race
		// after both threads saw !exists is still correct.
		_, err := hc.userMgr.GetBindPort(usermanager.GetBindPortRequest{
			Username:      event.Username,
			ContainerType: contracts.ContainerHysteria,
			InboundTag:    defaultInboundTag,
			TargetPort:    uint32(hc.cfg.Port),
			Protocol:      contracts.ProtocolHysteria2,
			Network:       "udp",
		})
		if err != nil {
			slog.Error("hysteria: allocate port failed", "user", event.Username, "err", err)
			return
		}
		hc.mu.Lock()
		hc.addedUsers[event.Username] = struct{}{}
		hc.mu.Unlock()

	case usermanager.UserEventRemove:
		hc.mu.Lock()
		delete(hc.addedUsers, event.Username)
		hc.mu.Unlock()
		port, ok := hc.userMgr.GetUserPortByDstForCleanup(event.Username, uint32(hc.cfg.Port))
		if !ok {
			return
		}
		if err := hc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
			Username: event.Username,
			BindPort: port,
		}); err != nil {
			slog.Error("hysteria: release port failed", "user", event.Username, "err", err)
		}

	case usermanager.UserEventUpdate:
		// Re-evaluate visibility after group or other changes.
		if event.User != nil && !hc.userMgr.IsUserVisible(event.User) {
			// User no longer belongs to this node — tear down forwarding rule.
			hc.mu.Lock()
			delete(hc.addedUsers, event.Username)
			hc.mu.Unlock()
			port, ok := hc.userMgr.GetUserPortByDstForCleanup(event.Username, uint32(hc.cfg.Port))
			if ok {
				if err := hc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
					Username: event.Username,
					BindPort: port,
				}); err != nil {
					slog.Error("hysteria: release port on group change failed", "user", event.Username, "err", err)
				}
			}
		} else if event.User != nil {
			// User became visible (e.g. group changed back) — ensure forwarding rule exists.
			hc.mu.Lock()
			enabled := hc.inboundEnabled
			_, exists := hc.addedUsers[event.Username]
			hc.mu.Unlock()
			if !enabled || exists {
				return
			}
			_, err := hc.userMgr.GetBindPort(usermanager.GetBindPortRequest{
				Username:      event.Username,
				ContainerType: contracts.ContainerHysteria,
				InboundTag:    defaultInboundTag,
				TargetPort:    uint32(hc.cfg.Port),
				Protocol:      contracts.ProtocolHysteria2,
				Network:       "udp",
			})
			if err != nil {
				slog.Error("hysteria: allocate port on group change failed", "user", event.Username, "err", err)
				return
			}
			hc.mu.Lock()
			hc.addedUsers[event.Username] = struct{}{}
			hc.mu.Unlock()
		}
	}
}

// saveInboundConfig persists the placeholder inbound config to store.
func (hc *HysteriaContainer) saveInboundConfig() error {
	if hc.storeMgr == nil {
		return nil
	}

	data := map[string]any{
		"tag":      defaultInboundTag,
		"protocol": string(contracts.ProtocolHysteria2),
		"port":     hc.cfg.Port,
		"listen":   hc.cfg.Listen,
		"domain":   hc.cfg.Domain,
		"enabled":  hc.inboundEnabled,
	}
	nativeJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("hysteria: marshal inbound config: %w", err)
	}

	rec := &store.InboundRecord{
		Tag:           defaultInboundTag,
		ContainerType: string(contracts.ContainerHysteria),
		CertSource:    "none",
		NativeJSON:    nativeJSON,
	}
	return hc.storeMgr.InboundStore().Save(rec)
}

// restoreInboundConfig restores the inbound config from store.
func (hc *HysteriaContainer) restoreInboundConfig() error {
	if hc.storeMgr == nil {
		return nil
	}

	records, err := hc.storeMgr.InboundStore().Load()
	if err != nil {
		return fmt.Errorf("hysteria: load inbound records: %w", err)
	}

	for _, rec := range records {
		if rec.Tag != defaultInboundTag {
			continue
		}
		if rec.ContainerType != string(contracts.ContainerHysteria) {
			continue
		}

		var data map[string]any
		if err := json.Unmarshal(rec.NativeJSON, &data); err != nil {
			slog.Warn("hysteria: unmarshal stored inbound config failed", "err", err)
			return nil
		}

		// Only restore from store if the config field is at its zero/default value,
		// so explicit config always wins over persisted state.
		if hc.cfg.Port == 0 {
			if port, ok := data["port"].(float64); ok {
				hc.cfg.Port = int(port)
			}
		}
		if hc.cfg.Listen == "" {
			if listen, ok := data["listen"].(string); ok {
				hc.cfg.Listen = listen
			}
		}
		if hc.cfg.Domain == "" {
			if domain, ok := data["domain"].(string); ok {
				hc.cfg.Domain = domain
			}
		}

		// Restore enabled state; true is the default when the key is absent (old records).
		if enabled, ok := data["enabled"].(bool); ok {
			hc.inboundEnabled = enabled
		}

		// Re-create inbound with restored config.
		// Actual listen is always 127.0.0.1 (see process.go); keep inbound metadata aligned.
		hc.inbound = inbound.NewDefaultInbound(
			defaultInboundTag,
			contracts.ProtocolHysteria2,
			uint32(hc.cfg.Port),
		)
		hc.inbound.(*inbound.DefaultInbound).SetListenAddr("127.0.0.1")
		return nil
	}

	return nil
}

// GetUserSubscriptions returns subscription specs for the given user.
// Returns nil when the inbound is disabled.
//
// The port exposed to the client is the per-user public UDP port allocated
// by the forward layer (NOT hc.cfg.Port, which is now a loopback internal
// port hysteria listens on). If the user has no forward rule yet, we return
// nil, nil so the caller skips this container for this user.
func (hc *HysteriaContainer) GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	if hc.userMgr == nil {
		return nil, nil
	}
	hc.mu.Lock()
	enabled := hc.inboundEnabled
	hc.mu.Unlock()
	if !enabled {
		return nil, nil
	}
	port, ok := hc.userMgr.GetUserPortByDst(req.User.Username, uint32(hc.cfg.Port))
	if !ok {
		return nil, nil
	}

	nodeName := req.NodeName
	if nodeName == "" {
		nodeName = "hysteria2"
	}

	// hysteria2://password@host:port/?[params]#nodeName
	// Standard URI scheme: https://v2.hysteria.network/zh/docs/developers/URI-Scheme/
	params := ""
	if hc.cfg.CertFile != "" {
		// self-signed cert → insecure=1
		params = "insecure=1"
	}
	if params != "" {
		params = "/?" + params
	} else {
		params = "/"
	}
	uri := fmt.Sprintf("hysteria2://%s@%s:%d%s#%s",
		req.User.AuthToken, req.Host, port, params, nodeName)

	spec := contracts.SubscriptionSpec{
		Protocol:   contracts.ProtocolHysteria2,
		Host:       req.Host,
		Port:       port,
		Password:   req.User.AuthToken,
		NodeName:   nodeName,
		InboundTag: defaultInboundTag,
		Username:   req.User.Username,
		URI:        uri,
	}
	return []contracts.SubscriptionSpec{spec}, nil
}

// Restore rebuilds forward rules for all existing users after restart.
func (hc *HysteriaContainer) Restore(ctx context.Context) error {
	hc.reconcileUsers()
	return nil
}

// reconcileUsers syncs all users from UserManager to ensure forward rules exist.
// This is called by Restore and periodically by the reconcile loop.
// GetBindPort is idempotent: if the relay already exists it returns the port;
// if PortMappings has a stale record (relay dead after restart) it recreates the relay.
//
// Concurrency: may run concurrently with handleUserEvent (event handler
// goroutine). All hc.addedUsers reads/writes happen under hc.mu; userMgr
// calls (GetBindPort / ReleaseBindPort / ListUsers /
// GetUserPortByDstForCleanup) are invoked without the lock held.
func (hc *HysteriaContainer) reconcileUsers() {
	if hc.userMgr == nil {
		return
	}

	users := hc.userMgr.ListUsers()

	// Build a set of visible usernames for fast lookup.
	visibleSet := make(map[string]struct{}, len(users))
	for _, u := range users {
		visibleSet[u.Username] = struct{}{}
	}

	// Snapshot tracked usernames under the lock so the range below doesn't
	// race with concurrent handleUserEvent writes.
	hc.mu.Lock()
	tracked := make([]string, 0, len(hc.addedUsers))
	for username := range hc.addedUsers {
		tracked = append(tracked, username)
	}
	hc.mu.Unlock()

	// Remove users that are tracked but no longer visible (e.g. group changed).
	for _, username := range tracked {
		if _, visible := visibleSet[username]; visible {
			continue
		}
		hc.mu.Lock()
		delete(hc.addedUsers, username)
		hc.mu.Unlock()
		port, ok := hc.userMgr.GetUserPortByDstForCleanup(username, uint32(hc.cfg.Port))
		if ok {
			if err := hc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
				Username: username,
				BindPort: port,
			}); err != nil {
				slog.Warn("hysteria: reconcile release port failed", "user", username, "err", err)
			}
		}
	}

	// Add users that are visible but not yet tracked (only when inbound is enabled).
	hc.mu.Lock()
	enabled := hc.inboundEnabled
	hc.mu.Unlock()
	if enabled {
		for _, user := range users {
			hc.mu.Lock()
			_, exists := hc.addedUsers[user.Username]
			hc.mu.Unlock()
			if exists {
				continue
			}
			if _, err := hc.userMgr.GetBindPort(usermanager.GetBindPortRequest{
				Username:      user.Username,
				ContainerType: contracts.ContainerHysteria,
				InboundTag:    defaultInboundTag,
				TargetPort:    uint32(hc.cfg.Port),
				Protocol:      contracts.ProtocolHysteria2,
				Network:       "udp",
			}); err != nil {
				slog.Warn("hysteria: reconcile forward rule failed", "user", user.Username, "err", err)
				continue
			}
			hc.mu.Lock()
			hc.addedUsers[user.Username] = struct{}{}
			hc.mu.Unlock()
		}
	}
}

// startReconcileLoop starts the periodic user reconciliation goroutine.
func (hc *HysteriaContainer) startReconcileLoop() {
	if hc.userMgr == nil {
		return
	}

	hc.reconcileStopCh = make(chan struct{})
	hc.reconcileWg.Add(1)

	go func() {
		defer hc.reconcileWg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				hc.reconcileUsers()
			case <-hc.reconcileStopCh:
				return
			}
		}
	}()
}

// stopReconcileLoop stops the periodic user reconciliation goroutine gracefully.
func (hc *HysteriaContainer) stopReconcileLoop() {
	if hc.reconcileStopCh != nil {
		close(hc.reconcileStopCh)
		hc.reconcileWg.Wait()
		hc.reconcileStopCh = nil
	}
}

// closeUserEventCh closes the user event channel to terminate the forwardUserEvents goroutine.
func (hc *HysteriaContainer) closeUserEventCh() {
	if hc.userEventCh != nil {
		close(hc.userEventCh)
		hc.userEventCh = nil
	}
}
