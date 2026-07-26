//go:build integration

package systemtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/containers/mihomo"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/store"
	"github.com/lureiny/v2raymg/pkg/store/migrations"
)

// ensureMihomoBin returns a path to a usable mihomo binary for the current
// integration test. Resolution order:
//
//  1. MIHOMO_BIN env var — if set and the file exists, returned as-is.
//     If set but the file is missing, the test fails fast (misconfigured env
//     is never a valid reason to silently fall through to a download).
//  2. Otherwise the mihomo Updater is used to download the Alpha pre-release
//     into tmpDir. Alpha is chosen deliberately (stage 2 validated this path
//     end-to-end and Alpha is the only mihomo release that ships a
//     checksums.txt, which exercises the SHA256 verify step).
//
// Network is required on path 2. If the caller cannot rely on network (CI
// without egress), set MIHOMO_BIN in CI to avoid flakes.
func ensureMihomoBin(t *testing.T, tmpDir string) string {
	t.Helper()
	if env := os.Getenv("MIHOMO_BIN"); env != "" {
		if _, err := os.Stat(env); err != nil {
			t.Fatalf("MIHOMO_BIN=%s set but not usable: %v", env, err)
		}
		t.Logf("using pre-installed mihomo binary: %s", env)
		return env
	}

	binPath := filepath.Join(tmpDir, "mihomo")
	u, err := mihomo.NewUpdater(mihomo.UpdaterConfig{
		BinaryPath:  binPath,
		DownloadDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("NewUpdater: %v", err)
	}
	// 3 min budget: Alpha .gz is ~20 MiB + checksums.txt + gunzip; wide
	// enough to tolerate slow CI networks without being open-ended.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := u.Update(ctx, container.UpdateRequest{
		TargetTag:     "Prerelease-Alpha",
		RestartPolicy: container.RestartPolicyNever,
	}); err != nil {
		t.Fatalf("download mihomo Alpha via Updater: %v", err)
	}
	t.Logf("downloaded mihomo Alpha to %s", binPath)
	return binPath
}

// mihomoTestRig bundles the collaborators a protocol test needs. Lifetime
// cleanup is registered with t via Cleanup in startMihomoContainer; callers
// just use the rig and return.
type mihomoTestRig struct {
	container  *mihomo.MihomoContainer
	userMgr    *usermanager.UserManager
	forwardMgr forward.ForwardManager
	apiAddr    string // external-controller host:port
	// dataDir is mihomo's -d argument (home directory equivalent). The
	// Alpha enforces a SAFE_PATHS rule: file references from the config
	// (e.g. trojan certificate/private-key paths) must live inside this
	// directory or they are rejected with "path is not subpath of home
	// directory". Tests that need to pass paths through to mihomo should
	// write them under this directory.
	dataDir string
}

