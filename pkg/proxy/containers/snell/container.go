package snell

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
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
	userMgr     *usermanager.UserManager
	userEventCh chan usermanager.UserEvent

	// storeMgr provides unified access to persistence stores.
	storeMgr *store.StoreManager

	// addedUsers tracks users that have been successfully wired up.
	addedUsers map[string]struct{}

	// Reconcile loop for periodic user sync
	reconcileStopCh chan struct{}
	reconcileWg     sync.WaitGroup
}

// snellHooks implements container.Hooks for SnellContainer.
type snellHooks struct {
	c *SnellContainer
}

func (h *snellHooks) GetRunFunc() (func() error, func() error) {
	run := func() error {
		// Restore persisted inbound config before starting
		if err := h.c.restoreInboundConfig(); err != nil {
			slog.Warn("snell: restore inbound config failed", "err", err)
		}
		if err := h.c.saveInboundConfig(); err != nil {
			slog.Warn("snell: save inbound config failed", "err", err)
		}

		// Start user event handler and reconcile loop
		h.c.startUserEventHandler()
		h.c.startReconcileLoop()

		// Start snell-server process
		return h.c.startProcess()
	}
	stop := func() error {
		// Stop reconcile loop first
		h.c.stopReconcileLoop()
		// Close user event channel to terminate forwardUserEvents goroutine
		h.c.closeUserEventCh()
		// Stop the snell-server process
		return h.c.stopProcess()
	}
	return run, stop
}

// NewSnellContainer creates a new SnellContainer from the given config.
func NewSnellContainer(cfg SnellConfig, opts ...SnellOption) (*SnellContainer, error) {
	sc := &SnellContainer{
		cfg:        cfg,
		psk:        cfg.PSK,
		addedUsers: make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(sc)
	}
	sc.BaseContainer = container.NewBaseContainer(contracts.ContainerSnell, &snellHooks{c: sc})

	sc.inbound = inbound.NewDefaultInbound(
		defaultInboundTag,
		contracts.ProtocolSnell,
		uint32(cfg.Port),
	)
	sc.inbound.(*inbound.DefaultInbound).SetListenAddr(cfg.Listen)

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
	sc.inbound.(*inbound.DefaultInbound).SetListenAddr(cfg.Listen)

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
	content := fmt.Sprintf("[snell-server]\nlisten = %s:%d\npsk = %s\nipv6 = false\n",
		sc.cfg.Listen, sc.cfg.Port, sc.psk)

	if err := os.MkdirAll(filepath.Dir(sc.cfg.ConfigFilePath), 0755); err != nil {
		return fmt.Errorf("snell: create config dir: %w", err)
	}
	if err := os.WriteFile(sc.cfg.ConfigFilePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("snell: write config: %w", err)
	}
	return nil
}

// RemoveInboundConfig returns an error — snell has a single fixed inbound.
func (sc *SnellContainer) RemoveInboundConfig(tag string) error {
	if tag == defaultInboundTag {
		return fmt.Errorf("snell: cannot remove default snell inbound")
	}
	return fmt.Errorf("snell: inbound %q not found", tag)
}

// GetInboundConfig returns the default inbound if the tag matches.
func (sc *SnellContainer) GetInboundConfig(tag string) (inbound.Inbound, error) {
	if tag == defaultInboundTag {
		return sc.inbound, nil
	}
	return nil, fmt.Errorf("snell: inbound %q not found", tag)
}

// ListInboundConfigs returns the single default inbound.
func (sc *SnellContainer) ListInboundConfigs() []inbound.Inbound {
	return []inbound.Inbound{sc.inbound}
}

// FastAddInbound is not supported for snell — snell uses a single fixed inbound.
// Users get their own forward port automatically via AddUser.
func (sc *SnellContainer) FastAddInbound(_ string, _ map[string]any) error {
	return fmt.Errorf("snell: FastAddInbound not supported; snell uses a single fixed inbound, add users via AddUser instead")
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

// startUserEventHandler starts a goroutine to process user events from the channel.
func (sc *SnellContainer) startUserEventHandler() {
	if sc.userEventCh == nil {
		return
	}
	go func() {
		for event := range sc.userEventCh {
			sc.handleUserEvent(event)
		}
	}()
}

// handleUserEvent processes a single user event.
func (sc *SnellContainer) handleUserEvent(event usermanager.UserEvent) {
	switch event.Type {
	case usermanager.UserEventAdd:
		if _, exists := sc.addedUsers[event.Username]; exists {
			return
		}
		_, err := sc.userMgr.GetBindPort(usermanager.GetBindPortRequest{
			Username:      event.Username,
			ContainerType: contracts.ContainerSnell,
			InboundTag:    defaultInboundTag,
			TargetPort:    uint32(sc.cfg.Port),
			Protocol:      contracts.ProtocolSnell,
		})
		if err != nil {
			slog.Error("snell: allocate port failed", "user", event.Username, "err", err)
			return
		}
		sc.addedUsers[event.Username] = struct{}{}

	case usermanager.UserEventRemove:
		delete(sc.addedUsers, event.Username)
		port, ok := sc.userMgr.GetUserPortByDstForCleanup(event.Username, uint32(sc.cfg.Port))
		if !ok {
			return
		}
		if err := sc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
			Username: event.Username,
			BindPort: port,
		}); err != nil {
			slog.Error("snell: release port failed", "user", event.Username, "err", err)
		}

	case usermanager.UserEventUpdate:
		// Re-evaluate visibility after group or other changes.
		if event.User != nil && !sc.userMgr.IsUserVisible(event.User) {
			// User no longer belongs to this node — tear down forwarding rule.
			delete(sc.addedUsers, event.Username)
			port, ok := sc.userMgr.GetUserPortByDstForCleanup(event.Username, uint32(sc.cfg.Port))
			if ok {
				if err := sc.userMgr.ReleaseBindPort(usermanager.ReleaseBindPortRequest{
					Username: event.Username,
					BindPort: port,
				}); err != nil {
					slog.Error("snell: release port on group change failed", "user", event.Username, "err", err)
				}
			}
		} else if event.User != nil {
			// User became visible (e.g. group changed back) — ensure forwarding rule exists.
			if _, exists := sc.addedUsers[event.Username]; !exists {
				_, err := sc.userMgr.GetBindPort(usermanager.GetBindPortRequest{
					Username:      event.Username,
					ContainerType: contracts.ContainerSnell,
					InboundTag:    defaultInboundTag,
					TargetPort:    uint32(sc.cfg.Port),
					Protocol:      contracts.ProtocolSnell,
				})
				if err != nil {
					slog.Error("snell: allocate port on group change failed", "user", event.Username, "err", err)
					return
				}
				sc.addedUsers[event.Username] = struct{}{}
			}
		}
	}
}

