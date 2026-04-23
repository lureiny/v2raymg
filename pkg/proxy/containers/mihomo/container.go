package mihomo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	"github.com/lureiny/v2raymg/pkg/proxy/tools/process"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/store"
)

// pushConfigTimeout bounds how long a REST PUT /configs may take. On
// localhost even a 1k-listener yaml should land in well under a second;
// 10 seconds leaves head-room for warm-up and slow disks without hiding a
// truly broken mihomo.
const pushConfigTimeout = 10 * time.Second

// reconcileInterval is the period of the background reconcile loop. 30s
// matches xray/snell; long enough that drift detection stays cheap, short
// enough that a missed UserEvent (channel full, transient error) converges
// within a user-noticeable window.
const reconcileInterval = 30 * time.Second

// MihomoContainer implements the Container interface for mihomo.
//
// Concurrency model:
//   - mu (RWMutex) guards runner / restClient / cachedVersion. Only held
//     around field assignments, never across I/O.
//   - inboundsMu (Mutex) guards the inbounds map AND serialises REST pushes
//     into mihomo. Held across PutConfigs so callers see "mutate then push"
//     as one atomic step — at the cost of Get/List briefly blocking on any
//     in-flight push. Acceptable because push is localhost-fast and reads
//     are not on a hot path.
//   - Per-inbound addedUsers (in MihomoInbound.addedUsersMu) is independent
//     of both locks above. See xray/exec_runner.go for the same model.
type MihomoContainer struct {
	*container.BaseContainer
	cfg MihomoConfig

	mu            sync.RWMutex
	runner        *process.Runner
	restClient    *RESTClient
	cachedVersion string

	inboundsMu sync.Mutex
	inbounds   map[string]*MihomoInbound

	storeMgr *store.StoreManager

	// User event + reconcile plumbing. All populated by WithUserManager at
	// construction time; nil if no user manager is provided (unit tests
	// that exercise only inbound CRUD).
	userMgr         *usermanager.UserManager
	userEventCh     chan usermanager.UserEvent
	userMgrSubCh    <-chan usermanager.UserEvent // kept for Unsubscribe on Close
	forwardStopCh   chan struct{}                // closed on Close to drain forwardUserEvents

	reconcileStopCh chan struct{}
	reconcileWg     sync.WaitGroup

	// userEventHandlerStarted makes startUserEventHandler idempotent so a
	// Start() retry (BaseContainer reverts to Stopped on run-hook failure
	// without calling the stop hook) doesn't spawn a second consumer on
	// userEventCh. Guarded by mu; written only by startUserEventHandler.
	userEventHandlerStarted bool

	// closeOnce makes Close safe to call from multiple goroutines
	// concurrently — the naive "select + close" is not actually idempotent
	// against concurrent callers (two callers can both observe default
	// and race to close, panicking on the second call).
	closeOnce sync.Once
}

// MihomoOption configures optional dependencies at construction time.
type MihomoOption func(*MihomoContainer)

// WithStoreMgr wires the inbound store so FastAddInbound/RemoveInboundConfig
// persist their effect across restarts. When omitted, state is kept in
// memory only — OK for tests, never OK in production.
func WithStoreMgr(sm *store.StoreManager) MihomoOption {
	return func(c *MihomoContainer) { c.storeMgr = sm }
}

// WithUserManager wires the user manager so user-event-driven forward rule
// CRUD works. Subscribes eagerly at construction (not at Start) so that
// events emitted during the pre-Start window are buffered into the
// container's local channel rather than dropped. Matches xray's pattern.
func WithUserManager(um *usermanager.UserManager) MihomoOption {
	return func(c *MihomoContainer) { c.userMgr = um }
}

type mihomoHooks struct {
	c *MihomoContainer
}

