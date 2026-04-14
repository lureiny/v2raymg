// Package usermanager provides user management for proxy containers.
// It handles user CRUD operations and port allocation.
package usermanager

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/errors"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	usync "github.com/lureiny/v2raymg/pkg/proxy/usermanager/sync"
	"github.com/lureiny/v2raymg/pkg/store"
)

// generateAuthToken returns a new UUID v4 string.
func generateAuthToken() string {
	return uuid.NewString()
}

// isValidUUIDv4 returns true if s is a standard RFC 4122 UUID version 4.
// Checks format, version nibble (4), and variant bits (RFC 4122 = 10xx).
func isValidUUIDv4(s string) bool {
	u, err := uuid.Parse(s)
	return err == nil && u.Version() == 4 && u.Variant() == uuid.RFC4122
}

// generateUniqueAuthTokenLocked generates a UUID that does not collide with any
// existing user's AuthToken. Caller must hold m.mu (read or write).
func (m *UserManager) generateUniqueAuthTokenLocked() string {
	for {
		token := generateAuthToken()
		collision := false
		for _, u := range m.users {
			if u.AuthToken == token {
				collision = true
				break
			}
		}
		if !collision {
			return token
		}
		log.Warn("[generateUniqueAuthTokenLocked] UUID collision detected, retrying")
	}
}

// UserEventType represents the type of user event.
type UserEventType string

const (
	// UserEventAdd indicates a new user was added.
	UserEventAdd UserEventType = "add"
	// UserEventRemove indicates a user was removed.
	UserEventRemove UserEventType = "remove"
	// UserEventUpdate indicates a user was updated (e.g., password change).
	UserEventUpdate UserEventType = "update"
	// UserEventExpire indicates a user has expired.
	UserEventExpire UserEventType = "expire"
	// UserEventPortBind indicates user's port binding changed.
	UserEventPortBind UserEventType = "port_bind"
)

// UserEvent represents a user state change event.
type UserEvent struct {
	Type     UserEventType
	Username string
	User     *contracts.User // nil for remove/expire events
	Ports    []uint32        // affected ports for port_bind events
}

// UserLister is the interface to list users.
type UserLister interface {
	ListUsers() []*contracts.User
}

// Ensure UserManager implements UserLister
var _ UserLister = (*UserManager)(nil)

// NodeGroupsStore is the persistence interface for local node group assignments.
type NodeGroupsStore interface {
	List() ([]string, error)
	Set(groups []string) error
}

// UserManager manages users and their port bindings.
// It also provides an event channel for user state changes.
type UserManager struct {
	mu            sync.RWMutex
	users         map[string]*contracts.User
	forwardMgr    forward.ForwardManager
	store         UserStore // nil = pure in-memory mode

	// eventCh is the legacy single channel for broadcasting user events.
	// Deprecated: use Subscribe() for multi-consumer pub/sub.
	// Kept for backward compatibility with code that calls UserEventChannel().
	eventCh      chan UserEvent
	eventChMutex sync.Mutex
	closed       bool

	// subscribers is the list of subscriber channels for pub/sub event distribution.
	// Each container gets its own channel via Subscribe().
	subscribers []chan UserEvent

	// statsCollector handles periodic traffic stats collection
	statsCollector *statsCollector

	// loginPasswordHasher, if set, is called to produce a login_password hash from
	// a plaintext proxy password whenever a user is created or has their password changed.
	// It is a function field to avoid a circular import on pkg/http/auth.
	loginPasswordHasher func(string) (string, error)

	// Cluster sync fields — clusterEnabled controls heartbeat digest exchange only.
	// Version stamping (stampVersion) is always active regardless of this flag.
	clusterEnabled  bool
	localNodeName   string
	defaultGroup    string
	nodeGroupsStore NodeGroupsStore

	// cachedNodeGroups is the in-memory set of group names this node belongs to.
	// nil or empty means "no filtering" (backward compat / fail-open).
	// Protected by mu.
	cachedNodeGroups map[string]struct{}
}

// NewUserManager creates a new user manager (pure in-memory mode, no persistence).
// localNodeName identifies this node for version stamping; use the node's config name.
func NewUserManager(fm forward.ForwardManager, localNodeName string) *UserManager {
	return &UserManager{
		users:          make(map[string]*contracts.User),
		forwardMgr:     fm,
		localNodeName:  localNodeName,
		eventCh:        make(chan UserEvent, 100), // Buffered channel
		statsCollector: newStatsCollector(fm, time.Second),
	}
}

// NewUserManagerWithStore creates a new user manager backed by the given store manager.
// All users are loaded from store on construction. Returns an error if load fails.
// localNodeName identifies this node for version stamping; use the node's config name.
func NewUserManagerWithStore(fm forward.ForwardManager, storeMgr *store.StoreManager, localNodeName string) (*UserManager, error) {
	m := &UserManager{
		users:          make(map[string]*contracts.User),
		forwardMgr:     fm,
		localNodeName:  localNodeName,
		store:          storeMgr.UserStore(),
		eventCh:        make(chan UserEvent, 100),
		statsCollector: newStatsCollector(fm, time.Second),
	}

	loaded, err := storeMgr.UserStore().Load()
	if err != nil {
		return nil, fmt.Errorf("NewUserManagerWithStore: load users: %w", err)
	}
	for _, u := range loaded {
		m.users[u.Username] = u
	}

	// Migrate legacy auth tokens (non-UUID) to UUID format.
	// Uses ResetAuthToken which goes through mutateUser (stampVersion + persist + event).
	for _, u := range m.users {
		if !isValidUUIDv4(u.AuthToken) {
			oldLen := len(u.AuthToken)
			if _, err := m.ResetAuthToken(u.Username); err != nil {
				log.Warn("[migration] failed to reset auth_token", "user", u.Username, "err", err)
				continue
			}
			log.Debug("[migration] regenerated auth_token for user", "user", u.Username, "old_len", oldLen)
		}
	}

	return m, nil
}

// SetLoginPasswordHasher injects the function used to hash login passwords.
// Call this once after construction (e.g., in cmd/server.go) before serving requests.
func (m *UserManager) SetLoginPasswordHasher(fn func(string) (string, error)) {
	m.loginPasswordHasher = fn
}

// GetForwardManager returns the ForwardManager for port mapping operations.
// Returns nil if ForwardManager is not configured.
func (m *UserManager) GetForwardManager() forward.ForwardManager {
	return m.forwardMgr
}

// EnableClusterSync activates cluster synchronisation mode.
// When enabled, heartbeat exchanges carry user digests so that stale data is
// detected and pulled from peers. Version stamping itself is always active
// (handled by stampVersion), independent of this flag.
// Call once during server startup before serving requests.
func (m *UserManager) EnableClusterSync(defaultGroup string, ngs NodeGroupsStore) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.clusterEnabled = true
	m.defaultGroup = defaultGroup
	m.nodeGroupsStore = ngs

	if ngs != nil {
		groups, err := ngs.List()
		if err != nil {
			log.Warn("EnableClusterSync: failed to load node groups, filtering disabled", "err", err)
			return
		}
		m.cachedNodeGroups = makeGroupSet(groups)
	}
}

// SetNodeGroups atomically replaces the node's group assignments and refreshes
// the in-memory cache. Call this instead of nodeGroupsStore.Set() directly.
func (m *UserManager) SetNodeGroups(groups []string) error {
	m.mu.RLock()
	ngs := m.nodeGroupsStore
	m.mu.RUnlock()
	if ngs == nil {
		return fmt.Errorf("node groups store not configured")
	}
	if err := ngs.Set(groups); err != nil {
		return err
	}
	m.mu.Lock()
	m.cachedNodeGroups = makeGroupSet(groups)
	m.mu.Unlock()
	return nil
}

// makeGroupSet converts a slice of group names to a set for O(1) lookups.
func makeGroupSet(groups []string) map[string]struct{} {
	s := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		s[g] = struct{}{}
	}
	return s
}

// isUserVisibleLocked returns true if the user should be visible to local
// containers/inbounds. Must be called with mu held (read or write).
func (m *UserManager) isUserVisibleLocked(user *contracts.User) bool {
	if !m.clusterEnabled || len(m.cachedNodeGroups) == 0 {
		return true
	}
	if user.TargetGroup == "" {
		return true
	}
	_, ok := m.cachedNodeGroups[user.TargetGroup]
	return ok
}

// IsUserVisible reports whether the user should be visible on this node
// based on group membership. Safe to call concurrently.
func (m *UserManager) IsUserVisible(user *contracts.User) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isUserVisibleLocked(user)
}

