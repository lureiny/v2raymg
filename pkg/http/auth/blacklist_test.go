package auth

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBlacklist_AddAndContains(t *testing.T) {
	jti := "test-blacklist-add-" + t.Name()
	BlacklistAdd(jti)
	if !BlacklistContains(jti) {
		t.Fatalf("expected blacklist to contain %q after Add", jti)
	}
}

func TestBlacklist_NotAdded(t *testing.T) {
	jti := "test-blacklist-notadded-" + t.Name()
	if BlacklistContains(jti) {
		t.Fatalf("expected blacklist not to contain %q before Add", jti)
	}
}

func TestBlacklist_Concurrent(t *testing.T) {
	const n = 50
	var wg sync.WaitGroup
	jtis := make([]string, n)
	for i := 0; i < n; i++ {
		jtis[i] = fmt.Sprintf("concurrent-blacklist-%s-%d", t.Name(), i)
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(jti string) {
			defer wg.Done()
			BlacklistAdd(jti)
			BlacklistContains(jti)
		}(jtis[i])
	}
	wg.Wait()
	// All added JTIs must be present
	for _, jti := range jtis {
		if !BlacklistContains(jti) {
			t.Errorf("expected blacklist to contain %q after concurrent Add", jti)
		}
	}
}

func TestBlacklist_ExpiryAndLazyEviction(t *testing.T) {
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	bl := newTokenBlacklist()
	bl.nowFunc = func() time.Time { return now }

	// Already-expired entry must not be stored.
	bl.add("already-expired", now.Add(-time.Second))
	if len(bl.entries) != 0 {
		t.Fatalf("expected 0 entries for already-expired JTI, got %d", len(bl.entries))
	}

	// Active entry expires cleanly and is lazily evicted on lookup.
	bl.add("jti-1", now.Add(10*time.Minute))
	if !bl.contains("jti-1") {
		t.Fatal("expected jti-1 to be present before expiry")
	}
	now = now.Add(10 * time.Minute)
	if bl.contains("jti-1") {
		t.Fatal("expected jti-1 to be expired at deadline")
	}
	if _, exists := bl.entries["jti-1"]; exists {
		t.Fatal("expected expired jti-1 to be deleted from map on lookup")
	}

	// Periodic sweep on add removes stale entries after blacklistSweepInterval.
	bl.add("old-a", now.Add(time.Minute))
	bl.add("old-b", now.Add(time.Minute))
	now = now.Add(blacklistSweepInterval + time.Second)
	bl.add("fresh", now.Add(time.Hour))
	if len(bl.entries) != 1 {
		t.Fatalf("expected periodic sweep to keep only 1 fresh entry, got %d", len(bl.entries))
	}
	if !bl.contains("fresh") {
		t.Fatal("expected fresh entry to survive sweep")
	}

	// Threshold sweep triggers immediately when len >= blacklistSweepMinEntries even before interval.
	for i := 0; i < blacklistSweepMinEntries; i++ {
		bl.add(fmt.Sprintf("burst-%d", i), now.Add(time.Minute))
	}
	now = now.Add(2 * time.Minute) // expire burst entries while "fresh" remains valid, still within blacklistSweepInterval
	bl.add("post-burst", now.Add(time.Hour))
	if len(bl.entries) != 2 || !bl.contains("fresh") || !bl.contains("post-burst") {
		t.Fatalf("expected threshold sweep to purge burst entries leaving 2 live entries, got len=%d", len(bl.entries))
	}
}

func TestBlacklist_AddWithExpiry_Global(t *testing.T) {
	jtiActive := "global-active-" + t.Name()
	jtiExpired := "global-expired-" + t.Name()

	BlacklistAddWithExpiry(jtiActive, time.Now().Add(time.Minute))
	if !BlacklistContains(jtiActive) {
		t.Fatalf("expected %q to be blacklisted before expiry", jtiActive)
	}

	BlacklistAddWithExpiry(jtiExpired, time.Now().Add(-time.Minute))
	if BlacklistContains(jtiExpired) {
		t.Fatalf("expected expired %q not to remain blacklisted", jtiExpired)
	}
}
