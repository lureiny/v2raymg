//go:build integration

package systemtest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	fillparams "github.com/lureiny/v2raymg/pkg/proxy/core/params"
)

// defaultE2ETarget is the URL the positive-path GET hits after the SOCKS5→
// client→server→internet chain is established. User can override with
// MIHOMO_E2E_TARGET when the default host is unreachable (GFW, corporate
// proxy) — per the stage 11+ design decision, the test fails fast on an
// unreachable target instead of silently t.Skip'ing, because a silent skip
// would hide "mihomo broken" behind "network broken".
const defaultE2ETarget = "https://www.google.com"

// e2eCase describes one protocol slice of the test matrix. The server side
// goes through v2raymg's FastAddInbound (production path); the client side
// is a second, separately-launched mihomo process using a hand-written yaml.
// Both halves use the SAME mihomo binary — the whole point of this test is
// to prove that the v2raymg-managed mihomo server speaks the real mihomo
// wire protocol, not some v2raymg-only dialect.
type e2eCase struct {
	Name     string
	Protocol string
	// serverParams produces the map passed to FastAddInbound. Called once
	// per state transition so the "change credential" step gets fresh
	// credentials distinct from the positive-baseline ones.
	serverParams func(port int) map[string]any
	// clientProxy produces the mihomo yaml `proxies:` entry that points at
	// the allocated server forward port. Credentials MUST match whatever
	// serverParams handed to the currently-active inbound.
	clientProxy func(name string, params map[string]any, forwardPort int) map[string]any
}

func e2eCases() []e2eCase {
	return []e2eCase{
		{
			Name:     "vmess",
			Protocol: "vmess",
			serverParams: func(port int) map[string]any {
				return map[string]any{"uuid": uuid.NewString(), "port": port}
			},
			clientProxy: func(name string, params map[string]any, forwardPort int) map[string]any {
				uuidStr, _ := params["uuid"].(string)
				return map[string]any{
					"name":    name,
					"type":    "vmess",
					"server":  "127.0.0.1",
					"port":    forwardPort,
					"uuid":    uuidStr,
					"alterId": 0,
					"cipher":  "auto",
					// Plain TCP transport — matches the server side which
					// FastAddInbound wires up as a bare listener with no
					// TLS/ws/grpc extensions.
				}
			},
		},
		{
			Name:     "trojan",
			Protocol: "trojan",
			// Stage 11+ E2E confirmed P2-9: mihomo Alpha refuses to boot a
			// trojan listener without certs ("disallow using Trojan
			// without both certificates/reality/ss config"). cert_file
			// and key_file are injected by runE2EProtocolCase via
			// writeTempCert — keeping cert plumbing in the runner instead
			// of the case table avoids threading tmpDir through every
			// closure.
			serverParams: func(port int) map[string]any {
				return map[string]any{
					"password": "trojan-e2e-" + randHex(8),
					"port":     port,
				}
			},
			clientProxy: func(name string, params map[string]any, forwardPort int) map[string]any {
				pw, _ := params["password"].(string)
				return map[string]any{
					"name":     name,
					"type":     "trojan",
					"server":   "127.0.0.1",
					"port":     forwardPort,
					"password": pw,
					// writeTempCert generates a CN=localhost cert; SNI
					// must match so TLS handshake picks the right cert.
					"sni":              "localhost",
					"skip-cert-verify": true,
				}
			},
		},
		{
			Name:     "shadowsocks",
			Protocol: "shadowsocks",
			serverParams: func(port int) map[string]any {
				return map[string]any{
					"password": "ss-e2e-" + randHex(16),
					"cipher":   "aes-256-gcm",
					"port":     port,
				}
			},
			clientProxy: func(name string, params map[string]any, forwardPort int) map[string]any {
				method, _ := params["cipher"].(string)
				pw, _ := params["password"].(string)
				return map[string]any{
					"name":     name,
					"type":     "ss",
					"server":   "127.0.0.1",
					"port":     forwardPort,
					"cipher":   method,
					"password": pw,
				}
			},
		},
	}
}

