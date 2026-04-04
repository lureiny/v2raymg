// Package sync provides cluster synchronisation primitives for UserManager.
// It handles version comparison, hash computation, and delta-sync digest logic.
package sync

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// UserDigest is a lightweight version summary of a user, sent during heartbeat.
// Deletion state is encoded in the Hash (via ComputeHash) and does not need
// a separate field here.
type UserDigest struct {
	Username    string
	UpdatedAtUs int64
	OriginNode  string
	Hash        string
}

// UserGetter looks up a user by username, including tombstones.
// Returns (nil, nil) if the user does not exist.
type UserGetter func(username string) (*contracts.User, error)

// CompareDigests compares remote digests against local state and returns usernames
// for which the full payload should be requested.
//
// A full payload is requested when:
//   - The user does not exist locally.
//   - The remote version is strictly newer.
//   - Versions match but hashes differ (data inconsistency / self-healing).
//   - A DB error occurred looking up the local user (self-healing).
func CompareDigests(getLocal UserGetter, remoteDigests []UserDigest) ([]string, error) {
	var needFull []string
	var dbErrors int

	for _, rd := range remoteDigests {
		local, err := getLocal(rd.Username)
		if err != nil {
			dbErrors++
			log.Error("sync: failed to get local user",
				"username", rd.Username, "err", err)
			needFull = append(needFull, rd.Username)
			continue
		}

		if local == nil {
			needFull = append(needFull, rd.Username)
			continue
		}

		// Build a synthetic User from the remote digest for version comparison.
		remote := &contracts.User{}
		remote.Username = rd.Username
		remote.UpdatedAtUs = rd.UpdatedAtUs
		remote.OriginNode = rd.OriginNode
		remote.Hash = rd.Hash

		if IsNewer(remote, local) {
			needFull = append(needFull, rd.Username)
			continue
		}

		// Same version but hash mismatch — request full record.
		if remote.UpdatedAtUs == local.UpdatedAtUs &&
			remote.OriginNode == local.OriginNode &&
			remote.Hash != local.Hash {
			needFull = append(needFull, rd.Username)
		}
	}

	var retErr error
	if dbErrors > 0 {
		retErr = fmt.Errorf("sync: %d/%d digest comparisons hit DB errors", dbErrors, len(remoteDigests))
	}
	return needFull, retErr
}
