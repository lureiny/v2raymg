// Package bootstrap handles the one-time import of local usermgr users into the
// cluster_users store when the store is empty. It also ensures the local node's
// group assignments are initialised.
package bootstrap

import (
	"context"
	"fmt"
	"time"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
	clusteruserstore "github.com/lureiny/v2raymg/pkg/cluster_user/store"
	"github.com/lureiny/v2raymg/pkg/cluster_user/hash"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// UserLister is the minimal interface required from usermgr (not modifying usermgr internals).
type UserLister interface {
	ListUsers() []*contracts.User
}

// Bootstrapper is responsible for importing local users into cluster_users when
// the store is empty (idempotent — does nothing when records already exist).
type Bootstrapper struct {
	clusterUserStore clusteruserstore.ClusterUserStore
	nodeGroupsStore  clusteruserstore.NodeGroupsStore
	userMgr          UserLister
	nodeName         string
	defaultGroup     string
}

// NewBootstrapper creates a Bootstrapper.
func NewBootstrapper(
	clusterUserStore clusteruserstore.ClusterUserStore,
	nodeGroupsStore clusteruserstore.NodeGroupsStore,
	userMgr UserLister,
	nodeName string,
	defaultGroup string,
) *Bootstrapper {
	if defaultGroup == "" {
		defaultGroup = "default"
	}
	return &Bootstrapper{
		clusterUserStore: clusterUserStore,
		nodeGroupsStore:  nodeGroupsStore,
		userMgr:          userMgr,
		nodeName:         nodeName,
		defaultGroup:     defaultGroup,
	}
}

// Bootstrap executes the bootstrap logic (idempotent):
//  1. Reads local_node_groups; if empty, writes defaultGroup.
//  2. Checks whether cluster_users is empty.
//  3. If empty and userMgr != nil, imports all local users with sensible defaults.
func (b *Bootstrapper) Bootstrap(ctx context.Context) error {
	// Step 1: ensure node groups are initialised.
	groups, err := b.nodeGroupsStore.List()
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		log.Info("bootstrap: node groups empty, setting default group", "group", b.defaultGroup)
		if err := b.nodeGroupsStore.Set([]string{b.defaultGroup}); err != nil {
			return err
		}
	}

	// Step 2: check cluster_users count.
	count, err := b.clusterUserStore.Count()
	if err != nil {
		return err
	}
	if count > 0 {
		log.Debug("bootstrap: cluster_users not empty, skipping import", "count", count)
		return nil
	}

	// Step 3: import from usermgr.
	if b.userMgr == nil {
		log.Debug("bootstrap: userMgr is nil, skipping import")
		return nil
	}

	users := b.userMgr.ListUsers()
	if len(users) == 0 {
		log.Debug("bootstrap: no local users to import")
		return nil
	}

	log.Info("bootstrap: importing local users into cluster_users", "count", len(users))

	// Use a monotonically-increasing base timestamp so every imported user has a
	// unique UpdatedAtUs value even when they are imported in the same microsecond.
	baseUs := time.Now().UnixMicro()

	imported := 0
	for i, u := range users {
		if u == nil || u.Username == "" {
			continue
		}

		// Compute absolute expiry from ExpiryTime (time.Time).
		var expire int64
		if !u.ExpiryTime.IsZero() {
			expire = u.ExpiryTime.Unix()
		}

		targetGroup := b.defaultGroup

		role := u.Role
		if role == "" {
			role = "normal"
		}

		cu := &clusteruser.ClusterUser{
			Username:    u.Username,
			Password:    u.Password,
			Expire:      expire,
			Role:        role,
			TargetGroup: targetGroup,
			Deleted:     false,
			UpdatedAtUs: baseUs + int64(i),
			OriginNode:  b.nodeName,
		}
		cu.Hash = hash.ComputeHash(cu)

		if err := b.clusterUserStore.Upsert(cu); err != nil {
			// Return error so that Count() remains 0 and bootstrap will be
			// retried on next startup instead of leaving a partial import.
			return fmt.Errorf("bootstrap: failed to upsert user %q: %w", u.Username, err)
		}
		imported++
	}

	log.Info("bootstrap: import complete", "imported", imported)
	return nil
}