// GetRunFunc returns the run/stop hooks invoked by BaseContainer.Start/Stop.
// The run sequence:
//  1. Start the user-event handler goroutine FIRST so events buffered in
//     userEventCh get drained promptly; no-op when userMgr wasn't provided.
//  2. Start the periodic reconcile loop (same no-op rule).
//  3. Bring the mihomo process up.
//  4. Replay persisted inbounds via REST.
//  5. One-shot reconcileUsers to wire forward rules for any users that
//     existed before Start.
//
// Failures past step 3 tear down the process so the container doesn't sit
// half-configured. The event handler / reconcile loop are intentionally not
// torn down on intermediate failure: the goroutines are cheap, and a later
// Start() retry doesn't need to re-subscribe.
func (h *mihomoHooks) GetRunFunc() (func() error, func() error) {
	run := func() error {
		h.c.startUserEventHandler()
		h.c.startReconcileLoop()

		if err := h.c.startProcess(); err != nil {
			return err
		}
		if err := h.c.restoreAndPushInbounds(); err != nil {
			_ = h.c.stopProcess()
			return fmt.Errorf("mihomo: restore inbounds after start: %w", err)
		}
		h.c.reconcileUsers()
		return nil
	}
	stop := func() error {
		h.c.stopReconcileLoop()
		return h.c.stopProcess()
	}
	return run, stop
}

// NewMihomoContainer builds a MihomoContainer from the given config and options.
func NewMihomoContainer(cfg MihomoConfig, opts ...MihomoOption) (*MihomoContainer, error) {
	c := &MihomoContainer{
		cfg:      cfg,
		inbounds: make(map[string]*MihomoInbound),
	}
	for _, opt := range opts {
		opt(c)
	}
	c.BaseContainer = container.NewBaseContainer(contracts.ContainerMihomo, &mihomoHooks{c: c})

	// Subscribe to user events eagerly so early events (added between
	// construction and Start) are buffered rather than lost. Buffer size
	// matches xray; the forwardUserEvents goroutine drops when full.
	// forwardStopCh is held so Close() can terminate the goroutine;
	// userMgrSubCh is held so Close() can Unsubscribe from UserManager.
	// (xray leaks this goroutine today — mihomo chooses to provide a
	// clean shutdown path instead.)
	if c.userMgr != nil {
		c.userEventCh = make(chan usermanager.UserEvent, 100)
		c.forwardStopCh = make(chan struct{})
		c.userMgrSubCh = c.userMgr.Subscribe()
		go c.forwardUserEvents(c.userMgrSubCh)
	}

	return c, nil
}

// Close tears down the user-event subscription and the forwardUserEvents
// goroutine started at construction. Safe to call multiple times (via
// sync.Once) AND from concurrent goroutines; safe when WithUserManager
// was never passed. Intended for production shutdown AND tests that
// construct MihomoContainer via WithUserManager — without Close, the
// goroutine and UserManager subscription leak for the process lifetime.
//
// Close does NOT stop the reconcile loop or the mihomo process; those are
// the Start/Stop lifecycle's responsibility. Use Stop first if the
// container is running, then Close.
//
// Lock discipline: mu is held only while reading+clearing the channel and
// subscription references. The UserManager.Unsubscribe call runs outside
// the lock — Unsubscribe is lock-only today, but holding mu across an
// external call violates the "never across I/O" invariant documented on
// the MihomoContainer struct and would risk future deadlock if
// UserManager grows a callback into container code.
func (c *MihomoContainer) Close() {
	c.closeOnce.Do(func() {
		// Don't nil forwardStopCh after close — forwardUserEvents reads
		// the field from its own goroutine in a select, and a concurrent
		// nil-write would race the read even though the goroutine is about
		// to return. The sync.Once already prevents double-close, so the
		// field can safely remain set to the (now-closed) channel.
		c.mu.Lock()
		stopCh := c.forwardStopCh
		sub := c.userMgrSubCh
		c.userMgrSubCh = nil
		c.mu.Unlock()

		if stopCh != nil {
			close(stopCh)
		}
		if c.userMgr != nil && sub != nil {
			c.userMgr.Unsubscribe(sub)
		}
	})
}

func (c *MihomoContainer) Init(_ any) error { return nil }

