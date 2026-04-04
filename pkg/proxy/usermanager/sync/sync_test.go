package sync

import (
	"fmt"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// ---------------------------------------------------------------------------
// IsNewer
// ---------------------------------------------------------------------------

func TestIsNewer_HigherTimestampWins(t *testing.T) {
	a := &contracts.User{UpdatedAtUs: 200, OriginNode: "a"}
	b := &contracts.User{UpdatedAtUs: 100, OriginNode: "z"}
	if !IsNewer(a, b) {
		t.Error("a (ts=200) should be newer than b (ts=100)")
	}
	if IsNewer(b, a) {
		t.Error("b (ts=100) should NOT be newer than a (ts=200)")
	}
}

func TestIsNewer_SameTimestamp_LexicographicTiebreak(t *testing.T) {
	a := &contracts.User{UpdatedAtUs: 100, OriginNode: "node-b"}
	b := &contracts.User{UpdatedAtUs: 100, OriginNode: "node-a"}
	if !IsNewer(a, b) {
		t.Error("node-b > node-a lexicographically, so a should be newer")
	}
	if IsNewer(b, a) {
		t.Error("node-a < node-b, so b should NOT be newer")
	}
}

func TestIsNewer_Identical_ReturnsFalse(t *testing.T) {
	a := &contracts.User{UpdatedAtUs: 100, OriginNode: "node-a"}
	b := &contracts.User{UpdatedAtUs: 100, OriginNode: "node-a"}
	if IsNewer(a, b) {
		t.Error("identical versions should not be newer")
	}
}

// ---------------------------------------------------------------------------
// ComputeHash
// ---------------------------------------------------------------------------

func TestComputeHash_Deterministic(t *testing.T) {
	u := &contracts.User{
		Username:    "alice",
		AuthToken:    "secret",
		ExpiryTime:  time.Unix(1700000000, 0),
		Role:        "normal",
		TargetGroup: "default",
		UpdatedAtUs: 12345678,
	}
	h1 := ComputeHash(u)
	h2 := ComputeHash(u)
	if h1 != h2 {
		t.Errorf("same input should produce same hash, got %q and %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64 hex chars (SHA-256), got %d", len(h1))
	}
}

func TestComputeHash_DifferentAuthToken_DifferentHash(t *testing.T) {
	u1 := &contracts.User{Username: "alice", AuthToken: "pw1", UpdatedAtUs: 1}
	u2 := &contracts.User{Username: "alice", AuthToken: "pw2", UpdatedAtUs: 1}
	if ComputeHash(u1) == ComputeHash(u2) {
		t.Error("different AuthTokens should produce different hashes")
	}
}

func TestComputeHash_DifferentUpdatedAtUs_DifferentHash(t *testing.T) {
	u1 := &contracts.User{Username: "alice", AuthToken: "pw", UpdatedAtUs: 1}
	u2 := &contracts.User{Username: "alice", AuthToken: "pw", UpdatedAtUs: 2}
	if ComputeHash(u1) == ComputeHash(u2) {
		t.Error("different UpdatedAtUs should produce different hashes")
	}
}

func TestComputeHash_DeletingState_DifferentHash(t *testing.T) {
	u1 := &contracts.User{Username: "alice", AuthToken: "pw", UpdatedAtUs: 1}
	u2 := &contracts.User{Username: "alice", AuthToken: "pw", UpdatedAtUs: 1}
	u2.MarkDeleting()
	if ComputeHash(u1) == ComputeHash(u2) {
		t.Error("deleting vs active should produce different hashes")
	}
}

func TestComputeHash_ZeroExpiryTime(t *testing.T) {
	u := &contracts.User{Username: "alice", UpdatedAtUs: 1}
	h := ComputeHash(u)
	if h == "" {
		t.Error("hash should not be empty for zero expiry")
	}
}

func TestComputeHash_OriginNode_NotIncluded(t *testing.T) {
	u1 := &contracts.User{Username: "alice", AuthToken: "pw", UpdatedAtUs: 1, OriginNode: "node-a"}
	u2 := &contracts.User{Username: "alice", AuthToken: "pw", UpdatedAtUs: 1, OriginNode: "node-b"}
	if ComputeHash(u1) != ComputeHash(u2) {
		t.Error("OriginNode should NOT be included in hash")
	}
}

// ---------------------------------------------------------------------------
// CompareDigests
// ---------------------------------------------------------------------------

func TestCompareDigests_NewUser_NeedsFull(t *testing.T) {
	getLocal := func(username string) (*contracts.User, error) {
		return nil, nil // user not found
	}
	need, err := CompareDigests(getLocal, []UserDigest{
		{Username: "alice", UpdatedAtUs: 100, OriginNode: "node-a", Hash: "abc"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(need) != 1 || need[0] != "alice" {
		t.Errorf("expected [alice], got %v", need)
	}
}

func TestCompareDigests_RemoteNewer_NeedsFull(t *testing.T) {
	local := &contracts.User{Username: "alice", UpdatedAtUs: 50, OriginNode: "node-a", Hash: "old"}
	getLocal := func(username string) (*contracts.User, error) {
		return local, nil
	}
	need, err := CompareDigests(getLocal, []UserDigest{
		{Username: "alice", UpdatedAtUs: 100, OriginNode: "node-a", Hash: "new"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(need) != 1 || need[0] != "alice" {
		t.Errorf("expected [alice], got %v", need)
	}
}

func TestCompareDigests_LocalNewer_NoFull(t *testing.T) {
	local := &contracts.User{Username: "alice", UpdatedAtUs: 200, OriginNode: "node-a", Hash: "h"}
	getLocal := func(username string) (*contracts.User, error) {
		return local, nil
	}
	need, err := CompareDigests(getLocal, []UserDigest{
		{Username: "alice", UpdatedAtUs: 100, OriginNode: "node-a", Hash: "h"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(need) != 0 {
		t.Errorf("expected no full sync needed, got %v", need)
	}
}

func TestCompareDigests_SameVersion_HashMismatch_NeedsFull(t *testing.T) {
	local := &contracts.User{Username: "alice", UpdatedAtUs: 100, OriginNode: "node-a", Hash: "aaa"}
	getLocal := func(username string) (*contracts.User, error) {
		return local, nil
	}
	need, err := CompareDigests(getLocal, []UserDigest{
		{Username: "alice", UpdatedAtUs: 100, OriginNode: "node-a", Hash: "bbb"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(need) != 1 || need[0] != "alice" {
		t.Errorf("hash mismatch should trigger full sync, got %v", need)
	}
}

func TestCompareDigests_SameVersion_SameHash_NoFull(t *testing.T) {
	local := &contracts.User{Username: "alice", UpdatedAtUs: 100, OriginNode: "node-a", Hash: "same"}
	getLocal := func(username string) (*contracts.User, error) {
		return local, nil
	}
	need, err := CompareDigests(getLocal, []UserDigest{
		{Username: "alice", UpdatedAtUs: 100, OriginNode: "node-a", Hash: "same"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(need) != 0 {
		t.Errorf("same version and hash should not need full sync, got %v", need)
	}
}

func TestCompareDigests_DBError_NeedsFullAndReportsError(t *testing.T) {
	getLocal := func(username string) (*contracts.User, error) {
		return nil, fmt.Errorf("db down")
	}
	need, err := CompareDigests(getLocal, []UserDigest{
		{Username: "alice", UpdatedAtUs: 100, OriginNode: "node-a", Hash: "h"},
	})
	if err == nil {
		t.Fatal("expected error when DB fails")
	}
	if len(need) != 1 || need[0] != "alice" {
		t.Errorf("DB error should still request full sync, got %v", need)
	}
}

func TestCompareDigests_MultipleUsers_Mixed(t *testing.T) {
	users := map[string]*contracts.User{
		"alice": {Username: "alice", UpdatedAtUs: 100, OriginNode: "node-a", Hash: "h1"},
		"bob":   {Username: "bob", UpdatedAtUs: 200, OriginNode: "node-a", Hash: "h2"},
	}
	getLocal := func(username string) (*contracts.User, error) {
		u, ok := users[username]
		if !ok {
			return nil, nil
		}
		return u, nil
	}
	need, err := CompareDigests(getLocal, []UserDigest{
		{Username: "alice", UpdatedAtUs: 100, OriginNode: "node-a", Hash: "h1"}, // same → skip
		{Username: "bob", UpdatedAtUs: 300, OriginNode: "node-a", Hash: "h3"},   // remote newer → need
		{Username: "carol", UpdatedAtUs: 50, OriginNode: "node-a", Hash: "h4"},  // new user → need
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(need) != 2 {
		t.Fatalf("expected 2 users needing full sync, got %d: %v", len(need), need)
	}
	// bob and carol should be in the list
	names := map[string]bool{}
	for _, n := range need {
		names[n] = true
	}
	if !names["bob"] || !names["carol"] {
		t.Errorf("expected bob and carol, got %v", need)
	}
}
