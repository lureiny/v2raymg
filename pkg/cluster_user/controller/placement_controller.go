// Package controller provides the PlacementController which reconciles
// ClusterUser records from the cluster store into the local usermanager.
package controller

import (
	"sync"
	"time"

	clusteruser "github.com/lureiny/v2raymg/pkg/cluster_user"
	clusteruserstore "github.com/lureiny/v2raymg/pkg/cluster_user/store"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// UserMgrInterface is the minimal interface PlacementController needs from
// usermanager. It intentionally avoids importing the full usermanager package
// type so that callers can supply any compatible implementation.
type UserMgrInterface interface {
	ListUsers() []*contracts.User
	AddUser(req usermanager.AddUserRequest) error
	UpdateUser(username, password string, expireTime int64) error
	RemoveUser(req usermanager.RemoveUserRequest) error
}

// PlacementController reconciles ClusterUser records from the cluster stores
// into the local usermanager at a configurable interval.
type PlacementController struct {
	clusterUserStore clusteruserstore.ClusterUserStore
	nodeGroupsStore  clusteruserstore.NodeGroupsStore
	userMgr          UserMgrInterface
	defaultGroup     string
	interval         time.Duration
	stopCh           chan struct{}
	stopOnce         sync.Once
}

// New creates a new PlacementController.
// The controller is not started automatically; call Start() to begin periodic
// reconciliation.
func New(
	clusterUserStore clusteruserstore.ClusterUserStore,
	nodeGroupsStore clusteruserstore.NodeGroupsStore,
	userMgr UserMgrInterface,
	cfg appconfig.ClusterUserConfig,
) *PlacementController {
	interval := time.Duration(cfg.SyncIntervalSec) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	defaultGroup := cfg.DefaultGroup
	if defaultGroup == "" {
		defaultGroup = "default"
	}
	return &PlacementController{
		clusterUserStore: clusterUserStore,
		nodeGroupsStore:  nodeGroupsStore,
		userMgr:          userMgr,
		defaultGroup:     defaultGroup,
		interval:         interval,
		stopCh:           make(chan struct{}),
	}
}

// Start launches the background reconciliation loop.
// It is safe to call Start only once.
func (c *PlacementController) Start() {
	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.Reconcile(); err != nil {
					log.Error("placement controller reconcile", "err", err)
				}
			case <-c.stopCh:
				return
			}
		}
	}()
}

// Stop signals the background loop to exit. Safe to call multiple times.
func (c *PlacementController) Stop() {
	c.stopOnce.Do(func() { close(c.stopCh) })
}

// Reconcile performs a single reconcile pass:
//  1. Loads nodeGroups; falls back to [defaultGroup] when empty.
//  2. Builds a desired map of username → ClusterUser for users whose
//     target_group is in nodeGroups and who are not deleted.
//  3. Compares against the local usermanager state and issues Add / Update /
//     Remove operations as needed.
//
// Per-user errors are logged and skipped; the function only returns an error
// when it cannot fetch the initial data sets.
func (c *PlacementController) Reconcile() error {
	if c.userMgr == nil {
		return nil
	}

	// Step 1 – node groups
	nodeGroups, err := c.nodeGroupsStore.List()
	if err != nil {
		return err
	}
	if len(nodeGroups) == 0 {
		nodeGroups = []string{c.defaultGroup}
	}
	groupSet := make(map[string]struct{}, len(nodeGroups))
	for _, g := range nodeGroups {
		groupSet[g] = struct{}{}
	}

	// Safety guard: if cluster_users is completely empty, skip all removal
	// operations to avoid deleting local users before the store is populated
	// (e.g. fresh node, failed bootstrap, or pre-sync state).
	totalCount, err := c.clusterUserStore.Count()
	if err != nil {
		return err
	}
	if totalCount == 0 {
		log.Debug("placement controller: cluster_users empty, skipping reconcile to protect local users")
		return nil
	}

	// Step 2 – fetch cluster users filtered by node groups (avoids loading
	// users from unrelated groups into memory).
	desired := make(map[string]*clusteruser.ClusterUser)
	deletedUsers := make(map[string]*clusteruser.ClusterUser)
	for g := range groupSet {
		users, err := c.clusterUserStore.ListByGroup(g)
		if err != nil {
			return err
		}
		for _, cu := range users {
			if cu.Deleted {
				deletedUsers[cu.Username] = cu
				continue
			}
			desired[cu.Username] = cu
		}
	}

	// Step 3 – actual local users
	localUsers := c.userMgr.ListUsers()
	actualMap := make(map[string]*contracts.User, len(localUsers))
	for _, u := range localUsers {
		actualMap[u.Username] = u
	}

	// Step 4a – deleted=true AND local exists → Remove
	for username := range deletedUsers {
		if _, exists := actualMap[username]; exists {
			if err := c.userMgr.RemoveUser(usermanager.RemoveUserRequest{Username: username}); err != nil {
				log.Error("placement controller: remove deleted user", "username", username, "err", err)
			}
		}
	}

	// Step 4b – desired exists, actual does not → Add
	// Step 4c – desired exists, actual exists but stale → Update
	for username, cu := range desired {
		actual, exists := actualMap[username]
		if !exists {
			var ttl time.Duration
			if cu.Expire != 0 {
				ttl = time.Until(time.Unix(cu.Expire, 0))
				if ttl <= 0 {
					// User already expired — skip adding.
					log.Debug("placement controller: skip expired user", "username", username, "expire", cu.Expire)
					continue
				}
			}
			if err := c.userMgr.AddUser(usermanager.AddUserRequest{
				Username: username,
				Password: cu.Password,
				TTL:      ttl,
			}); err != nil {
				log.Error("placement controller: add user", "username", username, "err", err)
			}
			continue
		}

		// Compare password and expiry
		needsUpdate := false
		if actual.Password != cu.Password {
			needsUpdate = true
		}
		var actualExpire int64
		if !actual.ExpiryTime.IsZero() {
			actualExpire = actual.ExpiryTime.Unix()
		}
		if actualExpire != cu.Expire {
			needsUpdate = true
		}
		if needsUpdate {
			if err := c.userMgr.UpdateUser(username, cu.Password, cu.Expire); err != nil {
				log.Error("placement controller: update user", "username", username, "err", err)
			}
		}
	}

	// Step 4d – actual exists but not in desired (and not in deletedUsers already handled) → Remove
	for username := range actualMap {
		if _, inDesired := desired[username]; inDesired {
			continue
		}
		if _, inDeleted := deletedUsers[username]; inDeleted {
			// already handled in step 4a
			continue
		}
		// group mismatch or completely unknown to cluster store – remove locally
		if err := c.userMgr.RemoveUser(usermanager.RemoveUserRequest{Username: username}); err != nil {
			log.Error("placement controller: remove group-mismatch user", "username", username, "err", err)
		}
	}

	return nil
}