// Reload re-pushes the current in-memory listener set to mihomo without
// restarting the process. This is the cert / shared-credential hot-update
// path: after mutating a MihomoInbound's SharedCred (or any field reflected
// in profilegen), call Reload to send the new yaml. mihomo's
// PatchInboundListeners compares each named listener's Config.Equal;
// listeners whose yaml text changed are Close()d and Listen()ed anew (their
// live TCP connections drop), while untouched listeners keep running.
//
// No-op when the container isn't running (pushConfig short-circuits on a
// nil restClient) — the current state is already the "about to be pushed"
// state, and Start will push it at process-ready time.
func (c *MihomoContainer) Reload() error      { return c.pushConfig() }
func (c *MihomoContainer) ConfigFile() string { return c.cfg.ConfigFilePath }

// Restore is the Restorable capability entry point invoked by
// container.ContainerMgr.StartAll after Start() returns. It rebuilds the
// in-memory inbound map from the InboundStore and converges forward rules
// for every visible user.
//
// The Start hook already runs restoreAndPushInbounds + reconcileUsers on
// process-ready (so the container is functional without an external Restore
// call — matching hysteria/snell). Restore therefore serves two purposes:
//
//  1. Fulfils the Restorable contract so ContainerMgr.StartAll drives it
//     after Start, matching the framework expectation and giving operators
//     an explicit RestoreAll entry point.
//  2. Runs a second idempotent pass: restoreAndPushInbounds clears the map
//     and reloads from store (covering the case where store state changed
//     between Start and Restore), and reconcileUsers converges any user
//     drift (e.g. a visibility event that arrived mid-Start).
//
// Both helpers are idempotent by construction: the PUT /configs uses
// name-keyed diff so an unchanged set of listeners is a no-op on the mihomo
// side, and reconcileUsers uses addedUsers tracking + UserManager.GetBindPort
// (idempotent) / ReleaseBindPort (idempotent). Running twice costs one REST
// round-trip + a user-set walk; a third run would be the same.
//
// Post-condition on failure: if restoreAndPushInbounds returns an error, the
// in-memory map has ALREADY been repopulated from the store but mihomo
// didn't accept the configuration. We intentionally do NOT roll the map
// back — the next reconcileDrift tick (every 30s) retries pushConfig against
// the same map state, so a transient REST failure self-heals within one
// tick. Callers that need "either fully applied or fully unchanged" semantics
// should retry Restore until it returns nil.
//
// Concurrency: Restore is designed for quiescent windows (StartAll, or an
// operator-triggered RestoreAll). Under live traffic, a concurrent
// snapshotInbounds that captures a *MihomoInbound pointer before Restore's
// clear+repopulate might continue operating on the pre-Restore object.
// handleUserEvent using that stale pointer is idempotent at the forward
// layer (GetBindPort returns the existing port), so there's no correctness
// issue — just a bounded amount of redundant UserManager calls until the
// next reconcileUsers tick observes the live pointer.
//
// Errors from restoreAndPushInbounds are returned (they typically mean the
// store is unreadable or mihomo rejected the config — both warrant caller
// attention). reconcileUsers's per-user failures are already logged
// internally and never propagated.
func (c *MihomoContainer) Restore(_ context.Context) error {
	if err := c.restoreAndPushInbounds(); err != nil {
		return err
	}
	c.reconcileUsers()
	return nil
}

func (c *MihomoContainer) Version() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cachedVersion
}

func (c *MihomoContainer) Update(_ context.Context, _ container.UpdateRequest) (*container.UpdateResult, error) {
	return nil, container.ErrNotSupported
}

// FastAddInbound creates a new mihomo listener from the given params and
// pushes the updated config to mihomo. Shared credential model: all users
// on this inbound will authenticate with the single credential carried in
// params. Users themselves are onboarded via the forward layer (stage 5).
//
// Persistence order: store first, then map, then REST. If REST fails the
// store record and map entry are both rolled back so a subsequent Start
// replay doesn't resurrect a listener mihomo rejected.
func (c *MihomoContainer) FastAddInbound(tag string, params map[string]any) error {
	inb, err := FromParams(tag, params)
	if err != nil {
		return err
	}
	// Wire the user manager onto the inbound before it enters the map so
	// a concurrent user event reading inbounds via handleUserEvent finds
	// it ready for AddUser. No-op when userMgr is nil (unit tests).
	if c.userMgr != nil {
		inb.SetUserManager(c.userMgr)
	}

	if err := c.addInboundLocked(tag, inb); err != nil {
		return err
	}

	// Sync existing users to this new inbound AFTER releasing inboundsMu
	// — GetBindPort is UserManager-owned and slow; holding inboundsMu
	// across it would stall concurrent List/Get/user-event snapshots.
	// Any orphan forward rules from a racing RemoveInboundConfig are
	// cleaned up by that Remove's ReleaseAllUserPorts call, and the
	// periodic reconcile loop converges further drift. Matches xray.
	c.reconcileUsersForInbound(tag)
	return nil
}

