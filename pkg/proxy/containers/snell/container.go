package snell

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
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

const defaultInboundTag = "snell-default"

// SnellContainer implements the Container interface for snell-server.
type SnellContainer struct {
	*container.BaseContainer
	cfg     SnellConfig
	psk     string
	inbound inbound.Inbound
	runner  *process.Runner

	// UserManager integration
	userMgr *usermanager.UserManager
	// userEventCh is created once in NewSnellContainer and never closed or
	// reassigned afterwards (immutable after construction), so forwardUserEvents
	// can read the field lock-free and sends can never panic on a closed
	// channel. Draining is done by the per-generation user event handler
	// goroutine; while the container is stopped, events accumulate in the
	// buffer (oldest beyond capacity dropped by forwardUserEvents' select
	// default) and are consumed again after the next Start.
	userEventCh chan usermanager.UserEvent

	// storeMgr provides unified access to persistence stores.
	storeMgr *store.StoreManager

	// mu guards addedUsers, inboundEnabled, accessTags, and remark against
	// concurrent access between the user-event handler goroutine, reconcile loop,
	// and runtime metadata/tag updates. Held only around map/field reads/writes —
	// never while calling userMgr or store methods.
	mu sync.Mutex
	// addedUsers tracks users that have been successfully wired up.
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
	reconcileMu sync.Mutex

	// Reconcile loop for periodic user sync
	reconcileStopCh chan struct{}
	reconcileWg     sync.WaitGroup
	// lifecycleMu guards swaps of the session field (run/stop hooks only).
	// It is never held while cancelling or waiting on a session.
	lifecycleMu sync.Mutex
	// session is the current Start/Stop generation; nil while stopped.
	session *lifecycleSession
	// procMu serializes startProcess/stopProcess so an interleaved Start/Stop
	// can never leak an untracked process (defense-in-depth; BaseContainer's
	// opMu already serializes the hooks that call them).
	procMu sync.Mutex
}

var (
	_ container.Container           = (*SnellContainer)(nil)
	_ container.ChainCapable        = (*SnellContainer)(nil)
	_ container.InboundTagUpdater   = (*SnellContainer)(nil)
	_ container.InboundMetaProvider = (*SnellContainer)(nil)
)

// lifecycleSession bundles the stop signal and goroutine tracking for one
// Start/Stop generation, mirroring the hysteria container. The run hook installs
// a fresh session; the stop hook cancels it and waits for its goroutines (the
// user event handler) to exit before stopping the process. Goroutines capture
// ctx by value and never read sc.session, so generation swaps cannot race.
type lifecycleSession struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// snellHooks implements container.Hooks for SnellContainer.
type snellHooks struct {
	c *SnellContainer
}

func (h *snellHooks) GetRunFunc() (func() error, func() error) {
	run := func() error {
		// Restore persisted inbound config before starting
		h.c.persistMu.Lock()
		if err := h.c.restoreInboundConfig(); err != nil {
			slog.Error("snell: restore inbound config failed; keeping inbound disabled (fail-closed)", "err", err)
		} else if err := h.c.saveInboundConfig(); err != nil {
			slog.Warn("snell: save inbound config failed", "err", err)
		}
		h.c.persistMu.Unlock()

		// Install a fresh lifecycle session for this generation, cancelling any
		// stale one (defensive: BaseContainer's opMu makes this a no-op in
		// practice, since the previous Stop has already nil'd session).
		s := &lifecycleSession{}
		s.ctx, s.cancel = context.WithCancel(context.Background())
		h.c.lifecycleMu.Lock()
		if old := h.c.session; old != nil {
			old.cancel()
		}
		h.c.session = s
		h.c.lifecycleMu.Unlock()

		// Start user event handler and reconcile loop
		h.c.startUserEventHandler(s)
		h.c.startReconcileLoop()

		// Start the snell-server process synchronously. Unlike hysteria (whose
		// run returns nil and starts the process in a background goroutine),
		// snell starts here, so on failure we must tear this session and the
		// reconcile loop down before returning the error — otherwise their
		// goroutines would leak.
		if err := h.c.startProcess(); err != nil {
			h.c.stopReconcileLoop()
			h.c.lifecycleMu.Lock()
			if h.c.session == s {
				h.c.session = nil
			}
			h.c.lifecycleMu.Unlock()
			s.cancel()
			s.wg.Wait()
			return err
		}
		return nil
	}
	stop := func() error {
		// Stop reconcile loop first
		h.c.stopReconcileLoop()
		// Cancel this generation and wait for its goroutines (the user event
		// handler) to exit before stopping the process. userEventCh is never
		// closed, so forwardUserEvents can never panic on a closed channel.
		h.c.lifecycleMu.Lock()
		s := h.c.session
		h.c.session = nil
		h.c.lifecycleMu.Unlock()
		if s != nil {
			s.cancel()
			s.wg.Wait()
		}
		// Stop the snell-server process
		return h.c.stopProcess()
	}
	return run, stop
}

