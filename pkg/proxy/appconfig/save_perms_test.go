package appconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSaveToFile_Is0600AndLeavesNoTemp guards the P2 fix: the config embeds
// secrets (cluster/center tokens, jwt_secret, DNS credentials), so SaveToFile
// must write 0600 (not the old world-readable 0644) and atomically (no leftover
// temp file). Reverting to os.WriteFile(path,data,0644) fails the perm check.
func TestSaveToFile_Is0600AndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")

	cfg := &AppConfig{}
	cfg.EndNode.Cluster.Token = "super-secret-cluster-token-01"

	if err := SaveToFile(cfg, p); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}

	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("config perms = %#o, want 0600 (secrets must not be world-readable)", perm)
	}

	// Atomic write must not leave the temp file behind.
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(ents) != 1 {
		var names []string
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("expected only the config file, found %v (temp not cleaned up?)", names)
	}

	// Overwrite of an existing file must also end up 0600.
	if err := SaveToFile(cfg, p); err != nil {
		t.Fatalf("overwrite SaveToFile: %v", err)
	}
	fi2, _ := os.Stat(p)
	if perm := fi2.Mode().Perm(); perm != 0600 {
		t.Errorf("perms after overwrite = %#o, want 0600", perm)
	}
}