// addInboundLocked holds inboundsMu across the "persist + map + REST push"
// tuple so stage-4 atomicity is preserved. Split out of FastAddInbound so
// the reconcile step can run outside the lock.
func (c *MihomoContainer) addInboundLocked(tag string, inb *MihomoInbound) error {
	c.inboundsMu.Lock()
	defer c.inboundsMu.Unlock()

	if _, exists := c.inbounds[tag]; exists {
		return fmt.Errorf("mihomo: inbound %q already exists", tag)
	}

	if err := c.persistInbound(inb); err != nil {
		return fmt.Errorf("mihomo: persist inbound: %w", err)
	}

	c.inbounds[tag] = inb

	if err := c.pushConfigLocked(); err != nil {
		// Roll back: REST rejected the listener, so the record on disk
		// and the map entry are both phantom state. If the store delete
		// ALSO fails we surface it via errors.Join — a leaked record
		// would otherwise be silently replayed by the next Start and
		// push the rejected listener again.
		delete(c.inbounds, tag)
		if storeErr := c.deleteInboundRecord(tag); storeErr != nil {
			log.Errorf("mihomo: rollback store delete for %q failed: %v (stale record may resurrect on restart)", tag, storeErr)
			return fmt.Errorf("mihomo: push config and rollback: %w", errors.Join(err, storeErr))
		}
		return fmt.Errorf("mihomo: push config: %w", err)
	}
	return nil
}

// RemoveInboundConfig removes an inbound by tag. Four phases, each failing
// in a recoverable way:
//
//  1. Lookup: missing tag returns an error with no side effects.
//  2. Release forward rules (ReleaseAllUserPorts): tears down per-user
//     relays first so no client can connect to a relay that points at a
//     backend we're about to delete. Failure here returns early, the
//     inbound is still fully registered, retry is idempotent.
//  3. REST push without this inbound: failure leaves the forward rules
//     already released but the listener still live on mihomo. A retry
//     re-releases (no-op) and re-pushes (name-keyed diff, no-op when no
//     drift). Acceptable — the listener is localhost-only so the
//     "released but listener live" window is safe.
//  4. Store delete + map delete: on store-delete failure we return an
//     error and leave the map entry so a retry can finish the store
//     delete. On success we drop the map entry.
//
// Ordering matches xray's RemoveInboundConfig exactly (see
// pkg/proxy/containers/xray/exec_runner.go:526-558): release forward →
// store delete → runtime remove → map delete. Earlier mihomo code did
// REST push first and logged release failures; that left orphan rules
// pointing at a dead listener if release failed after REST succeeded,
// and reconcileUsers does not sweep inbound-tag-scoped orphans.
func (c *MihomoContainer) RemoveInboundConfig(tag string) error {
	c.inboundsMu.Lock()
	defer c.inboundsMu.Unlock()

	inb, exists := c.inbounds[tag]
	if !exists {
		return fmt.Errorf("mihomo: inbound %q not found", tag)
	}

	// Phase 2: Release forward rules BEFORE REST push. If this fails we
	// haven't changed any state yet; caller can retry.
	if err := inb.ReleaseAllUserPorts(); err != nil {
		return fmt.Errorf("mihomo: release forward rules for inbound %q: %w", tag, err)
	}

	// Phase 3: Push the post-delete listener set.
	snapshot := make([]*MihomoInbound, 0, len(c.inbounds)-1)
	for t, v := range c.inbounds {
		if t == tag {
			continue
		}
		snapshot = append(snapshot, v)
	}
	if err := c.pushConfigWithSnapshot(snapshot); err != nil {
		return fmt.Errorf("mihomo: push config: %w", err)
	}

	// Phase 4: Store delete then map delete.
	if err := c.deleteInboundRecord(tag); err != nil {
		return fmt.Errorf("mihomo: delete inbound record %q (listener already removed from mihomo; retry to finish): %w", tag, err)
	}

	delete(c.inbounds, tag)
	return nil
}