// NewSnellContainer creates a new SnellContainer from the given config.
func NewSnellContainer(cfg SnellConfig, opts ...SnellOption) (*SnellContainer, error) {
	sc := &SnellContainer{
		cfg:            cfg,
		psk:            cfg.PSK,
		addedUsers:     make(map[string]struct{}),
		inboundEnabled: true,
	}
	for _, opt := range opts {
		opt(sc)
	}
	sc.BaseContainer = container.NewBaseContainer(contracts.ContainerSnell, &snellHooks{c: sc})

	// The port is fixed by container config (never drawn), so this container
	// does not claim it itself. It is claimed by the startup pre-claim pass in
	// cmd/server.go, together with every persisted inbound port, BEFORE any
	// container is constructed or started — that ordering is what stops an
	// earlier container's user forward ports from taking it. Snell's default
	// (16160) sits inside the forward pool, unlike hysteria's deliberately
	// sub-pool 9443, so this is not hypothetical.
	sc.inbound = inbound.NewDefaultInbound(
		defaultInboundTag,
		contracts.ProtocolSnell,
		uint32(cfg.Port),
	)
	// snell binds loopback only; forward layer handles external access.
	sc.inbound.(*inbound.DefaultInbound).SetListenAddr("127.0.0.1")

	// Subscribe to user events via pub/sub channel
	if sc.userMgr != nil {
		sc.userEventCh = make(chan usermanager.UserEvent, 100)
		go sc.forwardUserEvents(sc.userMgr.Subscribe())
	}

	return sc, nil
}

// SnellOption configures optional dependencies for SnellContainer.
type SnellOption func(*SnellContainer)

// WithStoreMgr sets the store manager for inbound persistence.
func WithStoreMgr(sm *store.StoreManager) SnellOption {
	return func(sc *SnellContainer) {
		sc.storeMgr = sm
	}
}

// WithUserManager sets the user manager for user event handling.
func WithUserManager(um *usermanager.UserManager) SnellOption {
	return func(sc *SnellContainer) {
		sc.userMgr = um
	}
}

// Init initializes the container with the given configuration.
// This is called when re-configuring a container after creation.
// Note: startup logic (event handler, reconcile loop, inbound restore) is in
// the hooks run function, which is invoked by Start().
func (sc *SnellContainer) Init(config any) error {
	cfg, ok := config.(*SnellConfig)
	if !ok || cfg == nil {
		return fmt.Errorf("snell: invalid config type, expected *SnellConfig")
	}
	sc.cfg = *cfg
	sc.psk = cfg.PSK

	sc.inbound = inbound.NewDefaultInbound(
		defaultInboundTag,
		contracts.ProtocolSnell,
		uint32(cfg.Port),
	)
	sc.inbound.(*inbound.DefaultInbound).SetListenAddr("127.0.0.1")

	return nil
}

// Reload is a no-op for snell.
func (sc *SnellContainer) Reload() error {
	return nil
}

// Version returns the configured version string.
func (sc *SnellContainer) Version() string {
	return sc.cfg.Version
}

