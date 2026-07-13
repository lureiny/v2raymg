package forward

import (
	"sync"
	"sync/atomic"
	"time"
)

// ClientLimitConfig holds the configuration for client-based connection limiting.
type ClientLimitConfig struct {
	// MaxClients is the maximum number of unique remote IPs allowed (slot limit).
	// Default: 1 (exclusive).
	MaxClients int

	// RecycleDelaySec is the delay in seconds before releasing an idle client slot.
	// Default: 60 seconds.
	RecycleDelaySec int

	// SingleDirectionDrainSec is the drain timeout after one direction ends.
	// Default: 2 seconds.
	SingleDirectionDrainSec int
}

// DefaultClientLimitConfig returns the default configuration.
func DefaultClientLimitConfig() ClientLimitConfig {
	return ClientLimitConfig{
		MaxClients:              1,
		RecycleDelaySec:         60,
		SingleDirectionDrainSec: 2,
	}
}

// clientSlot represents the state of a single remote IP (client).
type clientSlot struct {
	activeConns  atomic.Int64 // atomic counter for confirmed active connections
	pendingConns atomic.Int64 // atomic counter for pending connections (dialing, not yet confirmed)
	recycling    int32        // atomic flag: 1 = slot is being recycled (no active conns, timer running)
	recycleTimer *time.Timer  // timer for real-time slot recycling
	mu           sync.Mutex   // for timer and recycling flag operations
}

// ClientLimiter is the interface for remote_ip based client connection limiting.
type ClientLimiter interface {
	// Acquire attempts to acquire a connection slot for the given remote IP.
	// Returns true if allowed, false if rejected. Increments pending count.
	Acquire(remoteIP string) bool

	// ConfirmAcquire converts a pending connection to active after dial succeeds.
	// Decrements pending, increments active.
	ConfirmAcquire(remoteIP string)

	// CancelAcquire cancels a pending connection when dial fails.
	// Decrements pending and removes slot if no active/pending connections remain.
	CancelAcquire(remoteIP string)

	// Release releases a connection slot for the given remote IP.
	// Should be called when a connection is closed/cleaned up.
	Release(remoteIP string)

	// ActiveConnections returns the total number of active connections.
	ActiveConnections() int64

	// CleanupExpiredSlots removes slots that have been idle beyond recycle delay.
	CleanupExpiredSlots()

	// OnSingleDirectionEnd is called when one direction of the relay ends.
	// Returns the drain deadline for the other direction.
	OnSingleDirectionEnd(remoteIP string) time.Time

	// RecordActivity records activity for a remote IP, resetting its drain deadline.
	RecordActivity(remoteIP string)

	// IsDrainExpired reports whether the single-direction drain deadline for
	// remoteIP has elapsed with no intervening RecordActivity. Read live each
	// tick so active reverse traffic renews the deadline (no false close) while
	// idle half-open connections are still reclaimed after drainSec.
	IsDrainExpired(remoteIP string) bool
}

// remoteIPClientLimiter implements ClientLimiter with remote IP based slots.
type remoteIPClientLimiter struct {
	mu       sync.Mutex
	config   ClientLimitConfig
	slots    map[string]*clientSlot // key: remote IP
	drainEnd map[string]time.Time   // drain deadline for each remote IP (single-direction recovery)
}

// newRemoteIPClientLimiter creates a remoteIPClientLimiter.
// MaxClients <= 0 means passthrough (unlimited): Acquire still tracks slots
// so that a later SetConfig(MaxClients>0) can count in-flight sessions toward
// the quota, but never rejects. This "stable reference" design mirrors
// userBandwidthLimiter and lets callers update the policy in-place without
// swapping the limiter attached to active relays.
func newRemoteIPClientLimiter(config ClientLimitConfig) *remoteIPClientLimiter {
	if config.RecycleDelaySec <= 0 {
		config.RecycleDelaySec = 60
	}
	if config.SingleDirectionDrainSec <= 0 {
		config.SingleDirectionDrainSec = 2
	}
	return &remoteIPClientLimiter{
		config:   config,
		slots:    make(map[string]*clientSlot),
		drainEnd: make(map[string]time.Time),
	}
}

// Acquire attempts to acquire a connection for the given remote IP.
// Increments pending count to reserve the slot during dial.
func (l *remoteIPClientLimiter) Acquire(remoteIP string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if remote IP already has a slot
	slot, exists := l.slots[remoteIP]
	if exists {
		// Cancel any pending recycle timer (same IP reconnected during recycling)
		if slot.recycleTimer != nil {
			slot.recycleTimer.Stop()
			slot.recycleTimer = nil
		}
		// Clear recycling flag
		atomic.StoreInt32(&slot.recycling, 0)
		// Increment pending count for this acquire attempt
		slot.pendingConns.Add(1)
		return true
	}

	// Count active slots (slots with active OR pending connections, OR recycling)
	// Recycling slots still count towards the limit to block new IPs
	activeSlots := 0
	for _, s := range l.slots {
		// Count slot if: has active/pending, OR is recycling (still holds the slot)
		hasConnections := s.activeConns.Load() > 0 || s.pendingConns.Load() > 0
		isRecycling := atomic.LoadInt32(&s.recycling) == 1
		if hasConnections || isRecycling {
			activeSlots++
		}
	}

	// Passthrough when MaxClients <= 0: always admit, but still allocate a
	// slot below so that a subsequent SetConfig(MaxClients>0) can account for
	// in-flight sessions.
	if l.config.MaxClients > 0 && activeSlots >= l.config.MaxClients {
		return false
	}

	// Create new slot with pending=1 (will be converted to active on ConfirmAcquire)
	newSlot := &clientSlot{}
	newSlot.pendingConns.Store(1)
	l.slots[remoteIP] = newSlot
	return true
}