// TestMihomoE2E_RealInternet is the end-to-end system test requested in
// stage 11+. For each MVP protocol:
//
//	curl → mihomo-client:SOCKS5 → server forward port → v2raymg-managed
//	       mihomo listener → DIRECT → <defaultE2ETarget>
//
// The server half uses the exact production wiring: MihomoContainer +
// FastAddInbound + UserManager.AddUser (the same calls any HTTP handler
// would make). The client half runs the SAME mihomo binary as a separate
// process with a hand-written yaml pointed at the forward port. Both are
// torn down on test exit.
//
// The test also validates three disruption scenarios that prove the chain
// really runs through the server (not some false-positive direct path):
//
//  1. Stop server → request fails → restart server → request succeeds
//  2. Remove the mihomo user → forward rule drops → request fails
//  3. Remove the inbound and re-add it with fresh credentials on the same
//     tag+port → client still uses old credentials → handshake fails
//
// Preconditions (any missing → test fails with a clear message, not a skip):
//   - XRAY_BIN: not required here; we don't use xray at all
//   - MIHOMO_BIN: optional (Updater downloads Alpha into t.TempDir if unset)
//   - Network egress to the target URL
func TestMihomoE2E_RealInternet(t *testing.T) {
	target := os.Getenv("MIHOMO_E2E_TARGET")
	if target == "" {
		target = defaultE2ETarget
	}

	// Precondition: target is directly reachable without any proxy. If
	// this fails the rest of the test is meaningless — every subtest
	// would fail regardless of mihomo state.
	t.Logf("network precheck: GET %s (direct, no proxy)", target)
	if err := assertDirectReachable(target); err != nil {
		t.Fatalf("network precondition failed: %v\n"+
			"Set MIHOMO_E2E_TARGET=<reachable-url> to override, "+
			"e.g. http://example.com when google.com is blocked.", err)
	}
	t.Logf("network precheck passed")

	tmpDir := t.TempDir()
	mihomoBin := ensureMihomoBin(t, tmpDir)
	rig := startMihomoContainer(t, tmpDir, mihomoBin)
	t.Logf("mihomo server ready (api=%s, binary=%s)", rig.apiAddr, mihomoBin)

	for _, tc := range e2eCases() {
		t.Run(tc.Name, func(t *testing.T) {
			runE2EProtocolCase(t, tc, rig, mihomoBin, tmpDir, target)
		})
	}
}