// ConfigFile returns the config file path.
func (sc *SnellContainer) ConfigFile() string {
	return sc.cfg.ConfigFilePath
}

// Update downloads a new version, restarts the process, and returns the result.
// Supports atomic swap with rollback on failure.
func (sc *SnellContainer) Update(_ context.Context, req container.UpdateRequest) (*container.UpdateResult, error) {
	targetVersion := req.TargetTag
	if targetVersion == "" {
		targetVersion = sc.cfg.Version
	}

	fromVersion := sc.cfg.Version

	if req.DryRun {
		return &container.UpdateResult{
			FromVersion: fromVersion,
			ToVersion:   targetVersion,
		}, nil
	}

	// Download new version to a temp path
	tmpBinary := sc.cfg.BinaryPath + ".new"
	if err := downloadSnellServer(targetVersion, tmpBinary); err != nil {
		return nil, fmt.Errorf("snell: download update: %w", err)
	}

	// Stop current process
	if err := sc.Stop(); err != nil {
		os.Remove(tmpBinary)
		return nil, fmt.Errorf("snell: stop for update: %w", err)
	}

	// Backup current binary for rollback
	backupPath := sc.cfg.BinaryPath + ".bak"
	hasBackup := false
	if _, err := os.Stat(sc.cfg.BinaryPath); err == nil {
		if err := os.Rename(sc.cfg.BinaryPath, backupPath); err != nil {
			os.Remove(tmpBinary)
			// Try to restart with old binary (it's still in place since rename failed)
			_ = sc.Start()
			return nil, fmt.Errorf("snell: backup binary for rollback: %w", err)
		}
		hasBackup = true
	}

	// Atomic swap: move new binary into place
	if err := os.Rename(tmpBinary, sc.cfg.BinaryPath); err != nil {
		os.Remove(tmpBinary)
		// Rollback: restore backup
		if hasBackup {
			_ = os.Rename(backupPath, sc.cfg.BinaryPath)
		}
		_ = sc.Start()
		return nil, fmt.Errorf("snell: replace binary: %w", err)
	}

	sc.cfg.Version = targetVersion

	// Start new process
	if err := sc.Start(); err != nil {
		// Rollback: restore old binary and restart
		if hasBackup {
			_ = os.Rename(backupPath, sc.cfg.BinaryPath)
			_ = sc.Start()
		}
		return nil, fmt.Errorf("snell: start after update: %w", err)
	}

	// Success — clean up backup
	if hasBackup {
		os.Remove(backupPath)
	}

	return &container.UpdateResult{
		FromVersion: fromVersion,
		ToVersion:   targetVersion,
		Restarted:   true,
	}, nil
}

// generateConfigFile writes the snell-server INI config to cfg.ConfigFilePath.
func (sc *SnellContainer) generateConfigFile() error {
	content := fmt.Sprintf("[snell-server]\nlisten = 127.0.0.1:%d\npsk = %s\nipv6 = false\n",
		sc.cfg.Port, sc.psk)

	if err := os.MkdirAll(filepath.Dir(sc.cfg.ConfigFilePath), 0755); err != nil {
		return fmt.Errorf("snell: create config dir: %w", err)
	}
	if err := os.WriteFile(sc.cfg.ConfigFilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("snell: write config: %w", err)
	}
	return nil
}

// SupportsChainOutbound reports whether SnellContainer supports per-inbound chained outbounds.
// Mode A single-process containers do not support chained outbounds.
func (sc *SnellContainer) SupportsChainOutbound() bool {
	return false
}

// ValidateChainURI rejects chained outbound URIs because Mode A snell does
// not support per-inbound chained outbounds.
func (sc *SnellContainer) ValidateChainURI(rawURI string) error {
	return fmt.Errorf("snell: Mode A container does not support chained outbounds (official snell-server has no upstream proxy outbound)")
}

