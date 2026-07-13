package mihomo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// TestRemoveInboundConfig_KeepsCertFileSharedBySibling is the regression anchor
// for finding CTR-#53: writePEMToScratch names PEM cert files by content digest,
// so two inbounds with identical PEM share one pem-<digest> pair. Phase 5 of
// RemoveInboundConfig used to os.Remove those files unconditionally, destroying
// the surviving inbound's cert.
//
// Two trojan inbounds share the same CertFile/KeyFile (cert_source=pem).
// Removing the first must NOT delete the shared files (sibling still uses them);
// removing the last holder then cleans them up — so the fix is a survivor scan,
// not a blanket "never delete".
func TestRemoveInboundConfig_KeepsCertFileSharedBySibling(t *testing.T) {
	c := newTestContainer(t)
	srv := newCapturingRESTServer(t, nil)
	attachRunningREST(c, srv.URL())

	dir := t.TempDir()
	certFile := filepath.Join(dir, "pem-deadbeef-cert.pem")
	keyFile := filepath.Join(dir, "pem-deadbeef-key.pem")
	if err := os.WriteFile(certFile, []byte("CERT"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("KEY"), 0600); err != nil {
		t.Fatal(err)
	}

	port := uint32(10001)
	for _, tag := range []string{"trojan-a", "trojan-b"} {
		inb := NewMihomoInbound(tag, contracts.Protocol("trojan"), port, MihomoSharedCred{
			Password:   "p",
			CertSource: "pem",
			CertFile:   certFile,
			KeyFile:    keyFile,
		})
		c.inboundsMu.Lock()
		c.inbounds[tag] = inb
		c.inboundsMu.Unlock()
		if err := c.persistInbound(inb); err != nil {
			t.Fatalf("persist %s: %v", tag, err)
		}
		port++
	}

	// Remove the first holder — sibling trojan-b still references the pair.
	if err := c.RemoveInboundConfig("trojan-a"); err != nil {
		t.Fatalf("RemoveInboundConfig(trojan-a): %v", err)
	}
	if _, err := os.Stat(certFile); err != nil {
		t.Errorf("shared cert file wrongly deleted while sibling still uses it: %v", err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Errorf("shared key file wrongly deleted while sibling still uses it: %v", err)
	}

	// Remove the last holder — now nothing references the pair, so it is cleaned
	// up. This guards against an over-conservative "never delete" regression.
	if err := c.RemoveInboundConfig("trojan-b"); err != nil {
		t.Fatalf("RemoveInboundConfig(trojan-b): %v", err)
	}
	if _, err := os.Stat(certFile); !os.IsNotExist(err) {
		t.Errorf("cert file should be removed after the last holder is gone, stat err=%v", err)
	}
	if _, err := os.Stat(keyFile); !os.IsNotExist(err) {
		t.Errorf("key file should be removed after the last holder is gone, stat err=%v", err)
	}
}