// startMihomoContainer boots a real MihomoContainer against a real (in-process)
// ForwardManager + UserManager + SQLite store. Go through the container's own
// Start hook (not startProcess directly) — the integration test is meant to
// mirror production wiring.
//
// tmpDir must outlive the test (typically t.TempDir()). Cleanup stops the
// container, closes the forward manager, and closes the store.
func startMihomoContainer(t *testing.T, tmpDir, binaryPath string) *mihomoTestRig {
	t.Helper()

	// Ephemeral forward-port range — wide enough for multi-user tests, safely
	// above the 10000 floor used by production defaults so a leaked test
	// ForwardManager won't collide with a real one on the same host.
	fwdMgr, err := forward.NewDefaultForwardManager(forward.PortAllocatorConfig{
	// Port window deliberately BELOW the Linux ephemeral range (32768-60999).
	// PortAllocator does not ask the OS whether a port is free, so a pool that
	// overlaps the ephemeral range collides with outbound connections made by
	// other tests running in parallel — that was a real CI flake. AddRule now
	// retries past a collision, but tests should not depend on that.
		MinPort: 24000,
		MaxPort: 24999,
	})
	if err != nil {
		t.Fatalf("NewDefaultForwardManager: %v", err)
	}
	t.Cleanup(func() { _ = fwdMgr.Close() })

	um := usermanager.NewUserManager(fwdMgr, "mihomo-integration-test")

	dsn := filepath.Join(tmpDir, "mihomo-test.db")
	sm, err := store.NewStoreManager(dsn, migrations.All)
	if err != nil {
		t.Fatalf("NewStoreManager: %v", err)
	}
	t.Cleanup(func() { _ = sm.Close() })

	apiPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort for external-controller: %v", err)
	}
	apiAddr := fmt.Sprintf("127.0.0.1:%d", apiPort)

	dataDir := filepath.Join(tmpDir, "mihomo-data")
	cfg := mihomo.MihomoConfig{
		BinaryPath:         binaryPath,
		ConfigFilePath:     filepath.Join(tmpDir, "mihomo-config.yaml"),
		DataDir:            dataDir,
		ExternalController: apiAddr,
		Secret:             "",
		ReleaseTag:         "Prerelease-Alpha",
		// Binary already resolved; disable to prove we never fall back to a
		// GitHub fetch during the hot-path of the test.
		AutoDownload: false,
	}
	c, err := mihomo.NewMihomoContainer(cfg,
		mihomo.WithStoreMgr(sm),
		mihomo.WithUserManager(um),
	)
	if err != nil {
		t.Fatalf("NewMihomoContainer: %v", err)
	}

	if err := c.Start(); err != nil {
		t.Fatalf("mihomo Start: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Stop()
		c.Close()
	})

	// Defence-in-depth: Start already blocks on /version readiness via the
	// startProcess waitForVersion call, but the assertion here gives the
	// failure a cleaner signature if something regresses.
	if err := waitPort(apiAddr, 5*time.Second); err != nil {
		t.Fatalf("mihomo external-controller not reachable: %v", err)
	}

	return &mihomoTestRig{
		container:  c,
		userMgr:    um,
		forwardMgr: fwdMgr,
		apiAddr:    apiAddr,
		dataDir:    dataDir,
	}
}

// addMihomoUserAndWaitForPort adds a user through AddUser (triggering the real
// UserEventAdd → container handler → GetBindPort pipeline) then polls the
// forward-port map until the allocation lands, or fails the test. 5 s is
// generous; the UserEvent pipeline is fully in-process and usually resolves
// in <50 ms.
func addMihomoUserAndWaitForPort(
	t *testing.T,
	rig *mihomoTestRig,
	username string,
	inboundPort uint32,
) uint32 {
	t.Helper()
	if err := rig.userMgr.AddUser(usermanager.AddUserRequest{
		Username: username,
		Password: username + "-test-password",
	}); err != nil {
		t.Fatalf("AddUser(%s): %v", username, err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		port, ok := rig.userMgr.GetUserPortByDst(username, inboundPort)
		if ok && port != 0 {
			return port
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("user %q never got a forward port for inbound backend port %d within 5s",
		username, inboundPort)
	return 0
}

// removeMihomoUserAndWaitForPortRelease removes the user via the production
// pipeline (RemoveUser emits UserEventRemove → container → ReleaseBindPort)
// then polls until the rule is gone. Returns when the rule is confirmed
// released or fails the test after 5 s.
func removeMihomoUserAndWaitForPortRelease(
	t *testing.T,
	rig *mihomoTestRig,
	username, inboundTag string,
) {
	t.Helper()
	if err := rig.userMgr.RemoveUser(usermanager.RemoveUserRequest{Username: username}); err != nil {
		t.Fatalf("RemoveUser(%s): %v", username, err)
	}
	ruleKey := fmt.Sprintf("mihomo:%s:%s", inboundTag, username)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rig.forwardMgr.GetRule(ruleKey) == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("forward rule %q still present 5s after RemoveUser", ruleKey)
}