// RemoveInboundConfig disables the default inbound: tears down all per-user
// forward rules and persists the disabled state. The snell process keeps running.
func (sc *SnellContainer) RemoveInboundConfig(tag string) error {
	if tag != defaultInboundTag {
		return fmt.Errorf("snell: inbound %q not found", tag)
	}
	sc.persistMu.Lock()
	defer sc.persistMu.Unlock()

	sc.mu.Lock()
	sc.inboundEnabled = false
	sc.mu.Unlock()
	sc.releaseAllForwardRules()
	if err := sc.saveInboundConfig(); err != nil {
		slog.Warn("snell: save inbound config on disable failed", "err", err)
	}
	return nil
}

// releaseAllForwardRules tears down all per-user forward rules and clears addedUsers.
func (sc *SnellContainer) releaseAllForwardRules() {
	if sc.userMgr == nil {
		return
	}
	sc.mu.Lock()
	users := make([]string, 0, len(sc.addedUsers))
	for u := range sc.addedUsers {
		users = append(users, u)
	}
	sc.mu.Unlock()

	for _, username := range users {
		port, ok := sc.userMgr.GetUserPortByDstForCleanup(username, uint32(sc.cfg.Port))
		if !ok {
			continue
		}
		if err := sc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
			Username: username,
			BindPort: port,
		}); err != nil {
			slog.Error("snell: release port on disable failed", "user", username, "err", err)
		}
	}
	sc.mu.Lock()
	sc.addedUsers = make(map[string]struct{})
	sc.mu.Unlock()
}

// GetInboundConfig returns the default inbound if the tag matches and the inbound is enabled.
func (sc *SnellContainer) GetInboundConfig(tag string) (inbound.Inbound, error) {
	if tag != defaultInboundTag {
		return nil, fmt.Errorf("snell: inbound %q not found", tag)
	}
	sc.mu.Lock()
	enabled := sc.inboundEnabled
	sc.mu.Unlock()
	if !enabled {
		return nil, fmt.Errorf("snell: inbound %q is disabled", tag)
	}
	return sc.inbound, nil
}

// ListInboundConfigs returns the single default inbound, or empty if disabled.
func (sc *SnellContainer) ListInboundConfigs() []inbound.Inbound {
	sc.mu.Lock()
	enabled := sc.inboundEnabled
	sc.mu.Unlock()
	if !enabled {
		return nil
	}
	return []inbound.Inbound{sc.inbound}
}