// runE2EProtocolCase runs the full state-transition sequence for one
// protocol. Structure is a sequence of steps with inline assertions rather
// than sub-subtests because:
//
//   - Each step depends on the prior state (stop+start requires a running
//     client already pointed at a known forward port)
//   - Failure in a late step is more informative when the earlier steps
//     are in the same testing.T frame (log contains the full narrative)
//
// Step labels are logged at INFO so a failing run reads like a transcript.
func runE2EProtocolCase(
	t *testing.T,
	tc e2eCase,
	rig *mihomoTestRig,
	mihomoBin, tmpDir, target string,
) {
	t.Helper()

	// ---- SETUP: server inbound + user, client process ----
	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc inbound port: %v", err)
	}
	tag := fmt.Sprintf("e2e-%s-%d", tc.Name, inboundPort)

	serverParams := tc.serverParams(inboundPort)
	serverParams["protocol"] = tc.Protocol

	// Trojan needs TLS certs at the listener — otherwise mihomo fails at
	// boot with "disallow using Trojan without both certificates/reality/
	// ss config". Generate a self-signed cert+key pair and wire the
	// paths into the adapter's cert_file/key_file optional params. Same
	// cert pair is reused for the credential-swap step below (only the
	// password changes there; cert continuity keeps the failure
	// unambiguously attributable to password mismatch).
	//
	// Critical: the cert MUST live under mihomo's DataDir (its "home
	// directory"). mihomo Alpha enforces a SAFE_PATHS rule and rejects
	// config-referenced paths outside that directory with "path is not
	// subpath of home directory". rig.dataDir is that directory.
	var trojanCertFile, trojanKeyFile string
	if tc.Protocol == "trojan" {
		cp, kp, _, _, err := writeTempCert(rig.dataDir)
		if err != nil {
			t.Fatalf("writeTempCert: %v", err)
		}
		trojanCertFile, trojanKeyFile = cp, kp
		serverParams["cert_file"] = cp
		serverParams["key_file"] = kp
	}

	t.Logf("STEP setup: FastAddInbound tag=%s port=%d", tag, inboundPort)
	if err := rig.container.FastAddInbound(tag, serverParams); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 10*time.Second); err != nil {
		t.Fatalf("mihomo listener not ready: %v", err)
	}

	username := fmt.Sprintf("e2e-%s-alice-%d", tc.Name, inboundPort)
	forwardPort := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))
	t.Logf("STEP setup: user %s → forward port %d → 127.0.0.1:%d", username, forwardPort, inboundPort)

	clientSocksPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc client socks port: %v", err)
	}
	clientAPIPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc client API port: %v", err)
	}
	clientDataDir := filepath.Join(tmpDir, fmt.Sprintf("client-%s-data", tc.Name))
	if err := os.MkdirAll(clientDataDir, 0755); err != nil {
		t.Fatalf("mkdir client data dir: %v", err)
	}

	clientCfgYAML := buildMihomoClientConfig(
		tc, serverParams, int(forwardPort), clientSocksPort, clientAPIPort,
	)
	clientCfgPath := filepath.Join(tmpDir, fmt.Sprintf("client-%s.yaml", tc.Name))
	if err := os.WriteFile(clientCfgPath, []byte(clientCfgYAML), 0600); err != nil {
		t.Fatalf("write client yaml: %v", err)
	}

	t.Logf("STEP setup: starting mihomo client (socks=%d api=%d)", clientSocksPort, clientAPIPort)
	clientCancel := startMihomoClient(t, mihomoBin, clientDataDir, clientCfgPath, clientAPIPort, clientSocksPort)
	defer clientCancel()

	// Single HTTP client reused across all steps. Short timeout — on
	// negative steps we want quick failure, not a minute of hang.
	//
	// Keep-alive is disabled so each step opens a fresh TCP socket. A
	// pooled keep-alive connection established during the positive step
	// and carried forward into a negative step could succeed even after
	// the underlying chain is broken (the connection is already plumbed
	// through the SOCKS5 dialer; breakage shows up only at the next
	// fresh dial). Disabling keep-alive makes every request cover the
	// full SOCKS5→proxy→server path, which is exactly what we want to
	// assert on.
	httpClient, err := httpClientViaSocks5(fmt.Sprintf("127.0.0.1:%d", clientSocksPort))
	if err != nil {
		t.Fatalf("httpClientViaSocks5: %v", err)
	}
	httpClient.Timeout = 10 * time.Second
	if tr, ok := httpClient.Transport.(*http.Transport); ok {
		tr.DisableKeepAlives = true
	}

	// ---- STEP 1: positive baseline ----
	t.Logf("STEP positive_baseline: GET %s via chain", target)
	expectProxiedOK(t, httpClient, target, "positive_baseline")
	t.Logf("PASS positive_baseline")

	// ---- STEP 2: negative — stop server ----
	t.Logf("STEP negative_stop_server: stopping mihomo server container")
	if err := rig.container.Stop(); err != nil {
		// Stop may error if the prior process already exited; benign.
		t.Logf("container.Stop returned (non-fatal): %v", err)
	}
	expectProxyFail(t, httpClient, target, "negative_stop_server")
	t.Logf("PASS negative_stop_server")

	// ---- STEP 3: recover — restart server, verify chain heals ----
	t.Logf("STEP recover_restart_server: starting mihomo server container")
	if err := rig.container.Start(); err != nil {
		t.Fatalf("container.Start after Stop: %v", err)
	}
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 10*time.Second); err != nil {
		t.Fatalf("listener not ready after restart: %v", err)
	}
	// Sanity: PortMappings must keep the forward port stable across restart.
	forwardPortAfterRestart, ok := rig.userMgr.GetUserPortByDst(username, uint32(inboundPort))
	if !ok {
		t.Fatalf("user %s lost forward port after restart", username)
	}
	if forwardPortAfterRestart != forwardPort {
		t.Fatalf("forward port drifted across restart: %d → %d (PortMappings regression)",
			forwardPort, forwardPortAfterRestart)
	}
	expectProxiedOK(t, httpClient, target, "recover_restart_server")
	t.Logf("PASS recover_restart_server (port stable at %d)", forwardPort)

	// ---- STEP 4: negative — change server credentials ----
	// Order matters: we do this BEFORE remove_user so the user (and its
	// PortMappings[inboundPort] → forwardPort entry) is still live. The
	// point of this step is to prove "wrong credentials break the chain"
	// — we need the forward rule and listener to both be UP so the
	// failure isolates to the handshake layer. Doing remove_user first
	// and then changing credentials would conflate "no forward rule"
	// with "wrong credentials", weakening the evidence.
	//
	// Mechanism: RemoveInboundConfig releases alice's forward rule but
	// LEAVES PortMappings intact (stage 5 decision, see usermanager.go
	// cleanupUserPorts comment). The immediately-following FastAddInbound
	// on the SAME tag + SAME inbound port invokes reconcileUsersForInbound,
	// which for alice hits PortMappings[inboundPort] in GetBindPort and
	// re-grants the SAME forward port. Net result: forward port stable,
	// listener live with NEW credentials, alice's client still armed with
	// OLD credentials → handshake fails at the protocol layer.
	t.Logf("STEP negative_change_credential: swap inbound credentials on same tag/port")
	if err := rig.container.RemoveInboundConfig(tag); err != nil {
		t.Fatalf("RemoveInboundConfig: %v", err)
	}
	newServerParams := tc.serverParams(inboundPort)
	newServerParams["protocol"] = tc.Protocol
	// Reuse the same cert pair for trojan; only the password changes.
	// Keeping the cert stable isolates the assertion: failure is due to
	// the password mismatch, not a cert/SNI mismatch.
	if tc.Protocol == "trojan" {
		newServerParams["cert_file"] = trojanCertFile
		newServerParams["key_file"] = trojanKeyFile
	}
	if err := rig.container.FastAddInbound(tag, newServerParams); err != nil {
		t.Fatalf("FastAddInbound with new credentials: %v", err)
	}
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 10*time.Second); err != nil {
		t.Fatalf("listener not ready after credential swap: %v", err)
	}
	// Give reconcileUsersForInbound a beat to reinstate alice's forward
	// rule via PortMappings — GetBindPort is synchronous inside the call
	// but the internal bookkeeping settles asynchronously.
	forwardPortAfterSwap, ok := rig.userMgr.GetUserPortByDst(username, uint32(inboundPort))
	if !ok {
		t.Fatalf("user %s lost forward port after inbound swap (PortMappings should have made it stable)", username)
	}
	if forwardPortAfterSwap != forwardPort {
		// Not strictly a failure — PortMappings is a best-effort promise,
		// not a hard invariant, if the allocator's pool has recycled the
		// port. But for a loopback-only test with a wide pool this should
		// be stable; surface a warning so reviewers know if it drifts.
		t.Logf("NOTE: forward port drifted %d → %d after credential swap (PortMappings miss)",
			forwardPort, forwardPortAfterSwap)
	}
	// The client's upstream config is now credential-mismatched with the
	// new listener. Handshake surfaces as a read error/EOF/timeout at the
	// vmess AEAD or trojan TLS or ss AEAD layer; HTTP GET bubbles this up
	// as a net.OpError.
	expectProxyFail(t, httpClient, target, "negative_change_credential")
	t.Logf("PASS negative_change_credential")

	// ---- STEP 5: negative — remove user ----
	// After the credential swap alice still holds a forward rule (same
	// port, new listener credentials). RemoveUser takes that rule down
	// via the UserEventRemove pipeline. Client's SOCKS5 requests now
	// can't even reach the forward port — connect fails before any
	// handshake.
	t.Logf("STEP negative_remove_user: RemoveUser after credential swap")
	removeMihomoUserAndWaitForPortRelease(t, rig, username, tag)
	expectProxyFail(t, httpClient, target, "negative_remove_user")
	t.Logf("PASS negative_remove_user")
}

