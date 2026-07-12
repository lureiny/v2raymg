package certmgmtlego

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

// TestSaveCert_WritePhaseFailureLeavesLiveFilesIntact covers finding #1's real
// residual: a failure while committing the new files must not leave a persisted
// half-update. We inject a surgical failure by pre-creating the .key path as a
// directory, so the key rename fails; with the two-phase (stage-all-then-rename)
// implementation the crt is never renamed, so the previous live crt is intact.
func TestSaveCert_WritePhaseFailureLeavesLiveFilesIntact(t *testing.T) {
	base := t.TempDir()
	crtPath, keyPath, _, _ := certPaths(base, "example.com")
	if err := os.MkdirAll(filepath.Dir(crtPath), 0700); err != nil {
		t.Fatal(err)
	}
	// Pre-existing live cert (old value).
	if err := os.WriteFile(crtPath, []byte("OLD_CRT"), 0600); err != nil {
		t.Fatal(err)
	}
	// Make the key path a directory so its rename fails during commit.
	if err := os.MkdirAll(keyPath, 0700); err != nil {
		t.Fatal(err)
	}

	err := SaveCert(base, &domain.CertificateRecord{Domain: "example.com"},
		&domain.LegoResource{
			Domain:         "example.com",
			CertificatePEM: []byte("NEW_CRT"),
			PrivateKeyPEM:  []byte("NEW_KEY"),
		})
	if err == nil {
		t.Fatal("expected SaveCert to fail when the key path is a directory")
	}

	got, rerr := os.ReadFile(crtPath)
	if rerr != nil {
		t.Fatalf("read crt: %v", rerr)
	}
	if string(got) != "OLD_CRT" {
		t.Fatalf("live crt was overwritten by a failed SaveCert: got %q, want OLD_CRT", got)
	}
}

// TestSaveCert_NoResidualTmp verifies a successful save leaves no temp files and
// writes all four files.
func TestSaveCert_NoResidualTmp(t *testing.T) {
	base := t.TempDir()
	err := SaveCert(base, &domain.CertificateRecord{Domain: "example.com"},
		&domain.LegoResource{
			Domain:         "example.com",
			CertificatePEM: []byte("crt"),
			PrivateKeyPEM:  []byte("key"),
		})
	if err != nil {
		t.Fatalf("SaveCert: %v", err)
	}

	dir := filepath.Join(base, certsFolder)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	tmpCount, fileCount := 0, 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			tmpCount++
		} else {
			fileCount++
		}
	}
	if tmpCount != 0 {
		t.Errorf("residual temp files left behind: %d", tmpCount)
	}
	if fileCount != 4 {
		t.Errorf("expected 4 cert files, got %d", fileCount)
	}
}