// GetInboundMeta returns chain and visibility metadata for the default inbound
// when tag matches and the inbound is enabled.
func (sc *SnellContainer) GetInboundMeta(tag string) (contracts.InboundMeta, bool) {
	if tag != defaultInboundTag {
		return contracts.InboundMeta{}, false
	}
	sc.mu.Lock()
	enabled := sc.inboundEnabled
	tags := cloneStringSlice(sc.accessTags)
	remark := sc.remark
	sc.mu.Unlock()
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
// without restarting the snell process, persists the change (rolling back on
// store error), and immediately reconciles user forward rules.
func (sc *SnellContainer) SetInboundAccessTags(tag string, accessTags []string) error {
	if tag != defaultInboundTag {
		return fmt.Errorf("snell: inbound %q not found", tag)
	}
	normalized, err := contracts.NormalizeAccessTags(accessTags)
	if err != nil {
		return err
	}

	sc.persistMu.Lock()
	sc.mu.Lock()
	if !sc.inboundEnabled {
		sc.mu.Unlock()
		sc.persistMu.Unlock()
		return fmt.Errorf("snell: inbound %q is disabled", tag)
	}
	prevTags := cloneStringSlice(sc.accessTags)
	sc.accessTags = cloneStringSlice(normalized)
	sc.mu.Unlock()

	if err := sc.saveInboundConfig(); err != nil {
		sc.mu.Lock()
		sc.accessTags = prevTags
		sc.mu.Unlock()
		sc.persistMu.Unlock()
		return fmt.Errorf("snell: persist access tags for %q: %w", tag, err)
	}
	sc.persistMu.Unlock()

	sc.reconcileUsers()
	return nil
}

// FastAddInbound enables the default inbound: validates that chain mode is not
// requested, normalizes optional access_tags and remark, persists the state
// (rolling back on store error), and reconciles forward rules for all current users.
func (sc *SnellContainer) FastAddInbound(tag string, params map[string]any) error {
	meta, err := parseFastAddMetaParams(params, sc.ValidateChainURI)
	if err != nil {
		return err
	}
	if tag != defaultInboundTag {
		return fmt.Errorf("snell: only the default inbound %q is supported", defaultInboundTag)
	}

	sc.persistMu.Lock()
	sc.mu.Lock()
	prevEnabled := sc.inboundEnabled
	prevTags := cloneStringSlice(sc.accessTags)
	prevRemark := sc.remark
	sc.inboundEnabled = true
	sc.accessTags = cloneStringSlice(meta.accessTags)
	sc.remark = meta.remark
	sc.mu.Unlock()

	if err := sc.saveInboundConfig(); err != nil {
		sc.mu.Lock()
		sc.inboundEnabled = prevEnabled
		sc.accessTags = prevTags
		sc.remark = prevRemark
		sc.mu.Unlock()
		sc.persistMu.Unlock()
		return fmt.Errorf("snell: save inbound config on enable: %w", err)
	}
	sc.persistMu.Unlock()

	sc.reconcileUsers()
	return nil
}

// UserEventChannel returns the container's channel for receiving user events.
func (sc *SnellContainer) UserEventChannel() <-chan usermanager.UserEvent {
	return sc.userEventCh
}

// forwardUserEvents forwards events from UserManager to local channel.
func (sc *SnellContainer) forwardUserEvents(source <-chan usermanager.UserEvent) {
	for event := range source {
		if sc.userEventCh != nil {
			select {
			case sc.userEventCh <- event:
			default:
			}
		}
	}
}

// startUserEventHandler starts a goroutine to process user events from the
// channel. The goroutine belongs to the given lifecycle session: it exits on
// session cancel and is restarted by the next generation's run hook, so user
// event handling survives Stop→Start cycles (e.g. the Update() restart path).
func (sc *SnellContainer) startUserEventHandler(s *lifecycleSession) {
	if sc.userEventCh == nil {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.ctx.Done():
				return
			case event := <-sc.userEventCh:
				sc.handleUserEvent(event)
			}
		}
	}()
}

// handleUserEvent processes a single user event.
//
// Concurrency: addedUsers is guarded by sc.mu. The lock is held only around
// map/field reads/writes so that userMgr.GetBindPort / ReleaseBindPort (which
// take their own locks and may be slow) run without it. GetBindPort is
// idempotent, so a TOCTOU race where two callers both see !exists and both
// invoke GetBindPort is still correct.
func (sc *SnellContainer) handleUserEvent(event usermanager.UserEvent) {
	if sc.userMgr == nil {
		return
	}
	switch event.Type {
	case usermanager.UserEventAdd, usermanager.UserEventUpdate:
		sc.mu.Lock()
		enabled := sc.inboundEnabled
		accessTags := cloneStringSlice(sc.accessTags)
		sc.mu.Unlock()

		if !enabled || !sc.userMgr.CanUserAccessInbound(event.User, accessTags) {
			sc.revokeUserForwardRule(event.Username, "access denied")
			return
		}
		sc.ensureUserForwardRule(event.Username, event.User, accessTags)

	case usermanager.UserEventRemove, usermanager.UserEventExpire:
		sc.revokeUserForwardRule(event.Username, "remove")
	}
}