// saveInboundConfig persists the placeholder inbound config to store.
func (sc *SnellContainer) saveInboundConfig() error {
	if sc.storeMgr == nil {
		return nil
	}

	data := map[string]any{
		"tag":      defaultInboundTag,
		"protocol": string(contracts.ProtocolSnell),
		"port":     sc.cfg.Port,
		"listen":   sc.cfg.Listen,
		"psk":      sc.psk,
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
	}
	return sc.storeMgr.InboundStore().Save(rec)
}

// restoreInboundConfig restores the inbound config from store.
func (sc *SnellContainer) restoreInboundConfig() error {
	if sc.storeMgr == nil {
		return nil
	}

	records, err := sc.storeMgr.InboundStore().Load()
	if err != nil {
		return fmt.Errorf("snell: load inbound records: %w", err)
	}

	for _, rec := range records {
		if rec.Tag != defaultInboundTag {
			continue
		}
		if rec.ContainerType != string(contracts.ContainerSnell) {
			continue
		}

		var data map[string]any
		if err := json.Unmarshal(rec.NativeJSON, &data); err != nil {
			slog.Warn("snell: unmarshal stored inbound config failed", "err", err)
			return nil
		}

		// Restore port, listen, and psk from stored config
		if port, ok := data["port"].(float64); ok {
			sc.cfg.Port = int(port)
		}
		if listen, ok := data["listen"].(string); ok {
			sc.cfg.Listen = listen
		}
		if psk, ok := data["psk"].(string); ok && psk != "" {
			sc.cfg.PSK = psk
			sc.psk = psk
		}

		// Re-create inbound with restored config
		sc.inbound = inbound.NewDefaultInbound(
			defaultInboundTag,
			contracts.ProtocolSnell,
			uint32(sc.cfg.Port),
		)
		sc.inbound.(*inbound.DefaultInbound).SetListenAddr(sc.cfg.Listen)
		return nil
	}

	return nil
}

// GetUserSubscriptions returns a subscription spec with the snell PSK.
func (sc *SnellContainer) GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	port, ok := sc.userMgr.GetUserPortByDst(req.User.Username, uint32(sc.cfg.Port))
	if !ok {
		return nil, nil
	}

	nodeName := req.NodeName
	if nodeName == "" {
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
	sc.reconcileUsers()
	return nil
}

// reconcileUsers syncs all users from UserManager to ensure forward rules exist.
// This is called by Restore and periodically by the reconcile loop.
// GetBindPort is idempotent: if the relay already exists it returns the port;
// if PortMappings has a stale record (relay dead after restart) it recreates the relay.
func (sc *SnellContainer) reconcileUsers() {
	if sc.userMgr == nil {
		return
	}

	users := sc.userMgr.ListUsers()

	// Build a set of visible usernames for fast lookup.
	visibleSet := make(map[string]struct{}, len(users))
	for _, u := range users {
		visibleSet[u.Username] = struct{}{}
	}

	// Remove users that are tracked but no longer visible (e.g. group changed).
	for username := range sc.addedUsers {
		if _, visible := visibleSet[username]; !visible {
			delete(sc.addedUsers, username)
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
	}

	// Add users that are visible but not yet tracked.
	for _, user := range users {
		if _, exists := sc.addedUsers[user.Username]; exists {
			continue
		}
		if _, err := sc.userMgr.GetBindPort(usermanager.GetBindPortRequest{
			Username:      user.Username,
			ContainerType: contracts.ContainerSnell,
			InboundTag:    defaultInboundTag,
			TargetPort:    uint32(sc.cfg.Port),
			Protocol:      contracts.ProtocolSnell,
		}); err != nil {
			slog.Warn("snell: reconcile forward rule failed", "user", user.Username, "err", err)
			continue
		}
		sc.addedUsers[user.Username] = struct{}{}
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

// closeUserEventCh closes the user event channel to terminate the forwardUserEvents goroutine.
func (sc *SnellContainer) closeUserEventCh() {
	if sc.userEventCh != nil {
		close(sc.userEventCh)
		sc.userEventCh = nil
	}
}