// TestMihomoE2E_FillDefaults_TrojanSelfSigned locks the new stage 12 cert
// source wiring end-to-end. Stage 11+ only validated the caller-supplied
// cert_file/key_file path; this test drives trojan through the
// FillDefaults self_signed path:
//
//  1. params.FillDefaults generates a fresh self-signed cert under the
//     mihomo container's DataDir (SAFE_PATHS-compliant), materialises
//     cert_file + key_file paths, and sets cert_source="self_signed"
//  2. container.FastAddInbound accepts the resulting params, mihomo
//     boots the trojan listener
//  3. A mihomo client speaking trojan reaches google.com through the
//     chain — proves the generated cert is usable end-to-end
//  4. container.RemoveInboundConfig cleans up the on-disk cert files
//     because cert_source is a v2raymg-managed source
//
// Shares the container rig (process, user manager, forward manager) with
// the other E2E subtests — this single test only adds one inbound with
// one user, so runtime cost is negligible.
func TestMihomoE2E_FillDefaults_TrojanSelfSigned(t *testing.T) {
	// Unlike TestMihomoE2E_RealInternet this test uses mihomo as the
	// protocol client (same binary as the server). No XRAY_BIN needed.

	target := os.Getenv("MIHOMO_E2E_TARGET")
	if target == "" {
		target = defaultE2ETarget
	}
	if err := assertDirectReachable(target); err != nil {
		t.Fatalf("network precondition failed: %v", err)
	}

	tmpDir := t.TempDir()
	mihomoBin := ensureMihomoBin(t, tmpDir)
	rig := startMihomoContainer(t, tmpDir, mihomoBin)
	t.Logf("mihomo server ready (api=%s)", rig.apiAddr)

	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc inbound port: %v", err)
	}
	tag := fmt.Sprintf("e2e-fd-ss-trojan-%d", inboundPort)

	// Build minimal trojan params: no password (FillDefaults generates),
	// no cert files (FillDefaults self_signed generates). server_name
	// drives the cert CN so SNI matches.
	serverParams := map[string]any{
		"protocol":    "trojan",
		"port":        inboundPort,
		"self_signed": true,
		"server_name": "localhost",
	}
	t.Logf("FillDefaults: trojan self_signed → scratchDir=%s", rig.dataDir)
	if err := fillparams.FillDefaults(serverParams, "trojan", nil, rig.dataDir); err != nil {
		t.Fatalf("FillDefaults: %v", err)
	}

	// Post-fill invariants: password generated, cert material landed under
	// DataDir (not in some temp dir), source marked so cleanup runs on
	// Remove. Asserting here, not in the params unit test, because
	// "mihomo's DataDir is SAFE_PATHS-compliant" is a system-level
	// invariant — the unit test doesn't know about mihomo.
	if pw, _ := serverParams["password"].(string); pw == "" {
		t.Fatalf("FillDefaults did not auto-generate trojan password")
	}
	cf, _ := serverParams["cert_file"].(string)
	kf, _ := serverParams["key_file"].(string)
	if cf == "" || kf == "" {
		t.Fatalf("FillDefaults did not populate cert_file/key_file: %+v", serverParams)
	}
	if !strings.HasPrefix(cf, rig.dataDir) {
		t.Fatalf("cert_file not under DataDir; SAFE_PATHS will reject: %s", cf)
	}
	if src, _ := serverParams["cert_source"].(string); src != "self_signed" {
		t.Fatalf("cert_source mismatch: got %q want self_signed", src)
	}
	if _, err := os.Stat(cf); err != nil {
		t.Fatalf("cert file not materialised on disk: %v", err)
	}

	// Now register the inbound; mihomo will accept the generated cert.
	if err := rig.container.FastAddInbound(tag, serverParams); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 10*time.Second); err != nil {
		t.Fatalf("mihomo trojan listener not ready: %v", err)
	}

	// Bring up a user and a matching client; verify the chain reaches
	// the internet. Reuses the same mihomoProtocolCase.clientProxy
	// builder for trojan so the client-side wire format stays consistent
	// with the other E2E tests.
	username := fmt.Sprintf("fd-ss-alice-%d", inboundPort)
	forwardPort := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))

	clientSocksPort, _ := freeTCPPort()
	clientAPIPort, _ := freeTCPPort()
	clientDataDir := filepath.Join(tmpDir, fmt.Sprintf("client-fd-ss-data-%d", inboundPort))
	if err := os.MkdirAll(clientDataDir, 0755); err != nil {
		t.Fatalf("mkdir client data dir: %v", err)
	}

	trojanCase := e2eCase{}
	for _, tc := range e2eCases() {
		if tc.Name == "trojan" {
			trojanCase = tc
			break
		}
	}
	clientCfg := buildMihomoClientConfig(trojanCase, serverParams, int(forwardPort), clientSocksPort, clientAPIPort)
	clientCfgPath := filepath.Join(tmpDir, fmt.Sprintf("client-fd-ss-%d.yaml", inboundPort))
	if err := os.WriteFile(clientCfgPath, []byte(clientCfg), 0600); err != nil {
		t.Fatalf("write client yaml: %v", err)
	}
	cancelClient := startMihomoClient(t, mihomoBin, clientDataDir, clientCfgPath, clientAPIPort, clientSocksPort)
	defer cancelClient()

	httpClient, err := httpClientViaSocks5(fmt.Sprintf("127.0.0.1:%d", clientSocksPort))
	if err != nil {
		t.Fatalf("httpClientViaSocks5: %v", err)
	}
	httpClient.Timeout = 10 * time.Second
	if tr, ok := httpClient.Transport.(*http.Transport); ok {
		tr.DisableKeepAlives = true
	}

	t.Logf("GET %s via FillDefaults-generated trojan inbound", target)
	expectProxiedOK(t, httpClient, target, "fill_defaults_self_signed_baseline")
	t.Logf("PASS baseline via self-signed trojan inbound")

	// Cleanup invariant: RemoveInboundConfig must delete the cert files
	// because cert_source == "self_signed". This closes the loop on the
	// stage-12 lifecycle contract (v2raymg cleans up what v2raymg wrote).
	if err := rig.container.RemoveInboundConfig(tag); err != nil {
		t.Fatalf("RemoveInboundConfig: %v", err)
	}
	if _, err := os.Stat(cf); !os.IsNotExist(err) {
		t.Fatalf("cert file still present after RemoveInboundConfig: %v", err)
	}
	if _, err := os.Stat(kf); !os.IsNotExist(err) {
		t.Fatalf("key file still present after RemoveInboundConfig: %v", err)
	}
	t.Logf("PASS cleanup: cert+key removed after RemoveInboundConfig")
}

