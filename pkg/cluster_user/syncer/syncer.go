// Package syncer handles delta-sync of ClusterUser records between nodes.
// It compares remote UserDigests against the local store and determines which
// users require the remote node to send their full payload.
package syncer

import (
	"fmt"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
	clusteruserstore "github.com/lureiny/v2raymg/pkg/cluster_user/store"
	"github.com/lureiny/v2raymg/pkg/cluster_user/version"
	"github.com/lureiny/v2raymg/pkg/log"
)

// Syncer handles incoming digest comparisons and full-payload upserts from remote nodes.
type Syncer struct {
	store    clusteruserstore.ClusterUserStore
	nodeName string
}

// NewSyncer creates a new Syncer.
func NewSyncer(store clusteruserstore.ClusterUserStore, nodeName string) *Syncer {
	return &Syncer{
		store:    store,
		nodeName: nodeName,
	}
}

// CompareDigests receives the remote node's digest list and returns the usernames
// for which we want the remote to send us the full ClusterUser payload.
//
// We request a full payload when:
//   - The user does not exist locally at all.
//   - The remote version is strictly newer (by version.IsNewer rules).
//   - The versions are equivalent but the hash differs (data inconsistency).
//
// On individual DB read failures the affected user is still added to needFull
// (self-healing: we request the full record so we can overwrite a potentially
// corrupted local copy). A non-nil error is returned to signal that some lookups
// failed, but the needFull list is still usable.
func (s *Syncer) CompareDigests(remoteDigests []clusteruser.UserDigest) ([]string, error) {
	var needFull []string
	var dbErrors int

	for _, rd := range remoteDigests {
		local, err := s.store.Get(rd.Username)
		if err != nil {
			dbErrors++
			log.Error("syncer: failed to get local user",
				"username", rd.Username, "err", err)
			// On error, request the full record so we can recover.
			needFull = append(needFull, rd.Username)
			continue
		}

		if local == nil {
			// User not known locally — ask for full record.
			needFull = append(needFull, rd.Username)
			continue
		}

		// Build a synthetic ClusterUser from the remote digest for version comparison.
		remote := &clusteruser.ClusterUser{
			Username:    rd.Username,
			UpdatedAtUs: rd.UpdatedAtUs,
			OriginNode:  rd.OriginNode,
			Deleted:     rd.Deleted,
			Hash:        rd.Hash,
		}

		if version.IsNewer(remote, local) {
			// Remote is strictly newer — we need the full payload.
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
		retErr = fmt.Errorf("syncer: %d/%d digest comparisons hit DB errors", dbErrors, len(remoteDigests))
	}
	return needFull, retErr
}

// UpsertFromRemote writes the full ClusterUser records received from a remote node.
// Version arbitration is applied: we only store the record if the remote version is
// strictly newer than whatever we already hold (or if the user is new).
func (s *Syncer) UpsertFromRemote(users []*clusteruser.ClusterUser) error {
	var failures int
	for _, u := range users {
		if u == nil || u.Username == "" {
			continue
		}

		existing, err := s.store.Get(u.Username)
		if err != nil {
			failures++
			log.Error("syncer: failed to get existing user for upsert",
				"username", u.Username, "err", err)
			continue
		}

		if existing != nil && !version.IsNewer(u, existing) {
			// Our local copy is at least as new — skip.
			log.Debug("syncer: skipping upsert, local version is current or newer",
				"username", u.Username,
				"local_us", existing.UpdatedAtUs, "remote_us", u.UpdatedAtUs)
			continue
		}

		if err := s.store.Upsert(u); err != nil {
			failures++
			log.Error("syncer: failed to upsert user from remote",
				"username", u.Username, "err", err)
		}
	}
	if failures > 0 {
		return fmt.Errorf("syncer: %d/%d upserts failed", failures, len(users))
	}
	return nil
}
