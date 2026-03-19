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
	cfg              HysteriaConfig
	httpPort         int          // v2raymg HTTP server port, used for auth callback
	inbound          inbound.Inbound
	runner           *process.Runner
	userMgr          *usermanager.UserManager
	userEventCh      chan usermanager.UserEvent
	storeMgr         *store.StoreManager
	certReader       CertReader
	certMgr          certIssuer        // triggers cert issuance if not found
	reconcileStopCh  chan struct{}
	reconcileWg      sync.WaitGroup
	certWaitStopCh   chan struct{}
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

		// Start user event handler
		h.c.startUserEventHandler()

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
		cfg: cfg,
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
	hc.inbound.(*inbound.DefaultInbound).SetListenAddr(cfg.Listen)

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
	hc.inbound.(*inbound.DefaultInbound).SetListenAddr(cfg.Listen)

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

// RemoveInboundConfig returns an error — hysteria has a single fixed inbound.
func (hc *HysteriaContainer) RemoveInboundConfig(tag string) error {
	if tag == defaultInboundTag {
		return fmt.Errorf("hysteria: cannot remove default hysteria inbound")
	}
	return fmt.Errorf("hysteria: inbound %q not found", tag)
}

// GetInboundConfig returns the default inbound if the tag matches.
func (hc *HysteriaContainer) GetInboundConfig(tag string) (inbound.Inbound, error) {
	if tag == defaultInboundTag {
		return hc.inbound, nil
	}
	return nil, fmt.Errorf("hysteria: inbound %q not found", tag)
}

// ListInboundConfigs returns the single default inbound.
func (hc *HysteriaContainer) ListInboundConfigs() []inbound.Inbound {
	return []inbound.Inbound{hc.inbound}
}

// FastAddInbound is not supported for hysteria — hysteria uses a single fixed inbound.
func (hc *HysteriaContainer) FastAddInbound(_ string, _ map[string]any) error {
	return fmt.Errorf("hysteria: FastAddInbound not supported; hysteria uses a single fixed inbound")
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
// For hysteria, user add/remove are no-ops (no forward port needed).
func (hc *HysteriaContainer) handleUserEvent(event usermanager.UserEvent) {
	switch event.Type {
	case usermanager.UserEventAdd:
		// No-op: hysteria users share the same port, no forward rule needed
	case usermanager.UserEventRemove:
		// No-op: hysteria users share the same port, no forward rule needed
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

		// Re-create inbound with restored config
		hc.inbound = inbound.NewDefaultInbound(
			defaultInboundTag,
			contracts.ProtocolHysteria2,
			uint32(hc.cfg.Port),
		)
		hc.inbound.(*inbound.DefaultInbound).SetListenAddr(hc.cfg.Listen)
		return nil
	}

	return nil
}

// GetUserSubscriptions returns subscription specs for the given user.
func (hc *HysteriaContainer) GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
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
		req.User.Password, req.Host, hc.cfg.Port, params, nodeName)

	spec := contracts.SubscriptionSpec{
		Protocol:   contracts.ProtocolHysteria2,
		Host:       req.Host,
		Port:       uint32(hc.cfg.Port),
		Password:   req.User.Password,
		NodeName:   nodeName,
		InboundTag: defaultInboundTag,
		Username:   req.User.Username,
		URI:        uri,
	}
	return []contracts.SubscriptionSpec{spec}, nil
}

// Restore is a no-op for hysteria (no forward rules to rebuild).
func (hc *HysteriaContainer) Restore(ctx context.Context) error {
	return nil
}

// reconcileUsers is a no-op for hysteria (no forward rules needed).
func (hc *HysteriaContainer) reconcileUsers() {
	// No-op
}

// startReconcileLoop starts the periodic user reconciliation goroutine.
func (hc *HysteriaContainer) startReconcileLoop() {
	// No-op for hysteria
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