// buildMihomoClientConfig returns a mihomo yaml config string for the
// client side. Uses the top-level `mixed-port` (SOCKS5 + HTTP combined
// inbound) because (a) it doesn't need a listener credential — any local
// SOCKS5 client can hit it — and (b) production mihomo client deployments
// typically use mixed-port too, so the config shape matches real-world.
//
// Returns a yaml string rather than a map[string]any→marshal roundtrip
// because the handful of fields we set has no dynamic schema and the
// hand-written form is faster to audit line-by-line.
func buildMihomoClientConfig(
	tc e2eCase,
	serverParams map[string]any,
	forwardPort, socksPort, apiPort int,
) string {
	proxy := tc.clientProxy("upstream", serverParams, forwardPort)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("mixed-port: %d\n", socksPort))
	sb.WriteString("allow-lan: false\n")
	sb.WriteString(fmt.Sprintf("external-controller: 127.0.0.1:%d\n", apiPort))
	sb.WriteString("log-level: warning\n")
	sb.WriteString("mode: rule\n")
	sb.WriteString("\n")

	sb.WriteString("proxies:\n")
	sb.WriteString("  - ")
	sb.WriteString(marshalProxyInline(proxy))
	sb.WriteString("\n\n")

	// proxy-groups is required for rules to reference by name; a
	// single-proxy `select` group is the simplest shape.
	sb.WriteString("proxy-groups:\n")
	sb.WriteString("  - name: global\n")
	sb.WriteString("    type: select\n")
	sb.WriteString("    proxies:\n")
	sb.WriteString("      - upstream\n\n")

	// rules send everything through the group — no DIRECT fallback; if
	// the tunnel breaks, client requests fail rather than silently bypass.
	sb.WriteString("rules:\n")
	sb.WriteString("  - MATCH,global\n")

	return sb.String()
}