func (sc *SnellContainer) ensureUserForwardRule(username string, user *contracts.User, checkedTags []string) {
	sc.mu.Lock()
	enabled := sc.inboundEnabled
	_, exists := sc.addedUsers[username]
	sc.mu.Unlock()
	if !enabled || exists {
		return
	}
	port, err := sc.userMgr.GetBindPort(usermanager.GetBindPortRequest{
		Username:      username,
		ContainerType: contracts.ContainerSnell,
		InboundTag:    defaultInboundTag,
		TargetPort:    uint32(sc.cfg.Port),
		Protocol:      contracts.ProtocolSnell,
	})
	if err != nil {
		slog.Error("snell: allocate port failed", "user", username, "err", err)
		return
	}

	// Re-check under mu in case RemoveInboundConfig or SetInboundAccessTags
	// narrowed access while GetBindPort was running unlocked.
	sc.mu.Lock()
	enabledAfter := sc.inboundEnabled
	tagsAfter := cloneStringSlice(sc.accessTags)
	if enabledAfter && stringSlicesEqual(checkedTags, tagsAfter) {
		sc.addedUsers[username] = struct{}{}
		sc.mu.Unlock()
		return
	}
	sc.mu.Unlock()

	if !enabledAfter || !sc.userMgr.CanUserAccessInbound(user, tagsAfter) {
		if err := sc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
			Username: username,
			BindPort: port,
		}); err != nil {
			slog.Error("snell: release port after concurrent access change failed", "user", username, "err", err)
		}
		return
	}

	sc.mu.Lock()
	if sc.inboundEnabled {
		sc.addedUsers[username] = struct{}{}
	}
	sc.mu.Unlock()
}

func (sc *SnellContainer) revokeUserForwardRule(username, reason string) {
	sc.mu.Lock()
	delete(sc.addedUsers, username)
	sc.mu.Unlock()
	port, ok := sc.userMgr.GetUserPortByDstForCleanup(username, uint32(sc.cfg.Port))
	if !ok {
		return
	}
	if err := sc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
		Username: username,
		BindPort: port,
	}); err != nil {
		slog.Error("snell: release port failed", "user", username, "reason", reason, "err", err)
	}
}

// saveInboundConfig persists the placeholder inbound config to store.
func (sc *SnellContainer) saveInboundConfig() error {
	if sc.storeMgr == nil {
		return nil
	}

	sc.mu.Lock()
	enabled := sc.inboundEnabled
	accessTags := cloneStringSlice(sc.accessTags)
	remark := sc.remark
	sc.mu.Unlock()

	data := map[string]any{
		"tag":      defaultInboundTag,
		"protocol": string(contracts.ProtocolSnell),
		"port":     sc.cfg.Port,
		"listen":   "127.0.0.1",
		"psk":      sc.psk,
		"enabled":  enabled,
	}
	nativeJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("snell: marshal inbound config: %w", err)
	}

	rec := &store.InboundRecord{
		Tag:           defaultInboundTag,
		ContainerType: string(contracts.ContainerSnell),
		CertSource:    "none",
		NativeJSON:    nativeJSON,
		InboundType:   string(contracts.InboundTypeDirect),
		AccessTags:    accessTags,
		Remark:        remark,
	}
	return sc.storeMgr.InboundStore().Save(rec)
}

// restoreInboundConfig restores the inbound config from store.
// Fails closed (disables the inbound and returns an error) whenever the
// persisted record cannot be loaded, decoded, or its access_tags are invalid.
func (sc *SnellContainer) restoreInboundConfig() error {
	if sc.storeMgr == nil {
		return nil
	}

	rec, err := sc.storeMgr.InboundStore().Get(defaultInboundTag)
	if err != nil {
		sc.mu.Lock()
		sc.inboundEnabled = false
		sc.mu.Unlock()
		return fmt.Errorf("snell: load inbound record %q: %w", defaultInboundTag, err)
	}
	if rec == nil || rec.ContainerType != string(contracts.ContainerSnell) {
		return nil
	}

	var data map[string]any
	if err := json.Unmarshal(rec.NativeJSON, &data); err != nil {
		sc.mu.Lock()
		sc.inboundEnabled = false
		sc.mu.Unlock()
		return fmt.Errorf("snell: unmarshal stored inbound config: %w", err)
	}

	accessTags, tagErr := contracts.NormalizeAccessTags(rec.AccessTags)
	if tagErr != nil {
		sc.mu.Lock()
		sc.inboundEnabled = false
		sc.mu.Unlock()
		return fmt.Errorf("snell: normalize restored access_tags: %w", tagErr)
	}

	// Restore port and psk from stored config (listen is always 127.0.0.1).
	if port, ok := data["port"].(float64); ok {
		sc.cfg.Port = int(port)
	}
	if psk, ok := data["psk"].(string); ok && psk != "" {
		sc.cfg.PSK = psk
		sc.psk = psk
	}

	sc.mu.Lock()
	// Restore enabled state; true is the default when the key is absent (old records).
	if enabled, ok := data["enabled"].(bool); ok {
		sc.inboundEnabled = enabled
	}
	sc.accessTags = accessTags
	sc.remark = rec.Remark
	sc.mu.Unlock()

	// Re-create inbound with restored config; listen is always loopback.
	sc.inbound = inbound.NewDefaultInbound(
		defaultInboundTag,
		contracts.ProtocolSnell,
		uint32(sc.cfg.Port),
	)
	sc.inbound.(*inbound.DefaultInbound).SetListenAddr("127.0.0.1")
	return nil
}

