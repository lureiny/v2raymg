package sync

import (
	"crypto/sha256"
	"fmt"
	"strconv"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// ComputeHash computes a SHA-256 digest over the canonical fields of u.
// The digest includes UpdatedAtUs so that two records with identical user
// attributes but different versions produce different hashes, enabling conflict
// detection during cluster synchronisation. OriginNode is intentionally
// excluded so that the same logical update originating from different nodes
// does not trigger unnecessary full syncs.
//
// The encoding is wire-compatible with the old pkg/cluster_user/hash package:
//   - ExpiryTime is converted to Unix seconds (int64), matching ClusterUser.Expire
//   - DeletionState is mapped to "true"/"false", matching ClusterUser.Deleted (bool)
//   - Each field is written with a 4-byte big-endian length prefix
//
// The returned string is a lowercase hex-encoded SHA-256 digest.
func ComputeHash(u *contracts.User) string {
	deleted := "false"
	if u.IsDeleting() {
		deleted = "true"
	}

	// Convert ExpiryTime to Unix seconds for backward compatibility.
	var expireStr string
	if u.ExpiryTime.IsZero() {
		expireStr = "0"
	} else {
		expireStr = strconv.FormatInt(u.ExpiryTime.Unix(), 10)
	}

	h := sha256.New()
	writeField := func(s string) {
		b := []byte(s)
		l := uint32(len(b))
		h.Write([]byte{byte(l >> 24), byte(l >> 16), byte(l >> 8), byte(l)})
		h.Write(b)
	}
	writeField(u.Username)
	writeField(u.AuthToken)
	writeField(expireStr)
	writeField(u.Role)
	writeField(u.TargetGroup)
	writeField(deleted)
	writeField(strconv.FormatInt(u.UpdatedAtUs, 10))
	writeField(strconv.FormatInt(u.BandwidthUploadBps, 10))
	writeField(strconv.FormatInt(u.BandwidthDownloadBps, 10))
	writeField(strconv.Itoa(u.MaxClients))
	writeField(strconv.Itoa(u.ClientRecycleDelaySec))
	writeField(strconv.Itoa(u.ClientDrainSec))

	return fmt.Sprintf("%x", h.Sum(nil))
}
