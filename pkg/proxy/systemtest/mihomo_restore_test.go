//go:build integration

package systemtest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestMihomoRestore_RecoversInboundAndUser validates that after a
// container Stop + Start cycle — the code path every ContainerMgr.StartAll
// invocation takes after a process restart — the mihomo listener reappears,
// the user's forward port is still valid, and end-to-end connectivity
// through the proxy chain is restored.
//
// This is a coarser-grained integration of stage 6's Restore + stage 5's
// reconcileUsers: those have unit coverage already, but this test exercises
// them against a real mihomo binary + a real xray client + a real store +
// a real ForwardManager, which is where the reconcile loop's assumptions
// about production wiring pay off.
//
// We don't hard-kill the mihomo process on purpose: the goal is to validate
// the v2raymg-initiated restart path, not the mihomo-crash-recovery path
// (which has no auto-restart today anyway — operators use the Restart HTTP
// API after a crash, and that API goes through Stop + Start).
func TestMihomoRestore_RecoversInboundAndUser(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	const marker = "MIHOMO_RESTORE_OK"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(marker))
	}))
	defer origin.Close()

	tmpDir := t.TempDir()
	mihomoBin := ensureMihomoBin(t, tmpDir)
	rig := startMihomoContainer(t, tmpDir, mihomoBin)

	// Set up a vmess inbound + user (vmess is the cheapest MVP protocol to
	// instantiate and has the simplest xray-client outbound).
	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc inbound port: %v", err)
	}
	tag := fmt.Sprintf("restore-vmess-%d", inboundPort)
	sharedUUID := uuid.NewString()
	if err := rig.container.FastAddInbound(tag, map[string]any{
		"protocol": "vmess",
		"port":     inboundPort,
		"uuid":     sharedUUID,
	}); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 10*time.Second); err != nil {
		t.Fatalf("mihomo listener not ready pre-restart: %v", err)
	}

	username := fmt.Sprintf("alice-restore-%d", inboundPort)
	forwardPortBefore := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))
	t.Logf("pre-restart: user %s bound to forward port %d -> 127.0.0.1:%d",
		username, forwardPortBefore, inboundPort)

	// Sanity: actually talk through the proxy chain once before restart, so
	// a post-restart failure means "restart broke something" rather than
	// "never worked to begin with".
	socksPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc socks port pre-restart: %v", err)
	}
	runRestoreHandshake(t, xrayBin, tmpDir, "pre", sharedUUID, int(forwardPortBefore), socksPort, origin.URL, marker)

	// Restart the container. The Stop→Start cycle is what
	// ContainerMgr.StartAll would do after a process-level restart: Start's
	// run hook replays the store, reconcileUsers reconciles the user map,
	// periodic reconcileDrift is re-armed.
	t.Logf("restarting mihomo container (Stop → Start)")
	if err := rig.container.Stop(); err != nil {
		// Stop can error if the previous process already exited — not
		// fatal, the Start below is what we actually care about.
		t.Logf("mihomo Stop returned (non-fatal): %v", err)
	}
	if err := rig.container.Start(); err != nil {
		t.Fatalf("mihomo Start after Stop: %v", err)
	}

	// Defence-in-depth readiness wait on the external-controller API.
	// Start already blocks on /version, but a restart that silently misses
	// this assertion would surface as a cryptic handshake failure below.
	if err := waitPort(rig.apiAddr, 10*time.Second); err != nil {
		t.Fatalf("external-controller not reachable post-restart: %v", err)
	}

	// Also wait for the mihomo listener to re-appear — Start triggers
	// restoreAndPushInbounds, which PUTs the listener config back.
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 10*time.Second); err != nil {
		t.Fatalf("mihomo listener not ready post-restart: %v", err)
	}

	// Port stability: PortMappings in contracts.UserSpec is the explicit
	// promise that (user, dstPort) → forwardPort survives restart. A
	// regression that lost this would surface here.
	forwardPortAfter, ok := rig.userMgr.GetUserPortByDst(username, uint32(inboundPort))
	if !ok {
		t.Fatalf("user %s lost its forward port after restart", username)
	}
	if forwardPortAfter != forwardPortBefore {
		t.Fatalf("forward port drift: before=%d after=%d (PortMappings regression)",
			forwardPortBefore, forwardPortAfter)
	}

	// End-to-end: new xray client (old one was in its own Cleanup; start a
	// fresh one against the same forwardPort, confirm full chain).
	socksPort2, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc socks port post-restart: %v", err)
	}
	runRestoreHandshake(t, xrayBin, tmpDir, "post", sharedUUID, int(forwardPortAfter), socksPort2, origin.URL, marker)
	t.Logf("PASS: full chain recovered after restart (forward port stable at %d)", forwardPortAfter)
}

// runRestoreHandshake spins up a short-lived xray client, pointed at the
// given forward port, sends a single GET through the chain, and fails the
// test unless the origin marker comes back. label is a tag for log output
// (e.g. "pre"/"post") so post-restart failures are obvious in the log.
func runRestoreHandshake(
	t *testing.T,
	xrayBin, tmpDir, label, uuidStr string,
	forwardPort, socksPort int,
	targetURL, marker string,
) {
	t.Helper()

	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []map[string]any{{
			"tag": "socks-in", "protocol": "socks",
			"listen": "127.0.0.1", "port": socksPort,
			"settings": map[string]any{"udp": false},
		}},
		"outbounds": []map[string]any{{
			"tag": "proxy", "protocol": "vmess",
			"settings": map[string]any{
				"vnext": []map[string]any{{
					"address": "127.0.0.1", "port": forwardPort,
					"users": []map[string]any{{
						"id":       uuidStr,
						"security": "auto",
						"alterId":  0,
					}},
				}},
			},
		}},
	}
	cfgPath := filepath.Join(tmpDir, fmt.Sprintf("client-restore-%s-%d.json", label, socksPort))
	if err := writeJSONConfig(cfgPath, cfg); err != nil {
		t.Fatalf("%s: write client config: %v", label, err)
	}

	// Short-lived subprocess scoped to this handshake, so the "pre" client
	// is fully torn down before "post" starts — avoids a lingering SOCKS5
	// listener clinging to a stale forward port.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, xrayBin, "run", "-c", cfgPath)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("%s: start xray client: %v", label, err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitPort(socksAddr, 10*time.Second); err != nil {
		t.Fatalf("%s: xray client socks port %d not ready: %v", label, socksPort, err)
	}

	httpClient, err := httpClientViaSocks5(socksAddr)
	if err != nil {
		t.Fatalf("%s: httpClientViaSocks5: %v", label, err)
	}
	resp, err := httpClient.Get(targetURL)
	if err != nil {
		t.Fatalf("%s: proxied GET: %v", label, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: unexpected status %d", label, resp.StatusCode)
	}
	if !strings.Contains(string(body), marker) {
		t.Fatalf("%s: missing marker %q in response: %q", label, marker, body)
	}
}
