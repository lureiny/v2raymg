package auth

import (
	"sync"
	"time"
)

const (
	defaultBlacklistTTL      = 24 * time.Hour
	blacklistSweepInterval   = 5 * time.Minute
	blacklistSweepMinEntries = 256
)

// tokenBlacklist is an in-memory set of revoked JWT JTIs with per-entry expiry.
// Expired entries are evicted lazily on lookup/insert; cleared on process restart.
type tokenBlacklist struct {
	mu        sync.RWMutex
	entries   map[string]time.Time
	nowFunc   func() time.Time
	lastSweep time.Time
}

func newTokenBlacklist() *tokenBlacklist {
	return &tokenBlacklist{
		entries: make(map[string]time.Time),
		nowFunc: time.Now,
	}
}

var globalBlacklist = newTokenBlacklist()

func (b *tokenBlacklist) now() time.Time {
	if b.nowFunc != nil {
		return b.nowFunc()
	}
	return time.Now()
}

func (b *tokenBlacklist) purgeExpiredLocked(now time.Time) {
	for k, exp := range b.entries {
		if !now.Before(exp) {
			delete(b.entries, k)
		}
	}
	b.lastSweep = now
}

func (b *tokenBlacklist) add(jti string, expiresAt time.Time) {
	if jti == "" {
		return
	}
	now := b.now()
	if expiresAt.IsZero() {
		expiresAt = now.Add(defaultBlacklistTTL)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.entries) >= blacklistSweepMinEntries || now.Sub(b.lastSweep) >= blacklistSweepInterval {
		b.purgeExpiredLocked(now)
	}
	if !expiresAt.After(now) {
		return
	}
	b.entries[jti] = expiresAt
}

func (b *tokenBlacklist) contains(jti string) bool {
	if jti == "" {
		return false
	}
	now := b.now()

	b.mu.RLock()
	exp, ok := b.entries[jti]
	b.mu.RUnlock()
	if !ok {
		return false
	}
	if now.Before(exp) {
		return true
	}

	b.mu.Lock()
	if curExp, stillOk := b.entries[jti]; stillOk && !now.Before(curExp) {
		delete(b.entries, jti)
	}
	b.mu.Unlock()
	return false
}

// BlacklistAdd adds a jti to the revocation list using the default 24h TTL.
func BlacklistAdd(jti string) {
	globalBlacklist.add(jti, time.Time{})
}

// BlacklistAddWithExpiry adds a jti to the revocation list until expiresAt.
func BlacklistAddWithExpiry(jti string, expiresAt time.Time) {
	globalBlacklist.add(jti, expiresAt)
}

// BlacklistContains reports whether jti has been revoked and has not yet expired.
func BlacklistContains(jti string) bool {
	return globalBlacklist.contains(jti)
}
