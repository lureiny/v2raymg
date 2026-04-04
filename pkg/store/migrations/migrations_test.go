package migrations_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

func TestAll_VersionSequential(t *testing.T) {
	for i, m := range migrations.All {
		expected := i + 1
		if m.Version != expected {
			t.Errorf("migration[%d].Version = %d, want %d", i, m.Version, expected)
		}
	}
}

func TestAll_SQLNonEmpty(t *testing.T) {
	for i, m := range migrations.All {
		if strings.TrimSpace(m.SQL) == "" {
			t.Errorf("migration[%d] (version %d) has empty SQL", i, m.Version)
		}
	}
}

func TestAll_Count(t *testing.T) {
	if len(migrations.All) != 11 {
		t.Errorf("expected 11 migrations, got %d", len(migrations.All))
	}
}

func TestVersion6_AlterColumns(t *testing.T) {
	m := migrations.All[5] // version 6
	if !strings.Contains(m.SQL, "role") {
		t.Error("version 6 should contain role column")
	}
	if !strings.Contains(m.SQL, "login_password") {
		t.Error("version 6 should contain login_password column")
	}
}

func TestVersion6_DefaultValues(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Apply only v1-5 first, then insert a user, then apply v6.
	if err := store.Migrate(db, migrations.All[:5]); err != nil {
		t.Fatalf("Migrate v1-5: %v", err)
	}
	if _, err := db.DB().Exec(
		`INSERT INTO users (username, password) VALUES ('legacy', 'pass')`,
	); err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}
	if err := store.Migrate(db, migrations.All); err != nil {
		t.Fatalf("Migrate v6: %v", err)
	}

	var role, loginPassword string
	row := db.DB().QueryRow(`SELECT role, login_password FROM users WHERE username = 'legacy'`)
	if err := row.Scan(&role, &loginPassword); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if role != "normal" {
		t.Errorf("expected default role='normal', got %q", role)
	}
	if loginPassword != "" {
		t.Errorf("expected default login_password='', got %q", loginPassword)
	}
}

func TestVersion5_AlterTableStatements(t *testing.T) {
	m := migrations.All[4] // version 5
	if !strings.Contains(m.SQL, "traffic_total_uplink") {
		t.Error("version 5 should contain traffic_total_uplink ALTER")
	}
	if !strings.Contains(m.SQL, "traffic_total_downlink") {
		t.Error("version 5 should contain traffic_total_downlink ALTER")
	}
}

// ---------------------------------------------------------------------------
// v7-v11: ClusterUser schema tests
// ---------------------------------------------------------------------------

func openMigratedDB(t *testing.T, upTo int) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, migrations.All[:upTo]); err != nil {
		t.Fatalf("Migrate to v%d: %v", upTo, err)
	}
	return db
}

func tableExists(t *testing.T, db *store.DB, table string) bool {
	t.Helper()
	var name string
	err := db.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
	).Scan(&name)
	return err == nil && name == table
}

func indexExists(t *testing.T, db *store.DB, idx string) bool {
	t.Helper()
	var name string
	err := db.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx,
	).Scan(&name)
	return err == nil && name == idx
}

// TestVersion7_CreateClusterUsersTable: v7 creates cluster_users with expected columns
func TestVersion7_CreateClusterUsersTable(t *testing.T) {
	db := openMigratedDB(t, 7)

	if !tableExists(t, db, "cluster_users") {
		t.Fatal("cluster_users table should exist after v7")
	}

	// Verify key columns by inserting a minimal record
	_, err := db.DB().Exec(
		`INSERT INTO cluster_users
		 (username, password, updated_at_us, origin_node, hash, created_at, updated_at)
		 VALUES ('test-user', 'pw', 1000, 'node-1', 'h1', 0, 0)`,
	)
	if err != nil {
		t.Fatalf("insert into cluster_users failed: %v (column may be missing)", err)
	}
}

