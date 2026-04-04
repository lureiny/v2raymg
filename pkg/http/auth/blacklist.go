package auth

import "sync"

// tokenBlacklist is an in-memory set of revoked JWT JTIs.
// Cleared on process restart; JWT expiry provides the ultimate safety net.
type tokenBlacklist struct {
	mu  sync.RWMutex
	set map[string]struct{}
}

var globalBlacklist = &tokenBlacklist{set: make(map[string]struct{})}

// BlacklistAdd adds a jti to the revocation list.
func BlacklistAdd(jti string) {
	globalBlacklist.mu.Lock()
	defer globalBlacklist.mu.Unlock()
	globalBlacklist.set[jti] = struct{}{}
}

// BlacklistContains reports whether jti has been revoked.
func BlacklistContains(jti string) bool {
	globalBlacklist.mu.RLock()
	defer globalBlacklist.mu.RUnlock()
	_, ok := globalBlacklist.set[jti]
	return ok
}
