package hysteria

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
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
	cfg      HysteriaConfig
	httpPort int // v2raymg HTTP server port, used for auth callback
	inbound  inbound.Inbound
	runner   *process.Runner
	userMgr  *usermanager.UserManager
	// userEventCh is created once in NewHysteriaContainer and never closed
	// or reassigned afterwards (immutable after construction), so all
	// goroutines may read the field without locking and sends can never
	// panic on a closed channel. Draining is done by the per-generation
	// user event handler goroutine; while the container is stopped, events
	// accumulate in the buffer (oldest beyond capacity are dropped by
	// forwardUserEvents) and are consumed again after the next Start.
	userEventCh chan usermanager.UserEvent
	storeMgr    *store.StoreManager
	certReader  CertReader
	certMgr     certIssuer // triggers cert issuance if not found
	// mu guards addedUsers, inboundEnabled, accessTags, and remark against
	// concurrent access between the user-event handler goroutine, reconcile loop,
	// and runtime metadata/tag updates. Held only around map/field reads/writes —
	// never while calling userMgr or store methods.
	mu sync.Mutex
	// addedUsers tracks users for whom a forward UDP rule has been allocated.
	addedUsers map[string]struct{}
	// inboundEnabled controls whether per-user forward rules are active.
	// FastAddInbound sets it true; RemoveInboundConfig sets it false.
	inboundEnabled bool
	// accessTags restricts Layer 2 user visibility on the default inbound.
	accessTags []string
	// remark is an optional human-readable suffix for subscription node names.
	remark string
	// persistMu serializes inbound state + store mutations (FastAddInbound,
	// RemoveInboundConfig, SetInboundAccessTags, startup restore/save).
	persistMu sync.Mutex
	// reconcileMu serializes reconcileUsers sweeps so concurrent tag updates
	// converge deterministically to the latest accessTags.
	reconcileMu     sync.Mutex
	reconcileStopCh chan struct{}
	reconcileWg     sync.WaitGroup
	// lifecycleMu guards swaps of the session field (run/stop hooks only).
	// It is never held while cancelling or waiting on a session.
	lifecycleMu sync.Mutex
	// session is the current Start/Stop generation; nil while stopped.
	session *lifecycleSession
	// procMu serializes startProcess/stopProcess across generations so an
	// interleaved Start/Stop can never leak an untracked process.
	procMu sync.Mutex
}

var (
	_ container.Container           = (*HysteriaContainer)(nil)
	_ container.ChainCapable        = (*HysteriaContainer)(nil)
	_ container.InboundTagUpdater   = (*HysteriaContainer)(nil)
	_ container.InboundMetaProvider = (*HysteriaContainer)(nil)
)

// lifecycleSession bundles the stop signal and goroutine tracking for one
// Start/Stop generation. The run hook installs a fresh session; the stop hook
// cancels it and waits for all session goroutines (cert wait, user event
// handler) to exit before killing the process. Goroutines capture ctx by
// value and never read hc.session, so generation swaps cannot race with them.
type lifecycleSession struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// hysteriaHooks implements container.Hooks for HysteriaContainer.
type hysteriaHooks struct {
	c *HysteriaContainer
}