// GetUserSubscriptions returns a subscription spec with the snell PSK.
// Returns nil when the inbound is disabled or the user is not authorized.
func (sc *SnellContainer) GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	if sc.userMgr == nil {
		return nil, nil
	}
	sc.mu.Lock()
	enabled := sc.inboundEnabled
	accessTags := cloneStringSlice(sc.accessTags)
	remark := sc.remark
	sc.mu.Unlock()
	if !enabled {
		return nil, nil
	}
	if !sc.userMgr.CanUserAccessInboundByUsername(req.User.Username, accessTags) {
		return nil, nil
	}
	port, ok := sc.userMgr.GetUserPortByDst(req.User.Username, uint32(sc.cfg.Port))
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
		nodeName = "snell"
	}

	spec := contracts.SubscriptionSpec{
		Protocol:   contracts.ProtocolSnell,
		Host:       req.Host,
		Port:       port,
		Password:   sc.psk,
		NodeName:   nodeName,
		InboundTag: defaultInboundTag,
		Username:   req.User.Username,
		URI:        fmt.Sprintf("snell://%s@%s:%d?version=5#%s", sc.psk, req.Host, port, url.QueryEscape(nodeName)),
	}
	return []contracts.SubscriptionSpec{spec}, nil
}

// Restore rebuilds forward rules for all existing users after restart.
func (sc *SnellContainer) Restore(ctx context.Context) error {
	sc.persistMu.Lock()
	err := sc.restoreInboundConfig()
	sc.persistMu.Unlock()
	if err != nil {
		slog.Error("snell: restore inbound config failed; keeping inbound disabled (fail-closed)", "err", err)
	}
	sc.reconcileUsers()
	return err
}

