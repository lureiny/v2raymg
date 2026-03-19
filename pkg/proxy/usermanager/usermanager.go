// Package usermanager provides user management for proxy containers.
// It handles user CRUD operations and port allocation.
package usermanager

import (
	"fmt"
	"github.com/lureiny/v2raymg/pkg/log"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/errors"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/store"
)

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
}

// NewUserManager creates a new user manager (pure in-memory mode, no persistence).
func NewUserManager(fm forward.ForwardManager) *UserManager {
	return &UserManager{
		users:          make(map[string]*contracts.User),
		forwardMgr:     fm,
		eventCh:        make(chan UserEvent, 100), // Buffered channel
		statsCollector: newStatsCollector(fm, time.Minute),
	}
}

// NewUserManagerWithStore creates a new user manager backed by the given store manager.
// All users are loaded from store on construction. Returns an error if load fails.
func NewUserManagerWithStore(fm forward.ForwardManager, storeMgr *store.StoreManager) (*UserManager, error) {
	m := &UserManager{
		users:          make(map[string]*contracts.User),
		forwardMgr:     fm,
		store:          storeMgr.UserStore(),
		eventCh:        make(chan UserEvent, 100),
		statsCollector: newStatsCollector(fm, time.Minute),
	}

	loaded, err := storeMgr.UserStore().Load()
	if err != nil {
		return nil, fmt.Errorf("NewUserManagerWithStore: load users: %w", err)
	}
	for _, u := range loaded {
		m.users[u.Username] = u
	}
	return m, nil
}