// marshalProxyInline emits a single proxy entry as an inline yaml flow map
// (mihomo accepts these, and they dodge the 6-deep indentation tangle a
// block-style map would produce under yaml's list-under-map conventions).
// Input is restricted to strings, ints, and bools — no nested maps/lists.
func marshalProxyInline(p map[string]any) string {
	// Stable key order so test output is diffable across runs. Start with
	// the canonical mihomo proxy fields then tack on the rest alphabetically.
	canonical := []string{"name", "type", "server", "port"}
	seen := map[string]bool{}
	parts := make([]string, 0, len(p))
	for _, k := range canonical {
		if v, ok := p[k]; ok {
			parts = append(parts, fmt.Sprintf("%s: %s", k, yamlScalar(v)))
			seen[k] = true
		}
	}
	// Remaining keys in lexical order for stability.
	var extra []string
	for k := range p {
		if !seen[k] {
			extra = append(extra, k)
		}
	}
	// Simple ascending insertion sort — len is tiny (<10).
	for i := 1; i < len(extra); i++ {
		for j := i; j > 0 && extra[j] < extra[j-1]; j-- {
			extra[j], extra[j-1] = extra[j-1], extra[j]
		}
	}
	for _, k := range extra {
		parts = append(parts, fmt.Sprintf("%s: %s", k, yamlScalar(p[k])))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// yamlScalar renders one of the restricted scalar types we emit. The
// single-quoted form protects values that contain yaml-reserved chars
// (e.g. a password with "#" or ":"); numeric/bool fall through unquoted so
// mihomo's yaml decoder types them correctly.
func yamlScalar(v any) string {
	switch vv := v.(type) {
	case string:
		// Single-quote and escape embedded quotes by doubling them.
		return "'" + strings.ReplaceAll(vv, "'", "''") + "'"
	case int:
		return fmt.Sprintf("%d", vv)
	case int32:
		return fmt.Sprintf("%d", vv)
	case int64:
		return fmt.Sprintf("%d", vv)
	case uint32:
		return fmt.Sprintf("%d", vv)
	case bool:
		if vv {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("'%v'", vv)
	}
}

// startMihomoClient launches a second mihomo process to act as the SOCKS5
// client. Returns a cancellation func that kills the process; caller
// registers it via defer. The function blocks until the external-controller
// API responds to GET /version, mirroring the server-side readiness gate.
func startMihomoClient(
	t *testing.T,
	mihomoBin, dataDir, configPath string,
	apiPort, socksPort int,
) (cancel func()) {
	t.Helper()

	ctx, cancelCtx := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, mihomoBin, "-d", dataDir, "-f", configPath)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cancelCtx()
		t.Fatalf("start mihomo client: %v", err)
	}

	// Wait for the external-controller port AND the mixed-port (SOCKS5
	// entry). Both must be live before tests can issue GETs.
	apiAddr := fmt.Sprintf("127.0.0.1:%d", apiPort)
	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitPort(apiAddr, 10*time.Second); err != nil {
		cancelCtx()
		_ = cmd.Wait()
		t.Fatalf("client external-controller %s not ready: %v", apiAddr, err)
	}
	if err := waitPort(socksAddr, 10*time.Second); err != nil {
		cancelCtx()
		_ = cmd.Wait()
		t.Fatalf("client mixed-port %s not ready: %v", socksAddr, err)
	}

	return func() {
		cancelCtx()
		_ = cmd.Wait()
	}
}

// expectProxiedOK asserts that an HTTP GET through the SOCKS5 client
// reaches the target and returns a "successful" status. "Successful" is
// interpreted loosely: status < 500. The target (e.g. google.com) may
// return 301/302/403 depending on geography; none of those mean the proxy
// chain is broken. Only network errors and 5xx count as "chain down".
func expectProxiedOK(t *testing.T, client *http.Client, target, step string) {
	t.Helper()
	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("[%s] GET via proxy failed: %v (chain broken?)", step, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 500 {
		t.Fatalf("[%s] GET via proxy returned %d (chain broken or upstream down)", step, resp.StatusCode)
	}
}

// expectProxyFail asserts that an HTTP GET through the SOCKS5 client
// FAILS — proves the positive-path success wasn't via some alternate
// direct route. Failure can surface in several ways (TCP reset on the
// SOCKS5 socket, SOCKS5 reply indicating unreachable, TLS handshake
// abort, outright timeout); any of them are acceptable. What is NOT
// acceptable: a 200 OK. That would mean the "chain" is really not going
// through mihomo at all and the positive test is a false positive.
func expectProxyFail(t *testing.T, client *http.Client, target, step string) {
	t.Helper()
	resp, err := client.Get(target)
	if err == nil {
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode < 500 {
			t.Fatalf("[%s] GET via proxy unexpectedly succeeded (status %d) — chain may be bypassing the mihomo server",
				step, resp.StatusCode)
		}
		// 5xx here is a fail — that's "server-side exploded", which for
		// the sake of this test is indistinguishable from "chain broke".
		return
	}
	// err is non-nil — expected for a broken chain.
}

// assertDirectReachable is the network-precondition check. A 10-second
// timeout is enough to distinguish "no route" from "slow link" without
// making the CI hang on a blocked network. Uses a fresh http.Client so we
// don't inherit any SOCKS5 dialer.
func assertDirectReachable(target string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		return fmt.Errorf("direct GET %s: %w", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("direct GET %s: status %d", target, resp.StatusCode)
	}
	return nil
}