// GetInboundConfig returns a snapshot of the inbound with the given tag.
// The returned *MihomoInbound is the live pointer — callers must not mutate
// its fields; they should treat the return as read-only.
func (c *MihomoContainer) GetInboundConfig(tag string) (inbound.Inbound, error) {
	c.inboundsMu.Lock()
	defer c.inboundsMu.Unlock()
	inb, exists := c.inbounds[tag]
	if !exists {
		return nil, fmt.Errorf("mihomo: inbound %q not found", tag)
	}
	return inb, nil
}

// ListInboundConfigs returns all inbounds. Order is unspecified.
func (c *MihomoContainer) ListInboundConfigs() []inbound.Inbound {
	c.inboundsMu.Lock()
	defer c.inboundsMu.Unlock()
	out := make([]inbound.Inbound, 0, len(c.inbounds))
	for _, inb := range c.inbounds {
		out = append(out, inb)
	}
	return out
}

// UserEventChannel returns the container's internal user-event channel.
// nil when WithUserManager wasn't passed — documented as "this container
// doesn't subscribe".
func (c *MihomoContainer) UserEventChannel() <-chan usermanager.UserEvent { return c.userEventCh }

// persistInbound writes the inbound to InboundStore. No-op when storeMgr
// isn't configured (test paths).
func (c *MihomoContainer) persistInbound(inb *MihomoInbound) error {
	if c.storeMgr == nil {
		return nil
	}
	native, err := inb.ToNative()
	if err != nil {
		return fmt.Errorf("serialise: %w", err)
	}
	rec := &store.InboundRecord{
		Tag:           inb.Tag(),
		ContainerType: string(contracts.ContainerMihomo),
		NativeJSON:    native,
	}
	return c.storeMgr.InboundStore().Save(rec)
}

func (c *MihomoContainer) deleteInboundRecord(tag string) error {
	if c.storeMgr == nil {
		return nil
	}
	return c.storeMgr.InboundStore().Delete(tag)
}

// pushConfigLocked builds yaml from the current inbounds map and sends it
// to mihomo. Caller must hold inboundsMu. When the container isn't running
// (restClient nil), this is a no-op — the Start hook replays the aggregate
// state when the process comes up.
func (c *MihomoContainer) pushConfigLocked() error {
	snapshot := make([]*MihomoInbound, 0, len(c.inbounds))
	for _, inb := range c.inbounds {
		snapshot = append(snapshot, inb)
	}
	return c.pushConfigWithSnapshot(snapshot)
}

// pushConfigWithSnapshot pushes a specific set of inbounds rather than the
// current map state. Used by RemoveInboundConfig to push the "without tag"
// view before committing the delete locally.
func (c *MihomoContainer) pushConfigWithSnapshot(inbounds []*MihomoInbound) error {
	c.mu.RLock()
	rc := c.restClient
	c.mu.RUnlock()
	if rc == nil {
		return nil
	}

	data, err := BuildConfig(c.buildBaseConfig(), inbounds)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), pushConfigTimeout)
	defer cancel()
	return rc.PutConfigs(ctx, data, false)
}

// pushConfig is the exported push path (not caller-lock-required). Used by
// Reload to re-push current state without mutation. Acquires inboundsMu
// internally.
func (c *MihomoContainer) pushConfig() error {
	c.inboundsMu.Lock()
	defer c.inboundsMu.Unlock()
	return c.pushConfigLocked()
}