func (h *hysteriaHooks) GetRunFunc() (func() error, func() error) {
	run := func() error {
		// Restore persisted inbound config before starting
		h.c.persistMu.Lock()
		if err := h.c.restoreInboundConfig(); err != nil {
			slog.Warn("hysteria: restore inbound config failed", "err", err)
		}
		if err := h.c.saveInboundConfig(); err != nil {
			slog.Warn("hysteria: save inbound config failed", "err", err)
		}
		h.c.persistMu.Unlock()

		// Install a fresh lifecycle session for this generation. A stale
		// session can survive an interleaved Start/Stop (BaseContainer
		// allows Stopping→Starting); cancel it so its goroutines
		// self-terminate — they hold ctx by value, no waiting needed.
		s := &lifecycleSession{}
		s.ctx, s.cancel = context.WithCancel(context.Background())
		h.c.lifecycleMu.Lock()
		if old := h.c.session; old != nil {
			old.cancel()
		}
		h.c.session = s
		h.c.lifecycleMu.Unlock()

		// Start user event handler and periodic reconcile loop
		h.c.startUserEventHandler(s)
		h.c.startReconcileLoop()

		// Wait for the certificate and start the process in background
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			h.c.waitForCertAndStart(s.ctx)
		}()

		// NOTE: if this hook ever gains an error return path, it must
		// s.cancel() and s.wg.Wait() before returning that error.
		return nil
	}
	stop := func() error {
		// Stop reconcile loop first
		h.c.stopReconcileLoop()
		// Cancel this generation and wait for its goroutines to fully
		// exit BEFORE killing the process: once wg.Wait returns, no
		// session goroutine can call startProcess anymore, so the
		// stopProcess below cannot leave an orphan behind.
		h.c.lifecycleMu.Lock()
		s := h.c.session
		h.c.session = nil
		h.c.lifecycleMu.Unlock()
		if s != nil {
			s.cancel()
			s.wg.Wait()
		}
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

	// The port is fixed by container config (never drawn), so this container
	// does not claim it itself. It is claimed by the startup pre-claim pass in
	// cmd/server.go, together with every persisted inbound port, BEFORE any
	// container is constructed or started — that ordering is what stops an
	// earlier container's user forward ports from taking it. Claiming again
	// here would collide with our own earlier claim and be indistinguishable
	// from a real conflict.
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

// processUp reports whether the hysteria subprocess is actually running right
// now — started and not yet exited. This is distinct from BaseContainer's
// state, which flips to Running the instant the run hook returns, before
// waitForCertAndStart has launched the process (the cert wait runs in the
// background) and regardless of whether cert issuance or startProcess later
// fails. runner is set only on a successful startProcess and its IsRunning()
// reflects the process reaper, so a crash after start also reads as not-up.
func (hc *HysteriaContainer) processUp() bool {
	hc.procMu.Lock()
	defer hc.procMu.Unlock()
	return hc.runner != nil && hc.runner.IsRunning()
}

// IsRunning reports the real process state rather than merely that Start()
// returned. BaseContainer.IsRunning() alone would report Running throughout the
// (potentially minutes-long) certificate wait and even after a failed start, so
// health checks and failover would be misled. Both locks are taken and released
// in sequence, never held together, so there is no ordering hazard with
// startProcess/stopProcess.
func (hc *HysteriaContainer) IsRunning() bool {
	return hc.BaseContainer.IsRunning() && hc.processUp()
}

// State downgrades a BaseContainer "Running" to "Starting" while the process is
// not actually up yet (certificate wait, or a start that has not — or will not —
// succeed), so status reporting reflects the truth. Stopped/Stopping/Starting
// pass through unchanged; the load-bearing signal for callers is that
// IsRunning() is false and State() is not Running until the process is live.
func (hc *HysteriaContainer) State() container.ContainerState {
	if st := hc.BaseContainer.State(); st != container.ContainerStateRunning {
		return st
	}
	if hc.processUp() {
		return container.ContainerStateRunning
	}
	return container.ContainerStateStarting
}

// waitForCertAndStart polls for certificate availability.
// If the certificate does not exist, triggers issuance via certMgr.Issue() in a
// background goroutine, then continues polling until the cert is ready.
// If cert_file and key_file are set in config (direct path), skip polling and start immediately.
//
// ctx is the owning lifecycle session's context: the goroutine exits promptly
// once cancelled, and re-checks ctx before every startProcess call so a
// concurrent Stop cannot be raced into leaving an orphan process behind
// (the stop hook additionally waits for this goroutine before stopProcess).
func (hc *HysteriaContainer) waitForCertAndStart(ctx context.Context) {
	// Direct cert path configured — no polling needed, start immediately
	if hc.cfg.CertFile != "" && hc.cfg.KeyFile != "" {
		slog.Info("hysteria: using direct cert path, starting process",
			"cert", hc.cfg.CertFile, "key", hc.cfg.KeyFile)
		if ctx.Err() != nil {
			return
		}
		if err := hc.startProcess(ctx); err != nil {
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
		if ctx.Err() != nil {
			return
		}
		if err := hc.startProcess(ctx); err != nil {
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
		case <-ctx.Done():
			return
		case <-ticker.C:
			if hasCert() {
				if ctx.Err() != nil {
					return
				}
				slog.Info("hysteria: certificate ready, starting process", "domain", hc.cfg.Domain)
				if err := hc.startProcess(ctx); err != nil {
					slog.Error("hysteria: start process failed", "err", err)
				}
				return
			}
			slog.Info("hysteria: still waiting for certificate", "domain", hc.cfg.Domain)
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
func (hc *HysteriaContainer) Update(ctx context.Context, req container.UpdateRequest) (*container.UpdateResult, error) {
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
	if err := downloadHysteria(ctx, targetVersion, tmpBinary); err != nil {
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

// SupportsChainOutbound reports whether HysteriaContainer supports per-inbound chained outbounds.
// Mode A single-process containers do not support chained outbounds.
func (hc *HysteriaContainer) SupportsChainOutbound() bool {
	return false
}

// ValidateChainURI rejects chained outbound URIs because Mode A hysteria does
// not support per-inbound chained outbounds.
func (hc *HysteriaContainer) ValidateChainURI(rawURI string) error {
	return fmt.Errorf("hysteria: Mode A container does not support chained outbounds; use mihomo container if Hysteria2 chained inbound is needed")
}

// RemoveInboundConfig disables the default inbound: tears down all per-user
// forward rules and persists the disabled state. The hysteria process keeps running.
func (hc *HysteriaContainer) RemoveInboundConfig(tag string) error {
	if tag != defaultInboundTag {
		return fmt.Errorf("hysteria: inbound %q not found", tag)
	}
	hc.persistMu.Lock()
	defer hc.persistMu.Unlock()

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

// GetInboundMeta returns chain and visibility metadata for the default inbound
// when tag matches and the inbound is enabled.
func (hc *HysteriaContainer) GetInboundMeta(tag string) (contracts.InboundMeta, bool) {
	if tag != defaultInboundTag {
		return contracts.InboundMeta{}, false
	}
	hc.mu.Lock()
	enabled := hc.inboundEnabled
	tags := cloneStringSlice(hc.accessTags)
	remark := hc.remark
	hc.mu.Unlock()
	if !enabled {
		return contracts.InboundMeta{}, false
	}
	return contracts.InboundMeta{
		InboundType: contracts.InboundTypeDirect,
		AccessTags:  tags,
		Remark:      remark,
	}, true
}

// SetInboundAccessTags updates the default inbound's visibility tags at runtime
// without restarting the hysteria process, persists the change (rolling back on
// store error), and immediately reconciles user forward rules.
func (hc *HysteriaContainer) SetInboundAccessTags(tag string, accessTags []string) error {
	if tag != defaultInboundTag {
		return fmt.Errorf("hysteria: inbound %q not found", tag)
	}
	normalized, err := contracts.NormalizeAccessTags(accessTags)
	if err != nil {
		return err
	}

	hc.persistMu.Lock()
	hc.mu.Lock()
	if !hc.inboundEnabled {
		hc.mu.Unlock()
		hc.persistMu.Unlock()
		return fmt.Errorf("hysteria: inbound %q is disabled", tag)
	}
	prevTags := cloneStringSlice(hc.accessTags)
	hc.accessTags = cloneStringSlice(normalized)
	hc.mu.Unlock()

	if err := hc.saveInboundConfig(); err != nil {
		hc.mu.Lock()
		hc.accessTags = prevTags
		hc.mu.Unlock()
		hc.persistMu.Unlock()
		return fmt.Errorf("hysteria: persist access tags for %q: %w", tag, err)
	}
	hc.persistMu.Unlock()

	hc.reconcileUsers()
	return nil
}

// FastAddInbound enables the default inbound: validates that chain mode is not
// requested, normalizes optional access_tags and remark, persists the state
// (rolling back on store error), and reconciles forward rules for all current users.
func (hc *HysteriaContainer) FastAddInbound(tag string, params map[string]any) error {
	meta, err := parseFastAddMetaParams(params, hc.ValidateChainURI)
	if err != nil {
		return err
	}
	if tag != defaultInboundTag {
		return fmt.Errorf("hysteria: only the default inbound %q is supported", defaultInboundTag)
	}

	hc.persistMu.Lock()
	hc.mu.Lock()
	prevEnabled := hc.inboundEnabled
	prevTags := cloneStringSlice(hc.accessTags)
	prevRemark := hc.remark
	hc.inboundEnabled = true
	hc.accessTags = cloneStringSlice(meta.accessTags)
	hc.remark = meta.remark
	hc.mu.Unlock()

	if err := hc.saveInboundConfig(); err != nil {
		hc.mu.Lock()
		hc.inboundEnabled = prevEnabled
		hc.accessTags = prevTags
		hc.remark = prevRemark
		hc.mu.Unlock()
		hc.persistMu.Unlock()
		return fmt.Errorf("hysteria: save inbound config on enable: %w", err)
	}
	hc.persistMu.Unlock()

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

// startUserEventHandler starts a goroutine to process user events from the
// channel. The goroutine belongs to the given lifecycle session: it exits on
// session cancel and is restarted by the next generation's run hook, so user
// event handling survives Stop→Start cycles (e.g. the Update() restart path).
func (hc *HysteriaContainer) startUserEventHandler(s *lifecycleSession) {
	if hc.userEventCh == nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.ctx.Done():
				return
			case event := <-hc.userEventCh:
				hc.handleUserEvent(event)
			}
		}
	}()
}

// handleUserEvent processes a single user event.
//
// Per-user UDP forward model (mirrors snell's TCP flow, see
// pkg/proxy/containers/snell/container.go:handleUserEvent):
//   - UserEventAdd / UserEventUpdate: evaluate two-layer visibility via
//     CanUserAccessInbound; allocate a public UDP port via GetBindPort if
//     allowed, or tear down any existing rule if denied.
//   - UserEventRemove: tear down the forward rule and release the port.
func (hc *HysteriaContainer) handleUserEvent(event usermanager.UserEvent) {
	if hc.userMgr == nil {
		return
	}
	switch event.Type {
	case usermanager.UserEventAdd, usermanager.UserEventUpdate:
		hc.mu.Lock()
		enabled := hc.inboundEnabled
		accessTags := cloneStringSlice(hc.accessTags)
		hc.mu.Unlock()

		if !enabled || !hc.userMgr.CanUserAccessInbound(event.User, accessTags) {
			hc.revokeUserForwardRule(event.Username, "access denied")
			return
		}
		hc.ensureUserForwardRule(event.Username, event.User, accessTags)

	case usermanager.UserEventRemove:
		hc.revokeUserForwardRule(event.Username, "remove")
	}
}

func (hc *HysteriaContainer) ensureUserForwardRule(username string, user *contracts.User, checkedTags []string) {
	hc.mu.Lock()
	enabled := hc.inboundEnabled
	_, exists := hc.addedUsers[username]
	hc.mu.Unlock()
	if !enabled || exists {
		return
	}
	// GetBindPort is idempotent, so a concurrent Add winning the race
	// after both threads saw !exists is still correct.
	port, err := hc.userMgr.GetBindPort(usermanager.GetBindPortRequest{
		Username:      username,
		ContainerType: contracts.ContainerHysteria,
		InboundTag:    defaultInboundTag,
		TargetPort:    uint32(hc.cfg.Port),
		Protocol:      contracts.ProtocolHysteria2,
		Network:       "udp",
	})
	if err != nil {
		slog.Error("hysteria: allocate port failed", "user", username, "err", err)
		return
	}

	// Re-check under mu in case RemoveInboundConfig or SetInboundAccessTags
	// narrowed access while GetBindPort was running unlocked.
	hc.mu.Lock()
	enabledAfter := hc.inboundEnabled
	tagsAfter := cloneStringSlice(hc.accessTags)
	if enabledAfter && stringSlicesEqual(checkedTags, tagsAfter) {
		hc.addedUsers[username] = struct{}{}
		hc.mu.Unlock()
		return
	}
	hc.mu.Unlock()

	if !enabledAfter || !hc.userMgr.CanUserAccessInbound(user, tagsAfter) {
		if err := hc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
			Username: username,
			BindPort: port,
		}); err != nil {
			slog.Error("hysteria: release port after concurrent access change failed", "user", username, "err", err)
		}
		return
	}

	hc.mu.Lock()
	if hc.inboundEnabled {
		hc.addedUsers[username] = struct{}{}
	}
	hc.mu.Unlock()
}

func (hc *HysteriaContainer) revokeUserForwardRule(username, reason string) {
	hc.mu.Lock()
	delete(hc.addedUsers, username)
	hc.mu.Unlock()
	port, ok := hc.userMgr.GetUserPortByDstForCleanup(username, uint32(hc.cfg.Port))
	if !ok {
		return
	}
	if err := hc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
		Username: username,
		BindPort: port,
	}); err != nil {
		slog.Error("hysteria: release port failed", "user", username, "reason", reason, "err", err)
	}
}

// saveInboundConfig persists the placeholder inbound config to store.
func (hc *HysteriaContainer) saveInboundConfig() error {
	if hc.storeMgr == nil {
		return nil
	}

	hc.mu.Lock()
	enabled := hc.inboundEnabled
	accessTags := cloneStringSlice(hc.accessTags)
	remark := hc.remark
	hc.mu.Unlock()

	data := map[string]any{
		"tag":      defaultInboundTag,
		"protocol": string(contracts.ProtocolHysteria2),
		"port":     hc.cfg.Port,
		"listen":   hc.cfg.Listen,
		"domain":   hc.cfg.Domain,
		"enabled":  enabled,
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
		InboundType:   string(contracts.InboundTypeDirect),
		AccessTags:    accessTags,
		Remark:        remark,
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

		accessTags, tagErr := contracts.NormalizeAccessTags(rec.AccessTags)
		if tagErr != nil {
			slog.Warn("hysteria: normalize restored access_tags failed", "err", tagErr)
			accessTags = nil
		}

		hc.mu.Lock()
		// Restore enabled state; true is the default when the key is absent (old records).
		if enabled, ok := data["enabled"].(bool); ok {
			hc.inboundEnabled = enabled
		}
		hc.accessTags = accessTags
		hc.remark = rec.Remark
		hc.mu.Unlock()

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
// Returns nil when the inbound is disabled or the user is not authorized.
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
	accessTags := cloneStringSlice(hc.accessTags)
	remark := hc.remark
	hc.mu.Unlock()
	if !enabled {
		return nil, nil
	}
	if !hc.userMgr.CanUserAccessInboundByUsername(req.User.Username, accessTags) {
		return nil, nil
	}
	port, ok := hc.userMgr.GetUserPortByDst(req.User.Username, uint32(hc.cfg.Port))
	if !ok {
		return nil, nil
	}

	nodeName := req.NodeName
	if remark != "" {
		if nodeName != "" {
			nodeName = nodeName + "-" + remark
		} else {
			nodeName = remark
		}
	} else if nodeName == "" {
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
	hc.persistMu.Lock()
	if err := hc.restoreInboundConfig(); err != nil {
		slog.Warn("hysteria: restore inbound config failed", "err", err)
	}
	hc.persistMu.Unlock()
	hc.reconcileUsers()
	return nil
}

// reconcileUsers syncs all users from UserManager to ensure forward rules exist
// only for users passing two-layer visibility (CanUserAccessInbound).
// GetBindPort is idempotent: if the relay already exists it returns the port;
// if PortMappings has a stale record (relay dead after restart) it recreates the relay.
//
// Concurrency: may run concurrently with handleUserEvent (event handler
// goroutine). All hc.addedUsers reads/writes happen under hc.mu; userMgr
// calls (GetBindPort / ReleaseBindPort / ListUsers /
// GetUserPortByDstForCleanup) are invoked without hc.mu held.
func (hc *HysteriaContainer) reconcileUsers() {
	if hc.userMgr == nil {
		return
	}

	hc.reconcileMu.Lock()
	defer hc.reconcileMu.Unlock()

	users := hc.userMgr.ListUsers()

	// Snapshot tracked usernames, enabled flag, and accessTags under the lock.
	hc.mu.Lock()
	enabled := hc.inboundEnabled
	accessTags := cloneStringSlice(hc.accessTags)
	tracked := make([]string, 0, len(hc.addedUsers))
	for username := range hc.addedUsers {
		tracked = append(tracked, username)
	}
	hc.mu.Unlock()

	// Build a set of permitted usernames using both Layer 1 and Layer 2 checks.
	allowedSet := make(map[string]struct{}, len(users))
	if enabled {
		for _, u := range users {
			if hc.userMgr.CanUserAccessInbound(u, accessTags) {
				allowedSet[u.Username] = struct{}{}
			}
		}
	}

	// Remove users that are tracked but no longer permitted (group/tag change,
	// expiration/deletion, or inbound disabled).
	for _, username := range tracked {
		if _, allowed := allowedSet[username]; allowed {
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

	// Defense-in-depth: revoke any active forward rules in ForwardManager that
	// belong to this inbound for users not in allowedSet (e.g. out-of-band rules).
	if fm := hc.userMgr.GetForwardManager(); fm != nil {
		for _, r := range fm.GetAllRules() {
			if r == nil || r.ContainerType != contracts.ContainerHysteria || r.InboundTag != defaultInboundTag {
				continue
			}
			if _, allowed := allowedSet[r.Username]; allowed {
				continue
			}
			hc.mu.Lock()
			delete(hc.addedUsers, r.Username)
			hc.mu.Unlock()
			if r.ListenPort != 0 {
				if err := hc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
					Username: r.Username,
					BindPort: r.ListenPort,
				}); err != nil {
					slog.Warn("hysteria: reconcile release active rule failed", "user", r.Username, "err", err)
				}
			}
		}
	}

	// Add users that are permitted but not yet tracked (only when inbound is enabled).
	if enabled {
		for _, user := range users {
			if _, allowed := allowedSet[user.Username]; !allowed {
				continue
			}
			hc.mu.Lock()
			_, exists := hc.addedUsers[user.Username]
			hc.mu.Unlock()
			if exists {
				continue
			}
			port, err := hc.userMgr.GetBindPort(usermanager.GetBindPortRequest{
				Username:      user.Username,
				ContainerType: contracts.ContainerHysteria,
				InboundTag:    defaultInboundTag,
				TargetPort:    uint32(hc.cfg.Port),
				Protocol:      contracts.ProtocolHysteria2,
				Network:       "udp",
			})
			if err != nil {
				slog.Warn("hysteria: reconcile forward rule failed", "user", user.Username, "err", err)
				continue
			}
			hc.mu.Lock()
			enabledAfter := hc.inboundEnabled
			tagsAfter := cloneStringSlice(hc.accessTags)
			if enabledAfter && stringSlicesEqual(accessTags, tagsAfter) {
				hc.addedUsers[user.Username] = struct{}{}
				hc.mu.Unlock()
				continue
			}
			hc.mu.Unlock()

			if !enabledAfter || !hc.userMgr.CanUserAccessInbound(user, tagsAfter) {
				if err := hc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
					Username: user.Username,
					BindPort: port,
				}); err != nil {
					slog.Warn("hysteria: reconcile release port after concurrent access change failed", "user", user.Username, "err", err)
				}
				continue
			}
			hc.mu.Lock()
			if hc.inboundEnabled {
				hc.addedUsers[user.Username] = struct{}{}
			}
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

type fastAddMetaParams struct {
	accessTags []string
	remark     string
}

func parseFastAddMetaParams(params map[string]any, validateChain func(string) error) (fastAddMetaParams, error) {
	var out fastAddMetaParams
	if len(params) == 0 {
		return out, nil
	}

	var chainURI string
	if rawURI, ok := params["chain_uri"]; ok && rawURI != nil {
		s, ok := rawURI.(string)
		if !ok {
			return out, fmt.Errorf("hysteria: invalid chain_uri type %T: expected string", rawURI)
		}
		chainURI = strings.TrimSpace(s)
	}

	if rawType, ok := params["inbound_type"]; ok && rawType != nil {
		s, ok := rawType.(string)
		if !ok {
			return out, fmt.Errorf("hysteria: invalid inbound_type type %T: expected string", rawType)
		}
		inbType, err := contracts.NormalizeInboundType(s)
		if err != nil {
			return out, err
		}
		if inbType == contracts.InboundTypeChain {
			return out, validateChain(chainURI)
		}
	}

	if chainURI != "" {
		return out, validateChain(chainURI)
	}

	if rawTags, ok := params["access_tags"]; ok && rawTags != nil {
		tags, err := parseStringSliceParam(rawTags)
		if err != nil {
			return out, fmt.Errorf("hysteria: invalid access_tags: %w", err)
		}
		normalized, err := contracts.NormalizeAccessTags(tags)
		if err != nil {
			return out, err
		}
		out.accessTags = normalized
	}

	if rawRemark, ok := params["remark"]; ok && rawRemark != nil {
		s, ok := rawRemark.(string)
		if !ok {
			return out, fmt.Errorf("hysteria: invalid remark type %T: expected string", rawRemark)
		}
		out.remark = strings.TrimSpace(s)
	}

	return out, nil
}

func parseStringSliceParam(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("element %d has type %T, expected string", i, item)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unexpected type %T, expected []string", raw)
	}
}

func cloneStringSlice(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