// NodeGroupsStore returns the node groups store (nil if cluster sync is disabled).
func (m *UserManager) NodeGroupsStore() NodeGroupsStore {
	return m.nodeGroupsStore
}

// IsClusterEnabled returns true if cluster sync mode is active.
func (m *UserManager) IsClusterEnabled() bool {
	return m.clusterEnabled
}

// LocalNodeName returns the local node name used for version stamping.
func (m *UserManager) LocalNodeName() string {
	return m.localNodeName
}

// stampVersion stamps version fields (UpdatedAtUs, OriginNode, Hash) on the user
// after any local mutation. Called unconditionally regardless of cluster mode.
// SyncUpsertUser does NOT call this — it adopts remote versions directly.
func (m *UserManager) stampVersion(user *contracts.User) {
	user.UpdatedAtUs = time.Now().UnixMicro()
	user.OriginNode = m.localNodeName
	user.Hash = usync.ComputeHash(user)
}

// errMutateSkip is a sentinel returned by a mutateUser closure to signal that
// the mutation should be silently abandoned (no stamp, no persist, no event).
// mutateUser returns nil to the caller when it sees this sentinel.
var errMutateSkip = fmt.Errorf("mutate: skip")

// mutateUser is the single mutation path for existing users. Every method that
// modifies a user's synced fields MUST go through this function, which
// guarantees the sequence: lock → lookup → mutate → stampVersion → persist →
// emit event.
//
// The closure may return errMutateSkip to abort without error (useful for
// idempotent operations like RemoveUser). Any other non-nil error is returned
// as-is and no stamp/persist/emit happens.
//
// Pre-mutation work that might fail (e.g. loginPassword hashing) should be
// done BEFORE calling mutateUser so that a failure never leaves the user in
// an inconsistent state.
//
// SyncUpsertUser does NOT use this — it adopts remote versions directly.
func (m *UserManager) mutateUser(username string, eventType UserEventType, mutate func(*contracts.User) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[username]
	if !exists {
		return errors.New(errors.ErrUserNotFound, fmt.Sprintf("user %s not found", username))
	}

	if err := mutate(user); err != nil {
		if err == errMutateSkip {
			return nil
		}
		return err
	}

	m.stampVersion(user)

	if m.store != nil {
		if err := m.store.Save(user); err != nil {
			return fmt.Errorf("mutateUser(%s): persist: %w", username, err)
		}
	}

	evt := UserEvent{Type: eventType, Username: username}
	if eventType == UserEventRemove {
		evt.Ports = user.BindPorts
	} else {
		evt.User = user
	}
	m.emitEvent(evt)
	return nil
}

// UserEventChannel returns a read-only channel for receiving user events.
// The channel must be read continuously to prevent blocking.
// The returned channel is owned by the UserManager and must not be closed by callers.
// Deprecated: use Subscribe() for multi-consumer pub/sub.
func (m *UserManager) UserEventChannel() <-chan UserEvent {
	m.eventChMutex.Lock()
	defer m.eventChMutex.Unlock()
	return m.eventCh
}

// Subscribe creates a new subscriber channel for receiving user events.
// Each subscriber receives a copy of every event emitted by the UserManager.
// The channel must be read continuously to prevent blocking.
// Call Unsubscribe() to stop receiving events and free resources.
func (m *UserManager) Subscribe() <-chan UserEvent {
	m.eventChMutex.Lock()
	defer m.eventChMutex.Unlock()

	ch := make(chan UserEvent, 100)
	m.subscribers = append(m.subscribers, ch)
	return ch
}

// Unsubscribe removes a subscriber channel from the event distribution list.
// The channel will no longer receive events after this call.
// It is safe to call Unsubscribe multiple times.
func (m *UserManager) Unsubscribe(ch <-chan UserEvent) {
	m.eventChMutex.Lock()
	defer m.eventChMutex.Unlock()

	for i, sub := range m.subscribers {
		if sub == ch {
			// Remove from slice, maintain order
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			return
		}
	}
}

// UserPortMapper provides an interface to query user-bound ports.
// This is used by subscription generation to get the forward port for a user.
type UserPortMapper interface {
	// GetUserPort returns the forward port bound to the user.
	// Returns (0, false) if no port is bound.
	GetUserPort(username string) (uint32, bool)
}

// emitEvent sends a user event to all subscribers (legacy + pub/sub).
// Uses eventChMutex (not m.mu) to avoid deadlock when called from methods
// that already hold m.mu (e.g., AddUser, RemoveUser).
func (m *UserManager) emitEvent(event UserEvent) {
	m.eventChMutex.Lock()
	defer m.eventChMutex.Unlock()

	if m.closed {
		return
	}

	// Send to legacy channel (backward compatibility)
	select {
	case m.eventCh <- event:
		// Event sent successfully
	default:
		// Channel full, event dropped (non-blocking)
	}

	// Broadcast to all pub/sub subscribers
	for _, sub := range m.subscribers {
		select {
		case sub <- event:
		default:
			// Subscriber channel full, event dropped
		}
	}
}

// AddUserRequest contains parameters for adding a user.
type AddUserRequest struct {
	Username string
	Password string
	TTL      time.Duration // 0 means no expiration
}

// AddUser adds a new user.
func (m *UserManager) AddUser(req AddUserRequest) error {
	if err := m.validateUserCredentials(req.Username, req.Password); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if user exists (including deleting state)
	if existing, exists := m.users[req.Username]; exists {
		if existing.IsDeleting() {
			// User is tombstoned. Allow re-add only if all forward rules
			// have been cleaned up — otherwise the old inbound state would
			// conflict with the new user object.
			if m.forwardMgr != nil {
				if rules := m.forwardMgr.GetRulesByUser(req.Username); len(rules) > 0 {
					return errors.New(errors.ErrUserDeletionInProgress,
						fmt.Sprintf("user %s is being deleted, still has %d forward rules", req.Username, len(rules)))
				}
			}
			// No remaining rules — fall through to rebuild the user object
			// below. The old entry in m.users will be overwritten.
		} else {
			return errors.New(errors.ErrUserAlreadyExists, fmt.Sprintf("user %s already exists", req.Username))
		}
	}

	user := &contracts.User{
		Username:  req.Username,
		AuthToken: m.generateUniqueAuthTokenLocked(),
	}

	if req.TTL > 0 {
		user.ExpiryTime = time.Now().Add(req.TTL)
	}

	// Assign default group if configured (set via EnableClusterSync).
	if user.TargetGroup == "" && m.defaultGroup != "" {
		user.TargetGroup = m.defaultGroup
	}
	m.stampVersion(user)

	// Generate login_password so the user can log in immediately via /login.
	// A hasher failure is fatal: returning without a login_password would leave the
	// user unable to authenticate via the frontend.
	if m.loginPasswordHasher != nil {
		lp, err := m.loginPasswordHasher(req.Password)
		if err != nil {
			return fmt.Errorf("AddUser: hash login_password: %w", err)
		}
		user.LoginPassword = lp
	}

	if m.store != nil {
		if err := m.store.Save(user); err != nil {
			return fmt.Errorf("AddUser: persist: %w", err)
		}
	}

	m.users[req.Username] = user

	// Emit add event
	m.emitEvent(UserEvent{
		Type:     UserEventAdd,
		Username: req.Username,
		User:     user,
	})

	return nil
}

// RemoveUser marks a user for deletion (tombstone).
// The user stays in memory and store — it is never physically deleted.
// Containers react to the UserEventRemove to clean up inbound/forward rules.
// A subsequent AddUser with the same username is allowed once all forward
// rules have been cleaned up, at which point the tombstone is replaced by
// a fresh user object.
// This method is idempotent.
func (m *UserManager) RemoveUser(req RemoveUserRequest) error {
	err := m.mutateUser(req.Username, UserEventRemove, func(user *contracts.User) error {
		if user.IsDeleting() {
			return errMutateSkip // idempotent
		}
		user.MarkDeleting()
		return nil
	})
	// RemoveUser is idempotent: not-found is not an error.
	if errors.HasCode(err, errors.ErrUserNotFound) {
		return nil
	}
	return err
}

// cleanupUserPorts cleans up all ports bound to a user.
func (m *UserManager) cleanupUserPorts(username string, bindPorts []uint32) {
	if m.forwardMgr == nil {
		return
	}

	// Use ForwardManager's built-in method to remove all rules for this user
	// This handles the rule key format internally
	m.forwardMgr.RemoveRulesByUser(username)

	// Note: Port release is handled by ForwardManager internally
}