// GetForwardManager returns the ForwardManager for port mapping operations.
// Returns nil if ForwardManager is not configured.
func (m *UserManager) GetForwardManager() forward.ForwardManager {
	return m.forwardMgr
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
			// User is in deletion state - get remaining forward rules info for error message
			var ruleInfo string
			if len(existing.BindPorts) > 0 && m.forwardMgr != nil {
				rules := m.forwardMgr.GetRulesByUser(req.Username)
				if len(rules) > 0 {
					ruleInfo = fmt.Sprintf(" (remaining rules: %d, ports: %v)", len(rules), existing.BindPorts)
				}
			}
			return errors.New(errors.ErrUserDeletionInProgress,
				fmt.Sprintf("user %s is being deleted, cannot add%s", req.Username, ruleInfo))
		}
		return errors.New(errors.ErrUserAlreadyExists, fmt.Sprintf("user %s already exists", req.Username))
	}

	user := &contracts.User{
		Username: req.Username,
		Password: req.Password,
	}

	if req.TTL > 0 {
		user.ExpiryTime = time.Now().Add(req.TTL)
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

// RemoveUser marks a user for deletion (soft delete).
// The user is marked as "deleting" and hidden from normal queries.
// Actual cleanup (port release, forward rule deletion) should be done via separate finalize step.
// This method is idempotent - calling multiple times will not cause errors.
func (m *UserManager) RemoveUser(req RemoveUserRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[req.Username]
	if !exists {
		// User not found - idempotent, return nil
		return nil
	}

	// Already marked for deletion - idempotent
	if user.IsDeleting() {
		return nil
	}

	// Physically delete from store before marking in-memory
	if m.store != nil {
		if err := m.store.Delete(req.Username); err != nil {
			return fmt.Errorf("RemoveUser: persist: %w", err)
		}
	}

	// Mark user as deleting (soft delete)
	// Do NOT delete the user record or clean up ports yet
	// Cleanup will be done via a separate finalize step
	user.MarkDeleting()

	// Emit remove event (but keep the user record for cleanup)
	m.emitEvent(UserEvent{
		Type:     UserEventRemove,
		Username: req.Username,
		Ports:    user.BindPorts, // Keep the ports for reference
	})

	return nil
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

// ListUsers returns all users.
// ListUsers returns all active (non-deleting) users.
func (m *UserManager) ListUsers() []*contracts.User {
	m.mu.RLock()
	defer m.mu.RUnlock()

	users := make([]*contracts.User, 0, len(m.users))
	for _, user := range m.users {
		// Skip users marked for deletion
		if user.IsDeleting() {
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
		result[user.Username] = user.Password
	}
	return result
}

// UpdateUser updates a user's password and/or expiry time.
// expireTime is a Unix timestamp; 0 means no change to expiry.
// If password is non-empty, it is updated. If expireTime > 0, ExpiryTime is set.
func (m *UserManager) UpdateUser(username, password string, expireTime int64) error {
	if username == "" {
		return errors.New(errors.ErrInvalidUserSpec, "username cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[username]
	if !exists {
		return errors.New(errors.ErrUserNotFound, fmt.Sprintf("user %s not found", username))
	}

	if password != "" {
		user.Password = password
	}
	if expireTime > 0 {
		user.ExpiryTime = time.Unix(expireTime, 0)
	}

	if m.store != nil {
		if err := m.store.Save(user); err != nil {
			return fmt.Errorf("UpdateUser: persist: %w", err)
		}
	}

	m.emitEvent(UserEvent{
		Type:     UserEventUpdate,
		Username: username,
		User:     user,
	})

	return nil
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

	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[req.Username]
	if !exists {
		return errors.New(errors.ErrUserNotFound, fmt.Sprintf("user %s not found", req.Username))
	}

	if m.store != nil {
		updated := *user
		updated.Password = req.NewPassword
		if err := m.store.Save(&updated); err != nil {
			return fmt.Errorf("UpdateUserPassword: persist: %w", err)
		}
	}

	user.Password = req.NewPassword

	// Emit update event
	m.emitEvent(UserEvent{
		Type:     UserEventUpdate,
		Username: req.Username,
		User:     user,
	})

	return nil
}

// GetBindPortRequest contains parameters for getting a bind port.
type GetBindPortRequest struct {
	Username      string
	TargetPort    uint32           // The target port to forward to
	ContainerType contracts.ContainerType
	InboundTag    string
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

// RotateUserPort releases all existing forward ports for a user and allocates
// new ones. Used when the user's current port is blocked/unreachable.
// The user continues to exist in all inbound configurations; only the forward
// mapping (external port → inbound port) changes.
func (m *UserManager) RotateUserPort(username string) error {
	if username == "" {
		return errors.New(errors.ErrInvalidUserSpec, "username is required")
	}

	// Step 1: snapshot existing rules and clear user port state (under lock)
	m.mu.Lock()
	user, exists := m.users[username]
	if !exists {
		m.mu.Unlock()
		return errors.New(errors.ErrUserNotFound, fmt.Sprintf("user %s not found", username))
	}
	if user.IsExpired() {
		m.mu.Unlock()
		return errors.New(errors.ErrUserExpired, fmt.Sprintf("user %s has expired", username))
	}

	// Collect existing rules *before* removing them
	var oldRules []*forward.ForwardRule
	if m.forwardMgr != nil {
		oldRules = m.forwardMgr.GetRulesByUser(username)
	}

	// Clear user port state so GetBindPort will allocate fresh ports
	user.BindPorts = nil
	user.PortMappings = nil
	m.mu.Unlock()

	if m.forwardMgr == nil {
		return errors.New(errors.ErrInternal, "forward manager not available")
	}

	// Step 2: remove all existing forward rules (releases old ports)
	if err := m.forwardMgr.RemoveRulesByUser(username); err != nil {
		log.Warn("[RotateUserPort] failed to remove old forward rules", "user", username, "err", err)
		// Continue anyway — old rules may already be dead
	}

	// Step 3: re-allocate a new forward port for each old rule
	for _, rule := range oldRules {
		// Parse target port from TargetAddr ("127.0.0.1:PORT")
		var targetPort uint32
		if _, err := fmt.Sscanf(rule.TargetAddr, "127.0.0.1:%d", &targetPort); err != nil || targetPort == 0 {
			log.Warn("[RotateUserPort] could not parse target port, skipping rule", "user", username, "target", rule.TargetAddr)
			continue
		}

		if _, err := m.GetBindPort(GetBindPortRequest{
			Username:      username,
			TargetPort:    targetPort,
			ContainerType: rule.ContainerType,
			InboundTag:    rule.InboundTag,
		}); err != nil {
			log.Error("[RotateUserPort] failed to allocate new port", "user", username, "targetPort", targetPort, "err", err)
			return fmt.Errorf("rotate port failed for user %s: %w", username, err)
		}
	}

	log.Info("[RotateUserPort] port rotation complete", "user", username, "rules", len(oldRules))
	return nil
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

	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[username]
	if !exists {
		return errors.New(errors.ErrUserNotFound, fmt.Sprintf("user %s not found", username))
	}

	if m.store != nil {
		updated := *user
		if kind == forward.BandwidthUpload {
			updated.BandwidthUploadBps = bytesPerSec
		} else {
			updated.BandwidthDownloadBps = bytesPerSec
		}
		if err := m.store.Save(&updated); err != nil {
			return fmt.Errorf("SetUserBandwidthLimit: persist: %w", err)
		}
	}

	// Update user's bandwidth configuration
	if kind == forward.BandwidthUpload {
		user.BandwidthUploadBps = bytesPerSec
	} else {
		user.BandwidthDownloadBps = bytesPerSec
	}

	// Sync to ForwardManager
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
	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[username]
	if !exists {
		return errors.New(errors.ErrUserNotFound, "user not found")
	}

	if m.store != nil {
		updated := *user
		updated.MaxClients = maxClients
		updated.ClientRecycleDelaySec = recycleDelaySec
		updated.ClientDrainSec = drainSec
		if err := m.store.Save(&updated); err != nil {
			return fmt.Errorf("SetUserClientLimit: persist: %w", err)
		}
	}

	user.MaxClients = maxClients
	user.ClientRecycleDelaySec = recycleDelaySec
	user.ClientDrainSec = drainSec

	// Sync to ForwardManager if available (including maxClients=0 to disable)
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

	// Emit port bind event for release
	m.emitEvent(UserEvent{
		Type:     UserEventPortBind,
		Username: req.Username,
		User:     user,
		Ports:    []uint32{req.BindPort},
	})

	// Remove forward rule via ForwardManager
	if m.forwardMgr != nil {
		// Remove rules for this user
		m.forwardMgr.RemoveRulesByUser(req.Username)

		// Check if user is in deleting state and needs finalization
		if user.IsDeleting() {
			// Query remaining rules for this user
			remainingRules := m.forwardMgr.GetRulesByUser(req.Username)
			if len(remainingRules) == 0 {
				// No more rules - finalize the deletion
				if m.store != nil {
					if err := m.store.Delete(req.Username); err != nil {
						log.Warn("ReleaseBindPort: failed to delete from store", "user", req.Username, "err", err)
					}
				}
				delete(m.users, req.Username)
			}
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

// StartExpiredUserCleanup starts a background goroutine that periodically
// checks for expired users and cleans them up.
// The interval parameter specifies how often to check for expired users.
func (m *UserManager) StartExpiredUserCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		for range ticker.C {
			m.cleanupExpiredUsers()
		}
	}()
}

// cleanupExpiredUsers removes all expired users.
func (m *UserManager) cleanupExpiredUsers() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for username, user := range m.users {
		if user.IsExpired() {
			// Capture ports for event
			ports := make([]uint32, len(user.BindPorts))
			copy(ports, user.BindPorts)

			// Delete from store (best-effort; log failure but continue cleanup)
			if m.store != nil {
				if err := m.store.Delete(username); err != nil {
					log.Warn("cleanupExpiredUsers: failed to delete from store", "user", username, "err", err)
				}
			}

			// Clean up ports first
			m.cleanupUserPorts(username, user.BindPorts)
			// Clear bindings
			user.BindPorts = nil
			// Remove user
			delete(m.users, username)

			// Emit expire event
			m.emitEvent(UserEvent{
				Type:     UserEventExpire,
				Username: username,
				Ports:    ports,
			})
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
		Password:  "test",
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
				u.TrafficTotalUplink = t[0]
				u.TrafficTotalDownlink = t[1]
				if m.store != nil {
					_ = m.store.Save(u)
				}
			}
		}
	}
	m.statsCollector.Start()
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
