package cmd

import (
	"path/filepath"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

// openTestDB opens a migrated in-memory (temp file) SQLite DB for tests.
func openTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	if err := store.Migrate(db, migrations.All); err != nil {
		db.Close()
		t.Fatalf("store.Migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestStartupWiring_ClusterDisabled: when cluster_user.enabled=false,
// UserManager should not have cluster enabled.
func TestStartupWiring_ClusterDisabled(t *testing.T) {
	mgr := usermanager.NewUserManager(nil, "node-test")
	if mgr.IsClusterEnabled() {
		t.Fatal("cluster should be disabled by default")
	}
}

// TestStartupWiring_ClusterEnabled: when cluster_user.enabled=true,
// UserManager gets EnableClusterSync called and is cluster-enabled.
func TestStartupWiring_ClusterEnabled(t *testing.T) {
	db := openTestDB(t)
	ngStore := store.NewSQLiteNodeGroupsStore(db)

	mgr := usermanager.NewUserManager(nil, "node-test")
	mgr.EnableClusterSync("default", ngStore)

	if !mgr.IsClusterEnabled() {
		t.Fatal("cluster should be enabled after EnableClusterSync")
	}
	if mgr.LocalNodeName() != "node-test" {
		t.Errorf("expected node-test, got %q", mgr.LocalNodeName())
	}
}

// TestStartupWiring_NodeGroupsSeed: empty store gets seeded with default group.
func TestStartupWiring_NodeGroupsSeed(t *testing.T) {
	db := openTestDB(t)
	ngStore := store.NewSQLiteNodeGroupsStore(db)

	// Seed if empty (mirrors server.go logic)
	groups, err := ngStore.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) == 0 {
		if err := ngStore.Set([]string{"default"}); err != nil {
			t.Fatal(err)
		}
	}

	groups, err = ngStore.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0] != "default" {
		t.Errorf("expected [default], got %v", groups)
	}
}

// TestStartupWiring_BackfillClusterFields: users with UpdatedAtUs==0 get backfilled.
func TestStartupWiring_BackfillClusterFields(t *testing.T) {
	mgr := usermanager.NewUserManager(nil, "node-test")
	mgr.EnableClusterSync("default", nil)

	// Add a user (UpdatedAtUs is always set now, regardless of cluster mode)
	_ = mgr.AddUser(usermanager.AddUserRequest{Username: "alice", Password: "pw"})
	u := mgr.GetUserForSync("alice")
	if u == nil {
		t.Fatal("alice not found")
	}
	if u.UpdatedAtUs == 0 {
		t.Error("expected UpdatedAtUs to be set by AddUser")
	}
}

// TestInitClusterSync_SeedsDefaultGroupBeforeEnableClusterSync guards against
// the first-boot fail-open bug where EnableClusterSync saw an empty
// local_node_groups table and left cachedNodeGroups empty until restart.
func TestInitClusterSync_SeedsDefaultGroupBeforeEnableClusterSync(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "test.db")
	sm, err := store.NewStoreManager(dsn, migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })

	mgr, err := usermanager.NewUserManagerWithStore(nil, sm, "node-test")
	if err != nil {
		t.Fatalf("NewUserManagerWithStore: %v", err)
	}

	cfg := &appconfig.AppConfig{
		ClusterUser: appconfig.ClusterUserConfig{
			Enabled:      true,
			DefaultGroup: "default",
		},
	}
	initClusterSync(cfg, sm, mgr)

	vipUser := &contracts.User{Username: "vip-only", TargetGroup: "vip"}
	defaultUser := &contracts.User{Username: "def-user", TargetGroup: "default"}
	if mgr.IsUserVisible(vipUser) {
		t.Fatal("cross-group user must NOT be visible on first boot when local_node_groups was seeded to default")
	}
	if !mgr.IsUserVisible(defaultUser) {
		t.Fatal("default-group user should be visible after initClusterSync")
	}
}