// RemoveUserRequest contains parameters for removing a user.
type RemoveUserRequest struct {
	Username string
}

// GetUser returns a user by username.
// It filters out users that are marked for deletion.
// Note: GetUser intentionally does NOT apply group filtering — it is used for
// admin operations and RPC profile lookups where the caller already knows the
// username. Group filtering only applies to enumeration methods (ListUsers, ListUsersWithPasswd).
func (m *UserManager) GetUser(username string) (*contracts.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.users[username]
	if !exists {
		return nil, errors.New(errors.ErrUserNotFound, fmt.Sprintf("user %s not found", username))
	}

	// Filter out deleting users
	if user.IsDeleting() {
		return nil, errors.New(errors.ErrUserNotFound, fmt.Sprintf("user %s not found", username))
	}

	// Check expiration
	if user.IsExpired() {
		return nil, errors.New(errors.ErrUserExpired, fmt.Sprintf("user %s has expired", username))
	}

	return user, nil
}

// FindUserByToken looks up an active user by their auth token.
// Returns nil if no matching user is found.
func (m *UserManager) FindUserByToken(token string) *contracts.User {
	if token == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, user := range m.users {
		if user.AuthToken == token && !user.IsDeleting() && !user.IsExpired() {
			return user
		}
	}
	return nil
}

// GetUserPort returns the first bound port for the user.
// Implements UserPortMapper interface.
func (m *UserManager) GetUserPort(username string) (uint32, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.users[username]
	if !exists || user.IsDeleting() || len(user.BindPorts) == 0 {
		return 0, false
	}
	return user.BindPorts[0], true
}

// GetUserPortByDst returns the forward port for a user given a dstPort (inbound backend listen port).
// Returns (forwardPort, true) if found, otherwise (0, false).
func (m *UserManager) GetUserPortByDst(username string, dstPort uint32) (uint32, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.users[username]
	if !exists || user.IsDeleting() {
		return 0, false
	}
	if user.PortMappings == nil {
		return 0, false
	}
	port, ok := user.PortMappings[dstPort]
	if !ok {
		return 0, false
	}
	return port, true
}

// GetUserPortByDstForCleanup returns the forward port for a user given a dstPort,
// including users marked for deletion. This is used by inbound cleanup paths
// (e.g. XrayInbound.RemoveUser, Snell UserEventRemove) that need to look up
// port mappings for users in the "deleting" state so that ReleaseBindPort can
// be called and the two-phase deletion can complete.
func (m *UserManager) GetUserPortByDstForCleanup(username string, dstPort uint32) (uint32, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.users[username]
	if !exists {
		return 0, false
	}
	if user.PortMappings == nil {
		return 0, false
	}
	port, ok := user.PortMappings[dstPort]
	if !ok {
		return 0, false
	}
	return port, true
}

// GetUserIncludingDeleting returns a user by username, including users marked for deletion.
// This is an internal method used for cleanup and error reporting.
// Returns nil if user doesn't exist at all.
func (m *UserManager) GetUserIncludingDeleting(username string) *contracts.User {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.users[username]
	if !exists {
		return nil
	}
	return user
}

// ListUsers returns all active (non-deleting, non-expired) users visible on this node.
// When cluster sync is enabled, only users whose TargetGroup matches the node's groups are returned.
func (m *UserManager) ListUsers() []*contracts.User {
	m.mu.RLock()
	defer m.mu.RUnlock()

	users := make([]*contracts.User, 0, len(m.users))
	for _, user := range m.users {
		if user.IsDeleting() || user.IsExpired() {
			continue
		}
		if !m.isUserVisibleLocked(user) {
			continue
		}
		users = append(users, user)
	}
	return users
}

// ListAllUsers returns all active (non-deleting, non-expired) users regardless of
// group visibility. This is intended for admin views that need the full cluster user list.
func (m *UserManager) ListAllUsers() []*contracts.User {
	m.mu.RLock()
	defer m.mu.RUnlock()

	users := make([]*contracts.User, 0, len(m.users))
	for _, user := range m.users {
		if user.IsDeleting() || user.IsExpired() {
			continue
		}
		users = append(users, user)
	}
	return users
}

// ListUsersWithPasswd returns a map of username -> password for all active users.
// Implements http.UserLister for Hysteria2 authentication.
func (m *UserManager) ListUsersWithPasswd() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]string, len(m.users))
	for _, user := range m.users {
		if user.IsDeleting() || user.IsExpired() {
			continue
		}
		if !m.isUserVisibleLocked(user) {
			continue
		}
		result[user.Username] = user.AuthToken
	}
	return result
}

// SetUserRole updates the role of an existing user and persists the change.
func (m *UserManager) SetUserRole(username, role string) error {
	return m.mutateUser(username, UserEventUpdate, func(user *contracts.User) error {
		user.Role = role
		return nil
	})
}

// SetUserGroup updates the cluster group of an existing user and persists the change.
func (m *UserManager) SetUserGroup(username, group string) error {
	return m.mutateUser(username, UserEventUpdate, func(user *contracts.User) error {
		user.TargetGroup = group
		return nil
	})
}

// UpdateUser updates a user's password and/or expiry time.
// expireTime is a Unix timestamp; 0 means no change to expiry.
// If password is non-empty, it is updated. If expireTime > 0, ExpiryTime is set.
func (m *UserManager) UpdateUser(username, password string, expireTime int64) error {
	if username == "" {
		return errors.New(errors.ErrInvalidUserSpec, "username cannot be empty")
	}

	// Pre-mutation: compute login_password hash before taking the lock so that
	// a hasher failure never leaves the user in an inconsistent state.
	var newLoginPassword string
	if password != "" && m.loginPasswordHasher != nil {
		lp, err := m.loginPasswordHasher(password)
		if err != nil {
			return fmt.Errorf("UpdateUser: hash login_password: %w", err)
		}
		newLoginPassword = lp
	}

	return m.mutateUser(username, UserEventUpdate, func(user *contracts.User) error {
		if password != "" {
			// Password update only changes LoginPassword (bcrypt hash).
			// AuthToken is independent and unchanged.
			if newLoginPassword != "" {
				user.LoginPassword = newLoginPassword
			}
		}
		if expireTime > 0 {
			user.ExpiryTime = time.Unix(expireTime, 0)
		}
		return nil
	})
}

// UpdateUserPassword updates a user's password.
type UpdateUserPasswordRequest struct {
	Username    string
	NewPassword string
}

// UpdateUserPassword updates a user's password.
func (m *UserManager) UpdateUserPassword(req UpdateUserPasswordRequest) error {
	if req.NewPassword == "" {
		return errors.New(errors.ErrInvalidUserSpec, "password cannot be empty")
	}

	// Pre-mutation: hash login_password before taking the lock.
	var newLoginPassword string
	if m.loginPasswordHasher != nil {
		lp, err := m.loginPasswordHasher(req.NewPassword)
		if err != nil {
			return fmt.Errorf("UpdateUserPassword: hash login_password: %w", err)
		}
		newLoginPassword = lp
	}

	return m.mutateUser(req.Username, UserEventUpdate, func(user *contracts.User) error {
		// Password update only changes LoginPassword (bcrypt hash).
		// AuthToken is independent and unchanged.
		if newLoginPassword != "" {
			user.LoginPassword = newLoginPassword
		}
		return nil
	})
}

// GetBindPortRequest contains parameters for getting a bind port.
type GetBindPortRequest struct {
	Username      string
	TargetPort    uint32           // The target port to forward to
	ContainerType contracts.ContainerType
	InboundTag    string
	Protocol      contracts.Protocol
}