// buildBaseConfig produces the base mihomo config with outbound capabilities
// disabled. The Listeners field is overwritten by BuildConfig from the
// current inbounds set.
func (c *MihomoContainer) buildBaseConfig() mihomoInitialConfig {
	return mihomoInitialConfig{
		MixedPort:          0,
		AllowLan:           false,
		ExternalController: c.cfg.ExternalController,
		Secret:             c.cfg.Secret,
		LogLevel:           "info",
		Mode:               "global",
		Proxies:            []any{},
		ProxyGroups:        []any{},
		Rules:              []string{"MATCH,DIRECT"},
		Listeners:          []any{}, // overwritten by BuildConfig
	}
}

// restoreAndPushInbounds is invoked on Start after the process is ready.
// It loads persisted mihomo inbounds into the map and pushes the full set
// via REST. A missing store is treated as "no persisted state" — Start
// still succeeds with an empty listeners array.
//
// The map is cleared before re-populating so a failed Start followed by a
// retry doesn't leak entries from the first attempt — BaseContainer allows
// Start() to be called again after a hook failure and the same
// MihomoContainer instance is reused.
func (c *MihomoContainer) restoreAndPushInbounds() error {
	if c.storeMgr != nil {
		records, err := c.storeMgr.InboundStore().Load()
		if err != nil {
			return fmt.Errorf("load inbound records: %w", err)
		}
		c.inboundsMu.Lock()
		clear(c.inbounds)
		for _, rec := range records {
			if rec.ContainerType != string(contracts.ContainerMihomo) {
				continue
			}
			inb, err := FromNative(rec.NativeJSON)
			if err != nil {
				// A single corrupt record shouldn't block startup. Log and
				// skip — operator can fix or delete the record.
				log.Warnf("mihomo: skip unrecoverable inbound record %q: %v", rec.Tag, err)
				continue
			}
			// Re-attach user manager to restored inbounds so user-event and
			// reconcile paths can call AddUser/RemoveUser/ReleaseAllUserPorts.
			if c.userMgr != nil {
				inb.SetUserManager(c.userMgr)
			}
			c.inbounds[rec.Tag] = inb
		}
		c.inboundsMu.Unlock()
	} else {
		// No store configured: still clear the map so the restore path is
		// idempotent across retries.
		c.inboundsMu.Lock()
		clear(c.inbounds)
		c.inboundsMu.Unlock()
	}
	return c.pushConfig()
}

// forwardUserEvents drains events from UserManager's subscription channel
// into the container's local buffered channel. Decouples UserManager's
// broadcast loop from mihomo's handler — if mihomo's handler stalls, only
// the local channel fills up (events dropped non-blocking), not UserManager's
// broadcast.
//
// Terminates when forwardStopCh is closed (via Close) or when source is
// closed (UserManager shutdown). Close is the production/test shutdown path;
// source-close is defensive for deployments where UserManager owns lifecycle.
func (c *MihomoContainer) forwardUserEvents(source <-chan usermanager.UserEvent) {
	for {
		select {
		case event, ok := <-source:
			if !ok {
				return
			}
			if c.userEventCh != nil {
				select {
				case c.userEventCh <- event:
				default:
					// Channel full — event dropped. Periodic reconcile
					// loop will converge state within reconcileInterval.
				}
			}
		case <-c.forwardStopCh:
			return
		}
	}
}

// startUserEventHandler spawns a goroutine that consumes userEventCh and
// dispatches each event via handleUserEvent. No-op when WithUserManager
// wasn't passed OR when a handler goroutine is already running.
//
// Idempotent on purpose: BaseContainer.Start reverts state to Stopped on
// run-hook failure without invoking the stop hook, so a retry re-enters
// this function. A second goroutine consuming the same channel would
// race — each event fans out to exactly one consumer, but which one is
// non-deterministic, and during a partial-Start an event could land on
// the handler of a container whose inbounds map is in the process of
// being cleared.
//
// The handler goroutine is not terminated by Stop() — it exits when
// userEventCh is closed, which only happens via Close(). That matches
// xray's model (long-lived subscription tied to container lifetime, not
// start/stop cycles).
func (c *MihomoContainer) startUserEventHandler() {
	if c.userEventCh == nil {
		return
	}
	c.mu.Lock()
	if c.userEventHandlerStarted {
		c.mu.Unlock()
		return
	}
	c.userEventHandlerStarted = true
	c.mu.Unlock()

	go func() {
		for event := range c.userEventCh {
			c.handleUserEvent(event)
		}
	}()
}

