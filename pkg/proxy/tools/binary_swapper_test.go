package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSwapAtomic_TargetMissing_NoFakeBackup covers finding #3: when the target
// binary does not exist (first install), SwapAtomic must return an empty backup
// path — not a non-existent ".bak" — so a later Rollback does not strand the
// freshly-installed binary.
func TestSwapAtomic_TargetMissing_NoFakeBackup(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "app") // does NOT exist
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := NewBinarySwapper()
	backup, err := s.SwapAtomic(bin, src)
	if err != nil {
		t.Fatalf("SwapAtomic: %v", err)
	}
	if backup != "" {
		t.Fatalf("backup path must be empty when the target did not exist, got %q", backup)
	}
	if b, _ := os.ReadFile(bin); string(b) != "NEW" {
		t.Fatal("new binary was not installed at the target path")
	}

	// Rollback with the (empty) backup must NOT destroy the installed binary.
	_ = s.Rollback(bin, backup) // returns an error, but must preserve the binary
	if b, err := os.ReadFile(bin); err != nil || string(b) != "NEW" {
		t.Fatalf("rollback stranded the binary: err=%v content=%q", err, string(b))
	}
	if _, err := os.Stat(bin + ".new"); err == nil {
		t.Error("binary was moved to .new and stranded")
	}
}

// TestSwapAtomic_RollbackRestoresOld guards the happy path: with a real previous
// binary, Rollback restores it.
func TestSwapAtomic_RollbackRestoresOld(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "app")
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}

	s := NewBinarySwapper()
	backup, err := s.SwapAtomic(bin, src)
	if err != nil || backup == "" {
		t.Fatalf("expected a real backup path, err=%v backup=%q", err, backup)
	}
	if b, _ := os.ReadFile(bin); string(b) != "NEW" {
		t.Fatal("new binary not installed")
	}
	if err := s.Rollback(bin, backup); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if b, _ := os.ReadFile(bin); string(b) != "OLD" {
		t.Fatalf("rollback did not restore the old binary, got %q", string(b))
	}
}
