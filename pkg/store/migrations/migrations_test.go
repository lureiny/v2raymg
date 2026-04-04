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
	if len(migrations.All) != 6 {
		t.Errorf("expected 6 migrations, got %d", len(migrations.All))
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
