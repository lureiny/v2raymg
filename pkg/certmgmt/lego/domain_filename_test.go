package certmgmtlego

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDomainToFilename_NoTraversal guards the P2 fix: a cert "domain" containing
// path separators or ".." must not escape the certs directory, while legitimate
// domains keep their previous (stable) filename so existing cert files remain
// addressable.
func TestDomainToFilename_NoTraversal(t *testing.T) {
	// Legitimate domains must be unchanged vs the old '*'->'_' behavior.
	stable := map[string]string{
		"example.com":            "example.com",
		"*.example.com":          "_.example.com",
		"sub-domain.example.com": "sub-domain.example.com",
	}
	for in, want := range stable {
		if got := domainToFilename(in); got != want {
			t.Errorf("domainToFilename(%q) = %q, want %q (legit domain must be stable)", in, got, want)
		}
	}

	// Traversal / separators must never survive.
	for _, mal := range []string{"../../etc/passwd", "a/b/c", "..", ".", "a\\b", "foo/../bar", ""} {
		got := domainToFilename(mal)
		if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
			t.Errorf("domainToFilename(%q) = %q still contains a separator or traversal", mal, got)
		}
	}

	// The joined cert path must stay inside <base>/certs.
	base := t.TempDir()
	crt, _, _, _ := certPaths(base, "../../../etc/passwd")
	certDir := filepath.Join(base, certsFolder)
	if !strings.HasPrefix(filepath.Clean(crt), filepath.Clean(certDir)+string(filepath.Separator)) {
		t.Errorf("certPaths escaped the certs dir: crt=%q not under %q", crt, certDir)
	}
}
