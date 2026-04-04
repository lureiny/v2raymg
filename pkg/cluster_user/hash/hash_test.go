package hash_test

import (
	"testing"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
	"github.com/lureiny/v2raymg/pkg/cluster_user/hash"
)

func baseUser() *clusteruser.ClusterUser {
	return &clusteruser.ClusterUser{
		Username:    "alice",
		Password:    "secret",
		Expire:      0,
		Role:        "normal",
		TargetGroup: "default",
		Deleted:     false,
		UpdatedAtUs: 1000000,
		OriginNode:  "node-1",
	}
}

func TestComputeHash_Stable(t *testing.T) {
	u := baseUser()
	h1 := hash.ComputeHash(u)
	h2 := hash.ComputeHash(u)
	if h1 != h2 {
		t.Errorf("hash is not stable: %q != %q", h1, h2)
	}
}

func TestComputeHash_NonEmpty(t *testing.T) {
	u := baseUser()
	h := hash.ComputeHash(u)
	if h == "" {
		t.Error("expected non-empty hash")
	}
}

func TestComputeHash_HexFormat(t *testing.T) {
	u := baseUser()
	h := hash.ComputeHash(u)
	// SHA-256 hex is 64 characters
	if len(h) != 64 {
		t.Errorf("expected 64-char hex string, got len=%d: %q", len(h), h)
	}
	for _, c := range h {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex character in hash: %q", string(c))
		}
	}
}

func TestComputeHash_DifferentUsername(t *testing.T) {
	u1 := baseUser()
	u2 := baseUser()
	u2.Username = "bob"

	if hash.ComputeHash(u1) == hash.ComputeHash(u2) {
		t.Error("different usernames should produce different hashes")
	}
}

func TestComputeHash_DifferentPassword(t *testing.T) {
	u1 := baseUser()
	u2 := baseUser()
	u2.Password = "other"

	if hash.ComputeHash(u1) == hash.ComputeHash(u2) {
		t.Error("different passwords should produce different hashes")
	}
}

func TestComputeHash_DifferentExpire(t *testing.T) {
	u1 := baseUser()
	u2 := baseUser()
	u2.Expire = 9999999

	if hash.ComputeHash(u1) == hash.ComputeHash(u2) {
		t.Error("different expire values should produce different hashes")
	}
}

func TestComputeHash_DifferentDeleted(t *testing.T) {
	u1 := baseUser()
	u2 := baseUser()
	u2.Deleted = true

	if hash.ComputeHash(u1) == hash.ComputeHash(u2) {
		t.Error("different deleted values should produce different hashes")
	}
}

func TestComputeHash_DifferentRole(t *testing.T) {
	u1 := baseUser()
	u2 := baseUser()
	u2.Role = "admin"

	if hash.ComputeHash(u1) == hash.ComputeHash(u2) {
		t.Error("different roles should produce different hashes")
	}
}

func TestComputeHash_DifferentTargetGroup(t *testing.T) {
	u1 := baseUser()
	u2 := baseUser()
	u2.TargetGroup = "premium"

	if hash.ComputeHash(u1) == hash.ComputeHash(u2) {
		t.Error("different target_group values should produce different hashes")
	}
}

func TestComputeHash_DifferentUpdatedAtUs(t *testing.T) {
	u1 := baseUser()
	u2 := baseUser()
	u2.UpdatedAtUs = 2000000

	if hash.ComputeHash(u1) == hash.ComputeHash(u2) {
		t.Error("different updated_at_us values should produce different hashes")
	}
}

func TestComputeHash_SameWhenOriginNodeDiffers(t *testing.T) {
	u1 := baseUser()
	u2 := baseUser()
	u2.OriginNode = "node-2"

	if hash.ComputeHash(u1) != hash.ComputeHash(u2) {
		t.Error("origin_node is excluded from hash: different origin_node should produce the same hash")
	}
}