// TestVersion7_DefaultValues: default values for nullable columns
func TestVersion7_DefaultValues(t *testing.T) {
	db := openMigratedDB(t, 7)

	_, err := db.DB().Exec(
		`INSERT INTO cluster_users
		 (username, password, updated_at_us, origin_node, hash, created_at, updated_at)
		 VALUES ('def-user', 'pw', 1000, 'node-1', 'h1', 0, 0)`,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var role, targetGroup string
	var expire, deleted int64
	row := db.DB().QueryRow(
		`SELECT role, target_group, expire, deleted FROM cluster_users WHERE username='def-user'`,
	)
	if err := row.Scan(&role, &targetGroup, &expire, &deleted); err != nil {
		t.Fatalf("scan defaults: %v", err)
	}
	if role != "normal" {
		t.Errorf("expected default role='normal', got %q", role)
	}
	if targetGroup != "default" {
		t.Errorf("expected default target_group='default', got %q", targetGroup)
	}
	if expire != 0 {
		t.Errorf("expected default expire=0, got %d", expire)
	}
	if deleted != 0 {
		t.Errorf("expected default deleted=0, got %d", deleted)
	}
}

// TestVersion8To10_CreateClusterUsersIndexes: v8-v10 create the three indexes
func TestVersion8To10_CreateClusterUsersIndexes(t *testing.T) {
	db := openMigratedDB(t, 10)

	for _, idx := range []string{
		"idx_cluster_users_target_group",
		"idx_cluster_users_updated_at_us",
		"idx_cluster_users_group_deleted",
	} {
		if !indexExists(t, db, idx) {
			t.Errorf("expected index %q to exist after v10", idx)
		}
	}
}

// TestVersion11_CreateLocalNodeGroupsTable: v11 creates local_node_groups
func TestVersion11_CreateLocalNodeGroupsTable(t *testing.T) {
	db := openMigratedDB(t, 11)

	if !tableExists(t, db, "local_node_groups") {
		t.Fatal("local_node_groups table should exist after v11")
	}

	// Verify insert/read round-trip
	_, err := db.DB().Exec(
		`INSERT INTO local_node_groups (group_name, created_at, updated_at) VALUES ('grp-a', 0, 0)`,
	)
	if err != nil {
		t.Fatalf("insert into local_node_groups: %v", err)
	}
	var name string
	if err := db.DB().QueryRow(`SELECT group_name FROM local_node_groups WHERE group_name='grp-a'`).Scan(&name); err != nil {
		t.Fatalf("read back local_node_groups: %v", err)
	}
	if name != "grp-a" {
		t.Errorf("expected 'grp-a', got %q", name)
	}
}

// TestClusterUserMigrations_UpgradeFromLegacyDB: apply v1-6 first, insert legacy data,
// then upgrade to full v11 — legacy data coexists with new tables.
func TestClusterUserMigrations_UpgradeFromLegacyDB(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Apply legacy migrations (v1-v6)
	if err := store.Migrate(db, migrations.All[:6]); err != nil {
		t.Fatalf("Migrate v1-6: %v", err)
	}

	// Insert a legacy user
	if _, err := db.DB().Exec(
		`INSERT INTO users (username, password) VALUES ('legacy-user', 'pw')`,
	); err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}

	// Upgrade to full migrations (v7-v11)
	if err := store.Migrate(db, migrations.All); err != nil {
		t.Fatalf("Migrate to v11: %v", err)
	}

	// Legacy user still readable
	var username string
	if err := db.DB().QueryRow(`SELECT username FROM users WHERE username='legacy-user'`).Scan(&username); err != nil {
		t.Fatalf("legacy user not found after upgrade: %v", err)
	}

	// New tables exist
	if !tableExists(t, db, "cluster_users") {
		t.Error("cluster_users should exist after full upgrade")
	}
	if !tableExists(t, db, "local_node_groups") {
		t.Error("local_node_groups should exist after full upgrade")
	}

	// New tables writable
	if _, err := db.DB().Exec(
		`INSERT INTO cluster_users
		 (username, password, updated_at_us, origin_node, hash, created_at, updated_at)
		 VALUES ('new-user', 'pw', 1000, 'node-1', 'h1', 0, 0)`,
	); err != nil {
		t.Fatalf("insert cluster_users after upgrade: %v", err)
	}
}
