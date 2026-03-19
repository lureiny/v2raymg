package migrations_test

import (
	"strings"
	"testing"

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
	if len(migrations.All) != 5 {
		t.Errorf("expected 5 migrations, got %d", len(migrations.All))
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