// reconcileUsers syncs all users from UserManager to ensure forward rules exist
// only for users passing two-layer visibility (CanUserAccessInbound).
// GetBindPort is idempotent: if the relay already exists it returns the port;
// if PortMappings has a stale record (relay dead after restart) it recreates the relay.
//
// Concurrency: may run concurrently with handleUserEvent. All sc.addedUsers
// reads/writes happen under sc.mu; userMgr calls run without sc.mu held.
func (sc *SnellContainer) reconcileUsers() {
	if sc.userMgr == nil {
		return
	}

	sc.reconcileMu.Lock()
	defer sc.reconcileMu.Unlock()

	users := sc.userMgr.ListUsers()

	// Snapshot tracked usernames, enabled flag, and accessTags under the lock.
	sc.mu.Lock()
	enabled := sc.inboundEnabled
	accessTags := cloneStringSlice(sc.accessTags)
	tracked := make([]string, 0, len(sc.addedUsers))
	for username := range sc.addedUsers {
		tracked = append(tracked, username)
	}
	sc.mu.Unlock()

	// Build a set of permitted usernames using both Layer 1 and Layer 2 checks.
	allowedSet := make(map[string]struct{}, len(users))
	if enabled {
		for _, u := range users {
			if sc.userMgr.CanUserAccessInbound(u, accessTags) {
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
		sc.mu.Lock()
		delete(sc.addedUsers, username)
		sc.mu.Unlock()
		port, ok := sc.userMgr.GetUserPortByDstForCleanup(username, uint32(sc.cfg.Port))
		if ok {
			if err := sc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
				Username: username,
				BindPort: port,
			}); err != nil {
				slog.Warn("snell: reconcile release port failed", "user", username, "err", err)
			}
		}
	}

	// Defense-in-depth: revoke any active forward rules in ForwardManager that
	// belong to this inbound for users not in allowedSet (e.g. out-of-band rules).
	if fm := sc.userMgr.GetForwardManager(); fm != nil {
		for _, r := range fm.GetAllRules() {
			if r == nil || r.ContainerType != contracts.ContainerSnell || r.InboundTag != defaultInboundTag {
				continue
			}
			if _, allowed := allowedSet[r.Username]; allowed {
				continue
			}
			sc.mu.Lock()
			delete(sc.addedUsers, r.Username)
			sc.mu.Unlock()
			if r.ListenPort != 0 {
				if err := sc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
					Username: r.Username,
					BindPort: r.ListenPort,
				}); err != nil {
					slog.Warn("snell: reconcile release active rule failed", "user", r.Username, "err", err)
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
			sc.mu.Lock()
			_, exists := sc.addedUsers[user.Username]
			sc.mu.Unlock()
			if exists {
				continue
			}
			port, err := sc.userMgr.GetBindPort(usermanager.GetBindPortRequest{
				Username:      user.Username,
				ContainerType: contracts.ContainerSnell,
				InboundTag:    defaultInboundTag,
				TargetPort:    uint32(sc.cfg.Port),
				Protocol:      contracts.ProtocolSnell,
			})
			if err != nil {
				slog.Warn("snell: reconcile forward rule failed", "user", user.Username, "err", err)
				continue
			}
			sc.mu.Lock()
			enabledAfter := sc.inboundEnabled
			tagsAfter := cloneStringSlice(sc.accessTags)
			if enabledAfter && stringSlicesEqual(accessTags, tagsAfter) {
				sc.addedUsers[user.Username] = struct{}{}
				sc.mu.Unlock()
				continue
			}
			sc.mu.Unlock()

			if !enabledAfter || !sc.userMgr.CanUserAccessInbound(user, tagsAfter) {
				if err := sc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
					Username: user.Username,
					BindPort: port,
				}); err != nil {
					slog.Warn("snell: reconcile release port after concurrent access change failed", "user", user.Username, "err", err)
				}
				continue
			}
			sc.mu.Lock()
			if sc.inboundEnabled {
				sc.addedUsers[user.Username] = struct{}{}
			}
			sc.mu.Unlock()
		}
	}
}

// startReconcileLoop starts the periodic user reconciliation goroutine.
func (sc *SnellContainer) startReconcileLoop() {
	if sc.userMgr == nil {
		return
	}

	sc.reconcileStopCh = make(chan struct{})
	sc.reconcileWg.Add(1)

	go func() {
		defer sc.reconcileWg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				sc.reconcileUsers()
			case <-sc.reconcileStopCh:
				return
			}
		}
	}()
}

// stopReconcileLoop stops the periodic user reconciliation goroutine gracefully.
func (sc *SnellContainer) stopReconcileLoop() {
	if sc.reconcileStopCh != nil {
		close(sc.reconcileStopCh)
		sc.reconcileWg.Wait()
		sc.reconcileStopCh = nil
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
			return out, fmt.Errorf("snell: invalid chain_uri type %T: expected string", rawURI)
		}
		chainURI = strings.TrimSpace(s)
	}

	if rawType, ok := params["inbound_type"]; ok && rawType != nil {
		s, ok := rawType.(string)
		if !ok {
			return out, fmt.Errorf("snell: invalid inbound_type type %T: expected string", rawType)
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
			return out, fmt.Errorf("snell: invalid access_tags: %w", err)
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
			return out, fmt.Errorf("snell: invalid remark type %T: expected string", rawRemark)
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