// handleUserEvent routes a single user event to every inbound.
//
//   - Add:    allocate forward rule on every inbound for this user
//   - Remove: release forward rule on every inbound for this user
//   - Update: re-evaluate IsUserVisible; flip to add or remove accordingly
//
// Per-inbound failures are logged (inside syncUserToInbound /
// removeUserFromInbounds) and do not short-circuit the rest of the
// inbounds — a single misbehaving inbound shouldn't block the others.
func (c *MihomoContainer) handleUserEvent(event usermanager.UserEvent) {
	if c.userMgr == nil {
		return
	}
	switch event.Type {
	case usermanager.UserEventAdd:
		c.syncUserToInbound(event.Username, event.User)
	case usermanager.UserEventRemove:
		c.removeUserFromInbounds(event.Username)
	case usermanager.UserEventUpdate:
		if event.User != nil && !c.userMgr.IsUserVisible(event.User) {
			c.removeUserFromInbounds(event.Username)
		} else if event.User != nil {
			c.syncUserToInbound(event.Username, event.User)
		}
	}
}

// syncUserToInbound wires a user onto every current inbound. Snapshots
// the inbound list under inboundsMu so the AddUser calls — which may block
// on UserManager.GetBindPort — run outside any container-level lock.
func (c *MihomoContainer) syncUserToInbound(username string, user *contracts.User) {
	if user == nil {
		return
	}
	for _, inb := range c.snapshotInbounds() {
		if _, err := inb.AddUser(username, user); err != nil {
			log.Warn("mihomo: add user to inbound failed",
				"user", username, "inbound", inb.Tag(), "err", err)
		}
	}
}

// removeUserFromInbounds tears down the forward rule for a user on every
// current inbound. Idempotent per inbound; safe to call for users that
// were never added.
func (c *MihomoContainer) removeUserFromInbounds(username string) {
	for _, inb := range c.snapshotInbounds() {
		if err := inb.RemoveUser(username); err != nil {
			log.Warn("mihomo: remove user from inbound failed",
				"user", username, "inbound", inb.Tag(), "err", err)
		}
	}
}

// snapshotInbounds returns a shallow copy of the current inbound slice so
// callers can iterate without holding inboundsMu. Pointers in the returned
// slice remain valid after unlock because MihomoInbound isn't pooled —
// concurrent RemoveInboundConfig may drop the tag from the map, but the
// underlying object (and its addedUsers / userMgr) stays reachable until
// the caller lets go.
func (c *MihomoContainer) snapshotInbounds() []*MihomoInbound {
	c.inboundsMu.Lock()
	defer c.inboundsMu.Unlock()
	out := make([]*MihomoInbound, 0, len(c.inbounds))
	for _, inb := range c.inbounds {
		out = append(out, inb)
	}
	return out
}

// reconcileUsers is the "full sweep" path: bring every inbound's forward
// rules in line with the UserManager's current visible-user set.
//
// Two phases per inbound:
//  1. Remove users that the inbound has tracked but UserManager no longer
//     considers visible (e.g. group change, user removed).
//  2. Add users that are visible but not yet tracked by the inbound.
//
// Called on Start after the initial restore, and on every reconcile tick.
// Best-effort: per-user failures are logged and skipped.
func (c *MihomoContainer) reconcileUsers() {
	if c.userMgr == nil {
		return
	}
	users := c.userMgr.ListUsers()
	visible := make(map[string]*contracts.User, len(users))
	for _, u := range users {
		visible[u.Username] = u
	}

	for _, inb := range c.snapshotInbounds() {
		for _, username := range inb.listAddedUsers() {
			if _, ok := visible[username]; ok {
				continue
			}
			if err := inb.RemoveUser(username); err != nil {
				log.Warn("mihomo: reconcile remove stale user failed",
					"user", username, "inbound", inb.Tag(), "err", err)
			}
		}
		for _, user := range users {
			if inb.hasAddedUser(user.Username) {
				continue
			}
			if _, err := inb.AddUser(user.Username, user); err != nil {
				log.Warn("mihomo: reconcile add user failed",
					"user", user.Username, "inbound", inb.Tag(), "err", err)
			}
		}
	}
}

