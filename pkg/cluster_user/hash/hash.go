package hash

import (
	"crypto/sha256"
	"fmt"
	"strconv"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
)

// ComputeHash computes a SHA-256 digest over the canonical fields of u.
// The digest includes updated_at_us so that two records with identical user
// attributes but different versions produce different hashes, enabling conflict
// detection during cluster synchronisation. origin_node is intentionally
// excluded so that the same logical update originating from different nodes
// does not trigger unnecessary full syncs.
//
// Each field is written with a 4-byte big-endian length prefix to prevent
// delimiter-collision attacks (e.g. username="a|b" vs username="a" password="b").
// The returned string is a lowercase hex-encoded SHA-256 digest.
func ComputeHash(u *clusteruser.ClusterUser) string {
	deleted := "false"
	if u.Deleted {
		deleted = "true"
	}

	h := sha256.New()
	writeField := func(s string) {
		b := []byte(s)
		// 4-byte big-endian length prefix
		l := uint32(len(b))
		h.Write([]byte{byte(l >> 24), byte(l >> 16), byte(l >> 8), byte(l)})
		h.Write(b)
	}
	writeField(u.Username)
	writeField(u.Password)
	writeField(strconv.FormatInt(u.Expire, 10))
	writeField(u.Role)
	writeField(u.TargetGroup)
	writeField(deleted)
	writeField(strconv.FormatInt(u.UpdatedAtUs, 10))

	return fmt.Sprintf("%x", h.Sum(nil))
}
