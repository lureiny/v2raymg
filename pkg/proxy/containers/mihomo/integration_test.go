//go:build integration

package mihomo

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMihomoContainer_StartStop is an integration test that downloads a real
// mihomo binary, starts it, verifies the REST API is reachable, and stops it.
//
// Requires network access to api.github.com and github.com. Run with:
//
//	go test -tags=integration ./pkg/proxy/containers/mihomo/...
func TestMihomoContainer_StartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	dir := t.TempDir()

	// Pick a free port for the external-controller. There's a TOCTOU window
	// between Close() and mihomo's bind; acceptable for integration testing.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	cfg := MihomoConfig{
		BinaryPath:         filepath.Join(dir, "mihomo"),
		ConfigFilePath:     filepath.Join(dir, "mihomo.yaml"),
		DataDir:            filepath.Join(dir, "data"),
		ExternalController: fmt.Sprintf("127.0.0.1:%d", port),
		Secret:             "",
		ReleaseTag:         "Prerelease-Alpha",
		AutoDownload:       true,
	}

	c, err := NewMihomoContainer(cfg)
	if err != nil {
		t.Fatalf("NewMihomoContainer: %v", err)
	}

	t0 := time.Now()
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Logf("mihomo ready in %s, version=%q", time.Since(t0), c.Version())

	if !c.IsRunning() {
		t.Error("expected container to be running after Start")
	}
	if c.Version() == "" {
		t.Error("expected non-empty Version after Start")
	}

	if err := c.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if c.IsRunning() {
		t.Error("expected container to not be running after Stop")
	}

	info, err := os.Stat(cfg.BinaryPath)
	if err != nil {
		t.Fatalf("Stat binary: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("downloaded binary is not executable: %v", info.Mode())
	}
}