// reconcileUsersForInbound wires every currently-visible user to a single
// newly-added inbound. Called by FastAddInbound (outside inboundsMu) so
// existing users get forward rules on the fresh listener without waiting
// for the next reconcile tick.
//
// Looks up the inbound via inboundsMu; a concurrent RemoveInboundConfig
// that beat this call returns silently (the Remove's ReleaseAllUserPorts
// cleaned any earlier state).
func (c *MihomoContainer) reconcileUsersForInbound(tag string) {
	if c.userMgr == nil {
		return
	}
	c.inboundsMu.Lock()
	inb, exists := c.inbounds[tag]
	c.inboundsMu.Unlock()
	if !exists {
		return
	}
	for _, user := range c.userMgr.ListUsers() {
		if inb.hasAddedUser(user.Username) {
			continue
		}
		if _, err := inb.AddUser(user.Username, user); err != nil {
			log.Warn("mihomo: reconcile for inbound add user failed",
				"user", user.Username, "inbound", tag, "err", err)
		}
	}
}

// reconcileDrift is the full sweep run on every reconcile tick. It does
// NOT probe mihomo for drift — it enforces our in-memory inbounds map as
// the source of truth, and converges the forward layer with UserManager's
// visible users. Two independent concerns:
//
//  1. map → mihomo (enforce): pushConfig() sends the current inbound set.
//     mihomo's PatchInboundListeners (name-keyed diff, dropOld=true) deletes
//     any listener not in the pushed map and re-Listens any whose config
//     text changed. So if a process restart brought mihomo back with no
//     listeners, or an operator added/mutated a listener via /configs
//     directly, the next tick overwrites it. We don't detect the drift —
//     we just re-assert the truth cheaply.
//  2. user → forward rules (converge): reconcileUsers aligns forward rules
//     with UserManager.ListUsers. Covers missed UserEvents (channel full
//     during bursts, transient failure) within reconcileInterval.
//
// Concern 1 failing doesn't block concern 2: if mihomo is unreachable we
// still reconcile users so the forward layer (the source-of-truth for
// traffic accounting) stays correct. Concern 2 failures are logged per-user
// inside reconcileUsers and similarly don't halt the tick.
//
// Note: store → map drift is *not* re-checked here. FastAddInbound /
// RemoveInboundConfig keep store and map in lockstep at runtime, and a
// process restart is the only time they can meaningfully diverge — the
// restoreAndPushInbounds call inside Restore covers that window. Repeating
// the store Load on every reconcile tick would be wasted DB work.
func (c *MihomoContainer) reconcileDrift() {
	if err := c.pushConfig(); err != nil {
		log.Warnf("mihomo: reconcile push config failed (continuing with user reconcile): %v", err)
	}
	c.reconcileUsers()
}

// startReconcileLoop spawns the periodic reconcile goroutine. Stopped via
// stopReconcileLoop (close the channel, wait for goroutine). No-op when
// userMgr isn't set — there's nothing to reconcile. Idempotent: a
// non-nil reconcileStopCh means a goroutine is already ticking, so a
// Start() retry after a run-hook failure does not spawn a duplicate.
// stopReconcileLoop nils the channel on exit, so a legitimate Stop+Start
// cycle still boots a fresh loop.
func (c *MihomoContainer) startReconcileLoop() {
	if c.userMgr == nil {
		return
	}
	if c.reconcileStopCh != nil {
		return
	}
	c.reconcileStopCh = make(chan struct{})
	c.reconcileWg.Add(1)
	go func() {
		defer c.reconcileWg.Done()
		ticker := time.NewTicker(reconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.reconcileDrift()
			case <-c.reconcileStopCh:
				return
			}
		}
	}()
}

// stopReconcileLoop signals the reconcile goroutine to exit and waits for
// it. Idempotent — safe to call when the loop was never started.
func (c *MihomoContainer) stopReconcileLoop() {
	if c.reconcileStopCh == nil {
		return
	}
	close(c.reconcileStopCh)
	c.reconcileWg.Wait()
	c.reconcileStopCh = nil
}