// GetBindPort returns a bound port for the user.
// ForwardManager handles port allocation internally.
// If the user already has a usable port that targets the same port, returns it.
// Otherwise, allocates a new port through ForwardManager.
func (m *UserManager) GetBindPort(req GetBindPortRequest) (uint32, error) {
	// Validate required parameters
	if req.ContainerType == "" {
		return 0, errors.New(errors.ErrInvalidUserSpec, "container type is required")
	}
	if req.InboundTag == "" {
		return 0, errors.New(errors.ErrInvalidUserSpec, "inbound tag is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[req.Username]
	if !exists {
		return 0, errors.New(errors.ErrUserNotFound, fmt.Sprintf("user %s not found", req.Username))
	}

	// Check expiration
	if user.IsExpired() {
		return 0, errors.New(errors.ErrUserExpired, fmt.Sprintf("user %s has expired", req.Username))
	}

	// ForwardManager handles port allocation - just pass target info
	if m.forwardMgr == nil {
		return 0, errors.New(errors.ErrInternal, "forward manager not available")
	}

	// Sync user bandwidth limits to ForwardManager before creating rule
	// This ensures the rule uses the correct user-level bandwidth limits
	if user.BandwidthUploadBps > 0 {
		_ = m.forwardMgr.SetUserBandwidthLimit(req.Username, forward.BandwidthUpload, user.BandwidthUploadBps)
	}
	if user.BandwidthDownloadBps > 0 {
		_ = m.forwardMgr.SetUserBandwidthLimit(req.Username, forward.BandwidthDownload, user.BandwidthDownloadBps)
	}

	// Check if we have a recorded port mapping for this target port (dstPort)
	// Key is now dstPort (inbound backend port), not inbound tag
	var preferredPort uint32
	if user.PortMappings != nil {
		if port, ok := user.PortMappings[req.TargetPort]; ok {
			preferredPort = port
			log.Debug("[GetBindPort] using preferred port from PortMappings", "user", req.Username, "targetPort", req.TargetPort, "preferredPort", port)
		} else {
			log.Debug("[GetBindPort] no port mapping found, will allocate new port", "user", req.Username, "targetPort", req.TargetPort)
		}
	} else {
		log.Debug("[GetBindPort] user has nil PortMappings", "user", req.Username)
	}

	// Check if the forward rule already exists in ForwardManager (relay is running).
	// If it does, return the existing port directly — no need to create a new rule.
	ruleKey := string(req.ContainerType) + ":" + req.InboundTag + ":" + req.Username
	if existingRule := m.forwardMgr.GetRule(ruleKey); existingRule != nil {
		log.Debug("[GetBindPort] forward rule already exists, returning existing port", "user", req.Username, "port", existingRule.ListenPort)
		return existingRule.ListenPort, nil
	}

	// Create forward rule - use preferredPort if available (0 = auto allocate)
	rule := forward.ForwardRule{
		Username:              req.Username,
		ContainerType:         req.ContainerType,
		InboundTag:            req.InboundTag,
		Protocol:              req.Protocol,
		ListenPort:            preferredPort, // 0 = auto allocate, >0 = specific port
		TargetAddr:            fmt.Sprintf("127.0.0.1:%d", req.TargetPort),
		MaxClients:            user.MaxClients,
		ClientRecycleDelaySec: user.ClientRecycleDelaySec,
		ClientDrainSec:        user.ClientDrainSec,
	}

	createdRule, err := m.forwardMgr.AddRule(rule)
	if err != nil {
		// If preferredPort was set but failed (port occupied by another process),
		// retry with auto-allocate
		if preferredPort != 0 {
			log.Warn("[GetBindPort] preferred port unavailable, reallocating", "preferredPort", preferredPort, "user", req.Username)
			rule.ListenPort = 0
			createdRule, err = m.forwardMgr.AddRule(rule)
			if err != nil {
				return 0, errors.Wrap(errors.ErrInternal, "failed to add forward rule", err)
			}
		} else {
			return 0, errors.Wrap(errors.ErrInternal, "failed to add forward rule", err)
		}
	}

	// Update user's bind ports and port mappings
	// Key is now dstPort (req.TargetPort), value is the allocated forwardPort
	user.BindPorts = append(user.BindPorts, createdRule.ListenPort)
	if user.PortMappings == nil {
		user.PortMappings = make(map[uint32]uint32)
	}
	user.PortMappings[req.TargetPort] = createdRule.ListenPort

	// Persist port mappings
	if m.store != nil {
		if err := m.store.Save(user); err != nil {
			log.Warn("GetBindPort: failed to persist port_mappings", "err", err)
		}
	}

	// Emit port bind event
	m.emitEvent(UserEvent{
		Type:     UserEventPortBind,
		Username: req.Username,
		User:     user,
		Ports:    []uint32{createdRule.ListenPort},
	})

	return createdRule.ListenPort, nil
}

// ReleaseBindPort releases a user's bound port.
type ReleaseBindPortRequest struct {
	Username string
	BindPort uint32
}

// ReleaseBindPort releases a user's bound port.
// This method is idempotent - calling multiple times will not cause errors.
// After releasing the port, if the user is in "deleting" state and has no more
// forward rules, it will physically delete the user (finalize).

// ResetAuthToken regenerates the user's auth token with a new UUID.
func (m *UserManager) ResetAuthToken(username string) (string, error) {
	var newToken string
	err := m.mutateUser(username, UserEventUpdate, func(user *contracts.User) error {
		newToken = m.generateUniqueAuthTokenLocked()
		user.AuthToken = newToken
		return nil
	})
	return newToken, err
}

// RotateUserPortForInbound allocates a new forward port for a specific container+inbound
// of a user, then releases the old port. If preferredPort > 0, tries that port first.
// Returns the newly allocated listen port.
//
// Flow: make (new relay) → break (old relay) → update state.
// The old port remains active until the new one is confirmed listening.
func (m *UserManager) RotateUserPortForInbound(username string, containerType contracts.ContainerType, inboundTag string, preferredPort uint32) (uint32, error) {
	if username == "" {
		return 0, errors.New(errors.ErrInvalidUserSpec, "username is required")
	}
	if containerType == "" {
		return 0, errors.New(errors.ErrInvalidUserSpec, "container type is required")
	}
	if inboundTag == "" {
		return 0, errors.New(errors.ErrInvalidUserSpec, "inbound tag is required")
	}
	if m.forwardMgr == nil {
		return 0, errors.New(errors.ErrInternal, "forward manager not available")
	}

	// Step 1: snapshot existing rule fields needed to rebuild the relay
	ruleKey := string(containerType) + ":" + inboundTag + ":" + username
	existingRule := m.forwardMgr.GetRule(ruleKey)
	if existingRule == nil {
		return 0, errors.New(errors.ErrInvalidUserSpec, fmt.Sprintf("no forward rule found for user %s, container %s, inbound %s", username, containerType, inboundTag))
	}
	var targetPort uint32
	if _, err := fmt.Sscanf(existingRule.TargetAddr, "127.0.0.1:%d", &targetPort); err != nil || targetPort == 0 {
		return 0, errors.New(errors.ErrInternal, fmt.Sprintf("could not parse target port from rule TargetAddr=%s", existingRule.TargetAddr))
	}
	oldListenPort := existingRule.ListenPort

	// Step 2: MAKE — start new relay under a temporary inbound tag to avoid key conflict.
	// Using a temp tag gives a different RuleKey while pointing to the same TargetAddr.
	// The new port is confirmed bindable before we touch the old relay.
	tmpTag := inboundTag + ":_r"
	tmpRuleKey := string(containerType) + ":" + tmpTag + ":" + username
	tmpRule := forward.ForwardRule{
		Username:              username,
		ContainerType:         containerType,
		InboundTag:            tmpTag,
		Protocol:              existingRule.Protocol,
		ListenAddr:            existingRule.ListenAddr,
		ListenPort:            preferredPort, // 0 = auto-allocate
		TargetAddr:            existingRule.TargetAddr,
		UploadBytesPerSec:     existingRule.UploadBytesPerSec,
		DownloadBytesPerSec:   existingRule.DownloadBytesPerSec,
		MaxConnections:        existingRule.MaxConnections,
		MaxClients:            existingRule.MaxClients,
		ClientRecycleDelaySec: existingRule.ClientRecycleDelaySec,
		ClientDrainSec:        existingRule.ClientDrainSec,
	}
	createdTmpRule, err := m.forwardMgr.AddRule(tmpRule)
	if err != nil {
		return 0, errors.Wrap(errors.ErrInternal, "failed to allocate new forward port", err)
	}
	newPort := createdTmpRule.ListenPort

	// Step 3: BREAK — remove old relay only after new one is confirmed up
	if removeErr := m.forwardMgr.RemoveRule(ruleKey); removeErr != nil {
		// Old rule removal failed — clean up temp relay and bail; old port stays intact
		_ = m.forwardMgr.RemoveRule(tmpRuleKey)
		return 0, errors.Wrap(errors.ErrInternal, "failed to remove old forward rule", removeErr)
	}

	// Step 4: replace temp relay with the canonical one under the proper rule key.
	// Remove temp first, then re-add with the real inbound tag and the exact new port.
	_ = m.forwardMgr.RemoveRule(tmpRuleKey)
	finalRule := forward.ForwardRule{
		Username:              username,
		ContainerType:         containerType,
		InboundTag:            inboundTag,
		Protocol:              existingRule.Protocol,
		ListenAddr:            existingRule.ListenAddr,
		ListenPort:            newPort, // reclaim the same port (just freed above)
		TargetAddr:            existingRule.TargetAddr,
		UploadBytesPerSec:     existingRule.UploadBytesPerSec,
		DownloadBytesPerSec:   existingRule.DownloadBytesPerSec,
		MaxConnections:        existingRule.MaxConnections,
		MaxClients:            existingRule.MaxClients,
		ClientRecycleDelaySec: existingRule.ClientRecycleDelaySec,
		ClientDrainSec:        existingRule.ClientDrainSec,
	}
	createdFinalRule, err := m.forwardMgr.AddRule(finalRule)
	if err != nil {
		// Port grabbed in the tiny window — fall back to auto-allocate
		log.Warn("[RotateUserPortForInbound] port reclaim failed, auto-allocating", "user", username, "port", newPort, "err", err)
		finalRule.ListenPort = 0
		createdFinalRule, err = m.forwardMgr.AddRule(finalRule)
		if err != nil {
			return 0, errors.Wrap(errors.ErrInternal, "failed to finalize forward rule", err)
		}
		newPort = createdFinalRule.ListenPort
	}

	// Step 5: update user state now that the new relay is confirmed
	m.mu.Lock()
	user, exists := m.users[username]
	if !exists {
		m.mu.Unlock()
		_ = m.forwardMgr.RemoveRule(createdFinalRule.RuleKey())
		return 0, errors.New(errors.ErrUserNotFound, fmt.Sprintf("user %s not found", username))
	}
	newBindPorts := make([]uint32, 0, len(user.BindPorts))
	for _, p := range user.BindPorts {
		if p != oldListenPort {
			newBindPorts = append(newBindPorts, p)
		}
	}
	user.BindPorts = append(newBindPorts, newPort)
	if user.PortMappings == nil {
		user.PortMappings = make(map[uint32]uint32)
	}
	user.PortMappings[targetPort] = newPort
	if m.store != nil {
		if err := m.store.Save(user); err != nil {
			log.Warn("[RotateUserPortForInbound] failed to persist port_mappings", "err", err)
		}
	}
	m.mu.Unlock()

	m.emitEvent(UserEvent{
		Type:     UserEventPortBind,
		Username: username,
		User:     user,
		Ports:    []uint32{newPort},
	})

	log.Info("[RotateUserPortForInbound] port rotated", "user", username, "container", containerType, "inbound", inboundTag, "oldPort", oldListenPort, "newPort", newPort)
	return newPort, nil
}

// RotateAllUserPorts rotates the forward port for every inbound of a user using
// make-before-break semantics (same as RotateUserPortForInbound).
// Returns a map of inbound tag → new listen port for all successfully rotated rules.
// If any single inbound fails, the error is collected and rotation continues for the rest.
// Returns a combined error if any rotation failed.
func (m *UserManager) RotateAllUserPorts(username string) (map[string]uint32, error) {
	if username == "" {
		return nil, errors.New(errors.ErrInvalidUserSpec, "username is required")
	}
	if m.forwardMgr == nil {
		return nil, errors.New(errors.ErrInternal, "forward manager not available")
	}

	// Snapshot all current rules for this user
	rules := m.forwardMgr.GetRulesByUser(username)
	if len(rules) == 0 {
		return nil, errors.New(errors.ErrInvalidUserSpec, fmt.Sprintf("no forward rules found for user %s", username))
	}

	result := make(map[string]uint32, len(rules))
	var firstErr error
	for _, rule := range rules {
		newPort, err := m.RotateUserPortForInbound(username, rule.ContainerType, rule.InboundTag, 0)
		// Key format: "container:inboundTag" — inboundTag is only unique within a
		// container, so the container prefix is required to avoid collisions.
		key := string(rule.ContainerType) + ":" + rule.InboundTag
		if err != nil {
			log.Error("[RotateAllUserPorts] failed to rotate inbound", "user", username, "inbound", rule.InboundTag, "err", err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		result[key] = newPort
	}

	log.Info("[RotateAllUserPorts] rotation complete", "user", username, "total", len(rules), "success", len(result))
	return result, firstErr
}

// SetUserBandwidthLimit sets the bandwidth limit for a user.
// It updates the user's bandwidth configuration and syncs to ForwardManager.
func (m *UserManager) SetUserBandwidthLimit(username string, kind forward.BandwidthLimitKind, bytesPerSec int64) error {
	if username == "" {
		return errors.New(errors.ErrInvalidUserSpec, "username is required")
	}
	if err := forward.ValidateBandwidthLimitKind(kind); err != nil {
		return err
	}
	if bytesPerSec < 0 {
		return errors.New(errors.ErrInvalidUserSpec, "bandwidth limit must not be negative")
	}

	err := m.mutateUser(username, UserEventUpdate, func(user *contracts.User) error {
		if kind == forward.BandwidthUpload {
			user.BandwidthUploadBps = bytesPerSec
		} else {
			user.BandwidthDownloadBps = bytesPerSec
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Sync to ForwardManager (outside mutateUser since it doesn't need stamp/persist)
	if m.forwardMgr != nil {
		_ = m.forwardMgr.SetUserBandwidthLimit(username, kind, bytesPerSec)
	}

	return nil
}

// GetUserBandwidthLimit returns the bandwidth limit for a user.
func (m *UserManager) GetUserBandwidthLimit(username string, kind forward.BandwidthLimitKind) (int64, bool) {
	if err := forward.ValidateBandwidthLimitKind(kind); err != nil {
		return 0, false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.users[username]
	if !exists {
		return 0, false
	}

	if kind == forward.BandwidthUpload {
		if user.BandwidthUploadBps == 0 {
			return 0, false
		}
		return user.BandwidthUploadBps, true
	}

	if user.BandwidthDownloadBps == 0 {
		return 0, false
	}
	return user.BandwidthDownloadBps, true
}

// SetUserClientLimit sets the client limit configuration for a user.
func (m *UserManager) SetUserClientLimit(username string, maxClients, recycleDelaySec, drainSec int) error {
	err := m.mutateUser(username, UserEventUpdate, func(user *contracts.User) error {
		user.MaxClients = maxClients
		user.ClientRecycleDelaySec = recycleDelaySec
		user.ClientDrainSec = drainSec
		return nil
	})
	if err != nil {
		return err
	}

	// Sync to ForwardManager (outside mutateUser since it doesn't need stamp/persist)
	if m.forwardMgr != nil {
		_ = m.forwardMgr.SetUserClientLimitConfig(username, forward.ClientLimitConfig{
			MaxClients:              maxClients,
			RecycleDelaySec:        recycleDelaySec,
			SingleDirectionDrainSec: drainSec,
		})
	}

	return nil
}

// GetUserClientLimit returns the client limit configuration for a user.
func (m *UserManager) GetUserClientLimit(username string) (maxClients, recycleDelaySec, drainSec int, ok bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.users[username]
	if !exists {
		return 0, 0, 0, false
	}

	if user.MaxClients == 0 {
		return 0, 0, 0, false
	}

	return user.MaxClients, user.ClientRecycleDelaySec, user.ClientDrainSec, true
}

func (m *UserManager) ReleaseBindPort(req ReleaseBindPortRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[req.Username]
	if !exists {
		// User not found - idempotent
		return nil
	}

	// Find and remove the port from user's bind ports
	found := false
	newPorts := make([]uint32, 0, len(user.BindPorts))
	for _, port := range user.BindPorts {
		if port == req.BindPort {
			found = true
			continue
		}
		newPorts = append(newPorts, port)
	}

	if !found {
		// Port not bound to user - idempotent
		// Still check for finalize if user is deleting
	}

	// Update bind ports if we found and removed the port
	if found {
		user.BindPorts = newPorts
	}

	// Remove the PortMappings entry whose value matches req.BindPort,
	// so that GetUserPortByDst no longer resolves this user for the
	// released forward port (prevents stale subscription links).
	for dst, fwd := range user.PortMappings {
		if fwd == req.BindPort {
			delete(user.PortMappings, dst)
		}
	}

	// Emit port bind event for release
	m.emitEvent(UserEvent{
		Type:     UserEventPortBind,
		Username: req.Username,
		User:     user,
		Ports:    []uint32{req.BindPort},
	})

	// Remove forward rule via ForwardManager
	if m.forwardMgr != nil {
		m.forwardMgr.RemoveRulesByUser(req.Username)
	}

	// Persist updated PortMappings / BindPorts so restart doesn't resurrect stale mappings.
	if m.store != nil {
		if err := m.store.Save(user); err != nil {
			log.Warn("[ReleaseBindPort] failed to persist after cleanup", "user", req.Username, "err", err)
		}
	}

	return nil
}

// ReleaseInboundPorts releases all bound ports for a specific inbound tag.
// This is called when an inbound is stopped or destroyed.
// It removes forward rules for all users associated with this inbound.
func (m *UserManager) ReleaseInboundPorts(inboundTag string) error {
	if inboundTag == "" {
		return nil // idempotent
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Get all forward rules for this inbound via ForwardManager
	if m.forwardMgr == nil {
		return nil // idempotent
	}

	// Remove all rules for this inbound tag
	_ = m.forwardMgr.RemoveRulesByInbound(inboundTag)

	// Note: We don't modify user.BindPorts here because those ports
	// may be reused by the same user on different inbounds.
	// The ports will be cleaned up when users are removed or expired.

	return nil
}

// cleanupExpiredUsers marks expired users as deleting (tombstone).
// This follows the same deletion flow as RemoveUser — no physical delete.
// Caller must NOT hold m.mu (this method acquires it, and emitEvent may
// trigger container handlers that call back into UserManager).
func (m *UserManager) cleanupExpiredUsers() {
	// Collect expired usernames under lock, then process outside lock
	// to avoid holding mu during event emission / container callbacks.
	m.mu.Lock()
	var expired []string
	for username, user := range m.users {
		if user.IsExpired() && !user.IsDeleting() {
			expired = append(expired, username)
		}
	}
	m.mu.Unlock()

	for _, username := range expired {
		if err := m.RemoveUser(RemoveUserRequest{Username: username}); err != nil {
			log.Warn("cleanupExpiredUsers: RemoveUser failed", "user", username, "err", err)
		}
	}
}

// validateUserCredentials validates username and password.
func (m *UserManager) validateUserCredentials(username, password string) error {
	if username == "" {
		return errors.New(errors.ErrInvalidUserSpec, "username cannot be empty")
	}
	if password == "" {
		return errors.New(errors.ErrInvalidUserSpec, "password cannot be empty")
	}
	return nil
}

// AddUserForTest adds a user with specific username and bind ports (for testing only).
// This is a test helper that bypasses the normal user creation flow.
func (m *UserManager) AddUserForTest(username string, bindPorts []uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	user := &contracts.User{
		Username:  username,
		AuthToken: generateAuthToken(),
		BindPorts: bindPorts,
	}
	m.users[username] = user
	return nil
}

// SetPortMappingForTest sets a port mapping for a user (for testing only).
// dstPort is the inbound backend port, forwardPort is the user-facing port.
func (m *UserManager) SetPortMappingForTest(username string, dstPort, forwardPort uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[username]
	if !exists {
		return
	}
	if user.PortMappings == nil {
		user.PortMappings = make(map[uint32]uint32)
	}
	user.PortMappings[dstPort] = forwardPort
}

// ============ Cluster Sync Methods ============

// SyncUpsertUser receives a full user record from a remote node and applies it
// locally after version arbitration. It does NOT update OriginNode/UpdatedAtUs
// so that the originating node recognises its own version on the next heartbeat
// (preventing sync loops).
//
// Returns (true, nil) if the record was applied, (false, nil) if skipped.
func (m *UserManager) SyncUpsertUser(incoming *contracts.User) (bool, error) {
	if incoming == nil || incoming.Username == "" {
		return false, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists := m.users[incoming.Username]

	// If local copy exists, check version arbitration.
	if exists && !usync.IsNewer(incoming, existing) {
		// Even when the remote version is not newer, adopt LoginPassword
		// if local is empty and remote has one. This handles the case where
		// a node upgraded from an old version has a newer timestamp but no
		// password, while an older-timestamped node already has the password.
		if existing.LoginPassword == "" && incoming.LoginPassword != "" {
			existing.LoginPassword = incoming.LoginPassword
			if m.store != nil {
				_ = m.store.Save(existing)
			}
		}
		return false, nil
	}

	// Case 1: incoming is a tombstone (deleted).
	if incoming.IsDeleting() {
		if !exists {
			// Remote deleted a user we never had — just store the tombstone.
			m.users[incoming.Username] = incoming
			if m.store != nil {
				if err := m.store.Save(incoming); err != nil {
					return false, fmt.Errorf("SyncUpsertUser: persist tombstone: %w", err)
				}
			}
			return true, nil
		}
		// Local user exists and is not yet deleting — trigger local removal.
		if !existing.IsDeleting() {
			existing.MarkDeleting()
		}
		// Adopt remote version fields (do NOT re-stamp).
		existing.UpdatedAtUs = incoming.UpdatedAtUs
		existing.OriginNode = incoming.OriginNode
		existing.Hash = incoming.Hash
		existing.TargetGroup = incoming.TargetGroup

		if m.store != nil {
			if err := m.store.Save(existing); err != nil {
				return false, fmt.Errorf("SyncUpsertUser: persist tombstone: %w", err)
			}
		}

		// Emit remove event so containers clean up ports.
		m.emitEvent(UserEvent{
			Type:     UserEventRemove,
			Username: existing.Username,
			Ports:    existing.BindPorts,
		})
		return true, nil
	}

	// Case 2: incoming is an active user.
	if !exists {
		// New user from remote. Ensure auth token is UUID.
		if !isValidUUIDv4(incoming.AuthToken) {
			incoming.AuthToken = m.generateUniqueAuthTokenLocked()
			m.stampVersion(incoming)
		}
		m.users[incoming.Username] = incoming
		if m.store != nil {
			if err := m.store.Save(incoming); err != nil {
				return false, fmt.Errorf("SyncUpsertUser: persist new user: %w", err)
			}
		}
		m.emitEvent(UserEvent{
			Type:     UserEventAdd,
			Username: incoming.Username,
			User:     incoming,
		})
		return true, nil
	}

	// Existing user, incoming is newer — update fields.
	// Ensure auth token is UUID; if remote sends a legacy plaintext password, regenerate.
	if !isValidUUIDv4(incoming.AuthToken) {
		incoming.AuthToken = m.generateUniqueAuthTokenLocked()
	}
	existing.AuthToken = incoming.AuthToken
	existing.ExpiryTime = incoming.ExpiryTime
	existing.Role = incoming.Role
	existing.TargetGroup = incoming.TargetGroup
	existing.UpdatedAtUs = incoming.UpdatedAtUs
	existing.OriginNode = incoming.OriginNode
	existing.Hash = incoming.Hash
	existing.BandwidthUploadBps = incoming.BandwidthUploadBps
	existing.BandwidthDownloadBps = incoming.BandwidthDownloadBps
	existing.MaxClients = incoming.MaxClients
	existing.ClientRecycleDelaySec = incoming.ClientRecycleDelaySec
	existing.ClientDrainSec = incoming.ClientDrainSec
	// Only update LoginPassword if incoming has one (old nodes send empty).
	if incoming.LoginPassword != "" {
		existing.LoginPassword = incoming.LoginPassword
	}
	// Clear deletion state if the remote version is active.
	if existing.IsDeleting() {
		existing.MarkActive()
	}

	if m.store != nil {
		if err := m.store.Save(existing); err != nil {
			return false, fmt.Errorf("SyncUpsertUser: persist update: %w", err)
		}
	}
	m.emitEvent(UserEvent{
		Type:     UserEventUpdate,
		Username: existing.Username,
		User:     existing,
	})
	return true, nil
}

// ListDigests returns lightweight version summaries for all users (including
// tombstones) for heartbeat delta-sync.
func (m *UserManager) ListDigests() []usync.UserDigest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	digests := make([]usync.UserDigest, 0, len(m.users))
	for _, u := range m.users {
		digests = append(digests, usync.UserDigest{
			Username:    u.Username,
			UpdatedAtUs: u.UpdatedAtUs,
			OriginNode:  u.OriginNode,
			Hash:        u.Hash,
		})
	}
	return digests
}

// GetUserForSync returns a user by username including tombstones.
// Returns nil if the user does not exist.
func (m *UserManager) GetUserForSync(username string) *contracts.User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.users[username]
}

// BackfillClusterFields fills in UpdatedAtUs/OriginNode/Hash for users loaded
// from a database that predates cluster-sync support (UpdatedAtUs == 0).
func (m *UserManager) BackfillClusterFields() {
	m.mu.Lock()
	defer m.mu.Unlock()

	var count int
	for _, u := range m.users {
		if u.UpdatedAtUs != 0 {
			continue
		}
		if u.TargetGroup == "" && m.defaultGroup != "" {
			u.TargetGroup = m.defaultGroup
		}
		m.stampVersion(u)
		if m.store != nil {
			if err := m.store.Save(u); err != nil {
				log.Warn("BackfillClusterFields: persist failed", "user", u.Username, "err", err)
			}
		}
		count++
	}
	if count > 0 {
		log.Info("BackfillClusterFields: backfilled cluster fields", "count", count)
	}
}

// ============ Traffic Stats Collection (Forward-Only) ============

// TrafficStats holds traffic statistics data.
// Delta is the incremental traffic since the last reset (not persisted).
// Total is the cumulative traffic since the user was created (persisted to DB).
type TrafficStats struct {
	// Delta is the increment since the last GetDelta(reset=true) call.
	// Used by Prometheus scrape: each scrape resets the counter.
	DeltaUplink   int64 `json:"delta_uplink"`
	DeltaDownlink int64 `json:"delta_downlink"`
	// Total is the lifetime cumulative traffic, persisted to DB.
	TotalUplink   int64 `json:"total_uplink"`
	TotalDownlink int64 `json:"total_downlink"`
}


// UserTrafficStats holds traffic stats for a specific user.
type UserTrafficStats struct {
	ContainerType contracts.ContainerType `json:"container_type"`
	InboundTag    string                  `json:"inbound_tag"`
	Protocol      contracts.Protocol      `json:"protocol"`
	Username      string                   `json:"username"`
	TrafficStats
}

// InboundTrafficStats holds traffic stats for an inbound.
type InboundTrafficStats struct {
	ContainerType contracts.ContainerType `json:"container_type"`
	InboundTag    string                  `json:"inbound_tag"`
	TrafficStats
}

// ContainerTrafficStats holds traffic stats for a container.
type ContainerTrafficStats struct {
	ContainerType contracts.ContainerType `json:"container_type"`
	TrafficStats
}

// GlobalTrafficStats holds global traffic stats (aggregated).
type GlobalTrafficStats struct {
	Total TrafficStats `json:"total"`
}

// AggregatedStats holds all levels of traffic aggregation.
type AggregatedStats struct {
	ByUser      map[string]UserTrafficStats      `json:"by_user"`      // key: "container:inbound:user"
	ByInbound   map[string]InboundTrafficStats   `json:"by_inbound"`   // key: "container:inbound"
	ByContainer map[string]ContainerTrafficStats `json:"by_container"` // key: "container"
	Global      GlobalTrafficStats                `json:"global"`
}

// statsCollector manages traffic stats collection from forward layer.
// Each collect() cycle accumulates increments into Delta and Total.
// Delta can be reset independently (e.g. by Prometheus scrape).
// Total is never reset automatically; use ResetUserTotalTraffic to reset it.
type statsCollector struct {
	mu         sync.RWMutex
	forwardMgr forward.ForwardManager
	// stats holds the current aggregated view (Delta = since last per-user reset, Total = cumulative)
	stats      AggregatedStats
	// prevCounters tracks the last seen forward-layer counter values to compute increments
	prevCounters map[string]trafficCounter // key: "container:inbound:user"
	interval   time.Duration
	stopCh     chan struct{}
	running    bool
	// onCollect is called after each collect() cycle with a snapshot of ByUser stats.
	// Used by UserManager to persist TotalUplink/TotalDownlink.
	onCollect func(byUser map[string]UserTrafficStats)
}

// trafficCounter tracks raw counter values from the forward layer.
type trafficCounter struct {
	uplink   int64
	downlink int64
}

// newStatsCollector creates a new forward-only stats collector.
func newStatsCollector(fm forward.ForwardManager, interval time.Duration) *statsCollector {
	if interval == 0 {
		interval = time.Minute // Default 1 minute
	}
	return &statsCollector{
		forwardMgr: fm,
		stats: AggregatedStats{
			ByUser:      make(map[string]UserTrafficStats),
			ByInbound:   make(map[string]InboundTrafficStats),
			ByContainer: make(map[string]ContainerTrafficStats),
		},
		prevCounters: make(map[string]trafficCounter),
		interval:     interval,
		stopCh:       make(chan struct{}),
	}
}

// Start starts the periodic stats collection.
func (sc *statsCollector) Start() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.running {
		return
	}

	sc.running = true
	sc.stopCh = make(chan struct{})

	go sc.collectLoop()
}

// Stop stops the periodic stats collection.
func (sc *statsCollector) Stop() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if !sc.running {
		return
	}

	sc.running = false
	if sc.stopCh != nil {
		close(sc.stopCh)
		sc.stopCh = nil
	}
}

// collectLoop is the main stats collection loop.
func (sc *statsCollector) collectLoop() {
	ticker := time.NewTicker(sc.interval)
	defer ticker.Stop()

	// Perform initial collection
	sc.collect()

	for {
		select {
		case <-ticker.C:
			sc.collect()
		case <-sc.stopCh:
			return
		}
	}
}

// collect gathers stats from the forward layer and accumulates increments.
// For each record, it computes the delta since the last collection cycle and adds to:
//   - DeltaUplink/DeltaDownlink (per entry, reset on demand via GetUserDeltaTraffic)
//   - TotalUplink/TotalDownlink (lifetime cumulative, persisted to DB)
func (sc *statsCollector) collect() {
	sc.mu.RLock()
	fm := sc.forwardMgr
	sc.mu.RUnlock()

	if fm == nil {
		return
	}

	// Non-resetting read — we compute increments ourselves
	records := fm.GetAllTrafficRecords(false)

	sc.mu.Lock()
	defer sc.mu.Unlock()

	var globalDeltaUp, globalDeltaDown int64

	for _, record := range records {
		userKey := string(record.ContainerType) + ":" + record.InboundTag + ":" + record.Username

		// Compute increment since last collection
		prev := sc.prevCounters[userKey]
		deltaUp := record.UplinkBytes - prev.uplink
		deltaDown := record.DownlinkBytes - prev.downlink
		// Handle counter reset (e.g. process restart)
		if deltaUp < 0 {
			deltaUp = record.UplinkBytes
		}
		if deltaDown < 0 {
			deltaDown = record.DownlinkBytes
		}
		sc.prevCounters[userKey] = trafficCounter{uplink: record.UplinkBytes, downlink: record.DownlinkBytes}

		if deltaUp == 0 && deltaDown == 0 {
			continue
		}

		// Accumulate user-level
		existing := sc.stats.ByUser[userKey]
		existing.ContainerType = record.ContainerType
		existing.InboundTag = record.InboundTag
		existing.Protocol = record.Protocol
		existing.Username = record.Username
		existing.DeltaUplink += deltaUp
		existing.DeltaDownlink += deltaDown
		existing.TotalUplink += deltaUp
		existing.TotalDownlink += deltaDown
		sc.stats.ByUser[userKey] = existing

		// Accumulate inbound-level
		inboundKey := string(record.ContainerType) + ":" + record.InboundTag
		ib := sc.stats.ByInbound[inboundKey]
		ib.ContainerType = record.ContainerType
		ib.InboundTag = record.InboundTag
		ib.DeltaUplink += deltaUp
		ib.DeltaDownlink += deltaDown
		ib.TotalUplink += deltaUp
		ib.TotalDownlink += deltaDown
		sc.stats.ByInbound[inboundKey] = ib

		// Accumulate container-level
		containerKey := string(record.ContainerType)
		ct := sc.stats.ByContainer[containerKey]
		ct.ContainerType = record.ContainerType
		ct.DeltaUplink += deltaUp
		ct.DeltaDownlink += deltaDown
		ct.TotalUplink += deltaUp
		ct.TotalDownlink += deltaDown
		sc.stats.ByContainer[containerKey] = ct

		globalDeltaUp += deltaUp
		globalDeltaDown += deltaDown
	}

	// Accumulate global
	sc.stats.Global.Total.DeltaUplink += globalDeltaUp
	sc.stats.Global.Total.DeltaDownlink += globalDeltaDown
	sc.stats.Global.Total.TotalUplink += globalDeltaUp
	sc.stats.Global.Total.TotalDownlink += globalDeltaDown

	// Notify caller to persist Total values
	if sc.onCollect != nil && (globalDeltaUp > 0 || globalDeltaDown > 0) {
		snapshot := make(map[string]UserTrafficStats, len(sc.stats.ByUser))
		for k, v := range sc.stats.ByUser {
			snapshot[k] = v
		}
		go sc.onCollect(snapshot)
	}
}


// GetStats returns the current aggregated stats.
func (sc *statsCollector) GetStats() AggregatedStats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.stats
}

// GetUserStats returns stats for a specific user.
func (sc *statsCollector) GetUserStats(username string) (*UserTrafficStats, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	// Search through all user stats
	for _, stats := range sc.stats.ByUser {
		if stats.Username == username {
			return &stats, true
		}
	}
	return nil, false
}

// GetInboundStats returns stats for a specific inbound.
func (sc *statsCollector) GetInboundStats(ct contracts.ContainerType, inboundTag string) (*InboundTrafficStats, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", ct, inboundTag)
	stats, ok := sc.stats.ByInbound[key]
	return &stats, ok
}

// GetContainerStats returns stats for a specific container.
func (sc *statsCollector) GetContainerStats(ct contracts.ContainerType) (*ContainerTrafficStats, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	key := string(ct)
	stats, ok := sc.stats.ByContainer[key]
	return &stats, ok
}

// GetGlobalStats returns global traffic stats.
func (sc *statsCollector) GetGlobalStats() GlobalTrafficStats {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.stats.Global
}

// ForceCollect forces an immediate stats collection.
func (sc *statsCollector) ForceCollect() {
	go sc.collect()
}

// resetUserDelta zeroes the DeltaUplink/DeltaDownlink for a specific user across all keys.
func (sc *statsCollector) resetUserDelta(username string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for key, v := range sc.stats.ByUser {
		if v.Username == username {
			v.DeltaUplink = 0
			v.DeltaDownlink = 0
			sc.stats.ByUser[key] = v
		}
	}
}

// resetAllDeltas zeroes DeltaUplink/DeltaDownlink for all entries.
func (sc *statsCollector) resetAllDeltas() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for k, v := range sc.stats.ByUser {
		v.DeltaUplink = 0
		v.DeltaDownlink = 0
		sc.stats.ByUser[k] = v
	}
	for k, v := range sc.stats.ByInbound {
		v.DeltaUplink = 0
		v.DeltaDownlink = 0
		sc.stats.ByInbound[k] = v
	}
	for k, v := range sc.stats.ByContainer {
		v.DeltaUplink = 0
		v.DeltaDownlink = 0
		sc.stats.ByContainer[k] = v
	}
	sc.stats.Global.Total.DeltaUplink = 0
	sc.stats.Global.Total.DeltaDownlink = 0
}

// resetUserTotal zeroes TotalUplink/TotalDownlink for a specific user.
func (sc *statsCollector) resetUserTotal(username string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for key, v := range sc.stats.ByUser {
		if v.Username == username {
			v.TotalUplink = 0
			v.TotalDownlink = 0
			sc.stats.ByUser[key] = v
		}
	}
}

// ============ UserManager Traffic Stats API ============

// StartTrafficStats starts periodic traffic stats collection from forward layer.
// interval specifies the collection interval (default 1 minute if 0).
// This is forward-only stats collection - no container registration needed.
func (m *UserManager) StartTrafficStats(interval time.Duration) {
	if m.statsCollector == nil {
		m.statsCollector = newStatsCollector(m.forwardMgr, interval)
	}
	// Register callback to persist TotalUplink/TotalDownlink after each collect cycle.
	// We track the last-seen statsCollector totals so we can compute the delta
	// since the previous persist cycle and add it to the DB value. This avoids
	// overwriting historical traffic when the process restarts (statsCollector
	// totals start at zero on each run).
	lastSeen := make(map[string][2]int64) // username -> [lastTotalUp, lastTotalDown]
	m.statsCollector.onCollect = func(byUser map[string]UserTrafficStats) {
		// Aggregate per-username (sum across inbounds)
		totals := make(map[string][2]int64) // username -> [totalUp, totalDown]
		for _, v := range byUser {
			t := totals[v.Username]
			t[0] += v.TotalUplink
			t[1] += v.TotalDownlink
			totals[v.Username] = t
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		for username, t := range totals {
			if u, ok := m.users[username]; ok {
				prev := lastSeen[username]
				deltaUp := t[0] - prev[0]
				deltaDown := t[1] - prev[1]
				lastSeen[username] = t
				if deltaUp <= 0 && deltaDown <= 0 {
					continue
				}
				if deltaUp > 0 {
					u.TrafficTotalUplink += deltaUp
				}
				if deltaDown > 0 {
					u.TrafficTotalDownlink += deltaDown
				}
				if m.store != nil {
					_ = m.store.Save(u)
				}
			}
		}
	}
	m.statsCollector.Start()
}

// StartMaintenance starts background maintenance tasks (expired user cleanup).
// The interval specifies how often to check for expired users (default 1 minute if 0).
func (m *UserManager) StartMaintenance(interval time.Duration) {
	if interval == 0 {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			m.cleanupExpiredUsers()
		}
	}()
}

// StopTrafficStats stops the periodic traffic stats collection.
func (m *UserManager) StopTrafficStats() {
	if m.statsCollector != nil {
		m.statsCollector.Stop()
	}
}

// GetTrafficStats returns all aggregated traffic stats.
func (m *UserManager) GetTrafficStats() AggregatedStats {
	if m.statsCollector == nil {
		return AggregatedStats{}
	}
	return m.statsCollector.GetStats()
}

// GetUserTrafficStats returns traffic stats for a specific user.
func (m *UserManager) GetUserTrafficStats(username string) (*UserTrafficStats, bool) {
	if m.statsCollector == nil {
		return nil, false
	}
	return m.statsCollector.GetUserStats(username)
}

// GetInboundTrafficStats returns traffic stats for a specific inbound.
func (m *UserManager) GetInboundTrafficStats(ct contracts.ContainerType, inboundTag string) (*InboundTrafficStats, bool) {
	if m.statsCollector == nil {
		return nil, false
	}
	return m.statsCollector.GetInboundStats(ct, inboundTag)
}

// GetContainerTrafficStats returns traffic stats for a specific container.
func (m *UserManager) GetContainerTrafficStats(ct contracts.ContainerType) (*ContainerTrafficStats, bool) {
	if m.statsCollector == nil {
		return nil, false
	}
	return m.statsCollector.GetContainerStats(ct)
}

// GetGlobalTrafficStats returns global traffic stats.
func (m *UserManager) GetGlobalTrafficStats() GlobalTrafficStats {
	if m.statsCollector == nil {
		return GlobalTrafficStats{}
	}
	return m.statsCollector.GetGlobalStats()
}

// ForceTrafficStatsCollection forces an immediate traffic stats collection.
func (m *UserManager) ForceTrafficStatsCollection() {
	if m.statsCollector != nil {
		m.statsCollector.ForceCollect()
	}
}

// GetUserDeltaTraffic returns the incremental traffic for a user since the last reset.
// If reset is true, the delta counters are zeroed after reading.
// This is the primary method for Prometheus scrape (always reset=true).
func (m *UserManager) GetUserDeltaTraffic(username string, reset bool) (*UserTrafficStats, bool) {
	if m.statsCollector == nil {
		return nil, false
	}
	stats, ok := m.statsCollector.GetUserStats(username)
	if ok && reset {
		m.statsCollector.resetUserDelta(username)
	}
	return stats, ok
}

// GetAllDeltaTraffic returns all users' delta traffic and optionally resets all deltas.
// Used by BandwidthStatsCollector to drain stats for Prometheus.
func (m *UserManager) GetAllDeltaTraffic(reset bool) []UserTrafficStats {
	if m.statsCollector == nil {
		return nil
	}
	agg := m.statsCollector.GetStats()
	result := make([]UserTrafficStats, 0, len(agg.ByUser))
	for _, v := range agg.ByUser {
		result = append(result, v)
	}
	if reset {
		m.statsCollector.resetAllDeltas()
	}
	return result
}

// ResetUserTotalTraffic resets the lifetime cumulative traffic counters for a user.
// This does NOT affect the delta counters.
func (m *UserManager) ResetUserTotalTraffic(username string) error {
	if m.statsCollector == nil {
		return fmt.Errorf("stats collector not running")
	}
	m.statsCollector.resetUserTotal(username)
	// TODO: persist the reset to DB (update traffic_total_uplink/download to 0)
	return nil
}