// ConfirmAcquire converts a pending connection to active after dial succeeds.
func (l *remoteIPClientLimiter) ConfirmAcquire(remoteIP string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	slot, exists := l.slots[remoteIP]
	if !exists {
		// This shouldn't happen if Acquire returned true
		return
	}
	// Decrement pending, increment active
	slot.pendingConns.Add(-1)
	slot.activeConns.Add(1)
}

// CancelAcquire cancels a pending connection when dial fails.
func (l *remoteIPClientLimiter) CancelAcquire(remoteIP string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	slot, exists := l.slots[remoteIP]
	if !exists {
		return
	}

	// Decrement pending
	slot.pendingConns.Add(-1)

	// Check if slot has no active or pending connections
	active := slot.activeConns.Load()
	pending := slot.pendingConns.Load()
	if active == 0 && pending == 0 {
		// Remove the empty slot and its drain deadline together — the client is
		// fully gone, so any drainEnd entry is stale. Without this the drainEnd
		// map grew one entry per unique remote IP that ever drained and was never
		// evicted (ClearDrain had no callers).
		delete(l.slots, remoteIP)
		l.clearDrainLocked(remoteIP)
	}
}

// Release releases a connection for the given remote IP.
func (l *remoteIPClientLimiter) Release(remoteIP string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	slot, exists := l.slots[remoteIP]
	if !exists {
		return
	}

	// Decrement atomic counter with underflow protection
	newVal := slot.activeConns.Add(-1)
	if newVal < 0 {
		// Underflow protection: clamp to 0
		slot.activeConns.Store(0)
		newVal = 0
	}

	// If still has connections, don't start recycling
	if newVal > 0 {
		return
	}

	// activeConns == 0, start real-time recycle timer
	recycleDelay := time.Duration(l.config.RecycleDelaySec) * time.Second

	// Set recycling flag so Acquire knows to skip this slot in count
	atomic.StoreInt32(&slot.recycling, 1)

	// Stop any existing timer first
	if slot.recycleTimer != nil {
		slot.recycleTimer.Stop()
	}

	// Create new timer that will delete the slot after delay
	// Capture the remoteIP for the callback
	ip := remoteIP
	slot.recycleTimer = time.AfterFunc(recycleDelay, func() {
		l.mu.Lock()
		defer l.mu.Unlock()

		// Double-check: only delete if still no active/pending connections
		s, exists := l.slots[ip]
		if !exists {
			return
		}
		active := s.activeConns.Load()
		pending := s.pendingConns.Load()
		if active == 0 && pending == 0 {
			// Safe to delete - stop timer and remove slot (and its stale
			// drainEnd entry) so neither map grows unbounded under IP churn.
			if s.recycleTimer != nil {
				s.recycleTimer.Stop()
			}
			delete(l.slots, ip)
			l.clearDrainLocked(ip)
		}
	})
}

// ActiveConnections returns the total number of active connections.
func (l *remoteIPClientLimiter) ActiveConnections() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	var total int64
	for _, slot := range l.slots {
		total += slot.activeConns.Load()
	}
	return total
}

// CleanupExpiredSlots is kept for interface compatibility.
// Actual recycling is now done via real-time timers in Release.
// This function is a no-op.
func (l *remoteIPClientLimiter) CleanupExpiredSlots() {
	// No-op: real-time recycling is handled by timers in Release
}

// OnSingleDirectionEnd is called when one direction ends.
// Returns the deadline for the other direction to complete.
func (l *remoteIPClientLimiter) OnSingleDirectionEnd(remoteIP string) time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()

	deadline := time.Now().Add(time.Duration(l.config.SingleDirectionDrainSec) * time.Second)
	l.drainEnd[remoteIP] = deadline
	return deadline
}

// RecordActivity records activity for a remote IP, resetting its drain deadline.
func (l *remoteIPClientLimiter) RecordActivity(remoteIP string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Reset drain deadline
	l.drainEnd[remoteIP] = time.Now().Add(time.Duration(l.config.SingleDirectionDrainSec) * time.Second)
}

// IsDrainExpired checks if the drain deadline has expired for a remote IP.
func (l *remoteIPClientLimiter) IsDrainExpired(remoteIP string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	deadline, exists := l.drainEnd[remoteIP]
	if !exists {
		return true // No deadline, treat as expired
	}
	return time.Now().After(deadline)
}

// ClearDrain clears the drain deadline for a remote IP.
func (l *remoteIPClientLimiter) ClearDrain(remoteIP string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.clearDrainLocked(remoteIP)
}

// clearDrainLocked removes any drain deadline for remoteIP. Caller must hold
// l.mu. Called both by ClearDrain and inline whenever a client slot is removed,
// so the drainEnd map is bounded to the same lifecycle as slots.
func (l *remoteIPClientLimiter) clearDrainLocked(remoteIP string) {
	delete(l.drainEnd, remoteIP)
}

// SetConfig updates the limiter configuration.
func (l *remoteIPClientLimiter) SetConfig(config ClientLimitConfig) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.config = config
}

// NewTestClientLimiter creates a ClientLimiter for testing with the given max clients.
func NewTestClientLimiter(maxClients int) ClientLimiter {
	config := ClientLimitConfig{
		MaxClients:              maxClients,
		RecycleDelaySec:         60,
		SingleDirectionDrainSec: 2,
	}
	return newRemoteIPClientLimiter(config)
}
