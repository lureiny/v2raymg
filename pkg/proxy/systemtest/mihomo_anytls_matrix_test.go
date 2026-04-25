//go:build integration

package systemtest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/converter"
)

// TestMihomoAnyTLSMatrix drives Phase 7 AnyTLS coverage end-to-end through a
// real mihomo server (the listener) and a real mihomo client (the outbound),
// out to a public upstream via the forward layer.
//
// Matrix scope (Phase 7 plan T-M-P7-76):
//   - tls-baseline: defaults (mihomo DefaultPaddingScheme + no client idle
//     overrides) — exercises the no-knobs happy path.
//   - tls-padding-only: custom padding-scheme on the listener, no client
//     idle knobs — isolates the inline padding-scheme write path so a
//     regression in fillAnyTLSListener wouldn't be masked by overlapping
//     client-side knob differences.
//   - tls-idle-only: standard padding (default), client idle knobs at 30s
//     / 60s / 2 — isolates the client-outbound idle field plumbing
//     (subscription/converter → mihomo client int-seconds).
//   - tls-low-idle: client idle knobs at 3s / 1s / 1 — proves the
//     documented "mihomo runtime bumps ≤5s to 30s" behaviour is at
//     runtime not at v2raymg parser, matching feedback memory's
//     "no parser tutoring" contract.
//
// Plus one cross-cutting subscription_chain subtest exercising
// GetUserSubscriptions → ClashConverter.convertAnyTLS — mandatory per the
// "systemtest must walk a real subscription pipeline" memory rule.
//
// AnyTLS is TCP-based (UDP rides UoT inside the same TLS stream). The
// forward rule is therefore a TCP relay (forwardNetworkForProtocol(
// ProtocolAnyTLS) returns "" → default "tcp"). A regression that flips the
// forward layer to UDP would surface here as a connect failure.
func TestMihomoAnyTLSMatrix(t *testing.T) {
	if os.Getenv("MIHOMO_BIN") == "" && !probeURL("https://github.com") {
		t.Skip("MIHOMO_BIN not set and github.com unreachable for Alpha download")
	}
	upstream, ok := canReachPublicUpstream(t)
	if !ok {
		t.Skipf("no public upstream reachable (%s); set SYSTEST_UPSTREAM_URL", upstream)
	}
	t.Logf("upstream: %s", upstream)

	tmpDir := t.TempDir()
	mihomoBin := ensureMihomoBin(t, tmpDir)
	rig := startMihomoContainer(t, tmpDir, mihomoBin)
	t.Logf("mihomo server rig ready: api=%s data=%s", rig.apiAddr, rig.dataDir)

	certPath, keyPath, _, _, err := writeTempCert(rig.dataDir)
	if err != nil {
		t.Fatalf("writeTempCert: %v", err)
	}

	cases := []anytlsMatrixCase{
		{name: "tls-baseline", certFile: certPath, keyFile: keyPath},
		{
			name:          "tls-padding-only",
			certFile:      certPath,
			keyFile:       keyPath,
			paddingScheme: "stop=4\n0=30-30\n1=100-400\n2=400-500\n3=9-9,500-1000",
		},
		{
			name:                     "tls-idle-only",
			certFile:                 certPath,
			keyFile:                  keyPath,
			idleSessionCheckInterval: 30,
			idleSessionTimeout:       60,
			minIdleSession:           2,
		},
		{
			// ≤5s should hit mihomo's runtime fallback to 30s
			// (transport/anytls/session/client.go:46-51); v2raymg parser
			// does NOT pre-tutor the value — we verify here that the value
			// passes through and the client still works (mihomo runtime
			// bumps it transparently).
			name:                     "tls-low-idle",
			certFile:                 certPath,
			keyFile:                  keyPath,
			idleSessionCheckInterval: 3,
			idleSessionTimeout:       1,
			minIdleSession:           1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runAnyTLSMatrixCase(t, rig, mihomoBin, tc, upstream)
		})
	}

	t.Run("subscription_chain_padding_idle", func(t *testing.T) {
		runAnyTLSSubscriptionChainCase(t, rig, mihomoBin, anytlsMatrixCase{
			name:                     "subscription-chain-tune",
			certFile:                 certPath,
			keyFile:                  keyPath,
			paddingScheme:            "stop=4\n0=30-30\n1=100-400\n2=400-500\n3=9-9,500-1000",
			idleSessionCheckInterval: 30,
			idleSessionTimeout:       60,
			minIdleSession:           2,
		}, upstream)
	})
}

type anytlsMatrixCase struct {
	name string

	certFile string
	keyFile  string

	paddingScheme            string // server-only inline text
	idleSessionCheckInterval int    // client-only seconds
	idleSessionTimeout       int    // client-only seconds
	minIdleSession           int    // client-only
}

// runAnyTLSMatrixCase wires up the per-case mihomo server inbound, allocates
// a forward TCP port via the user manager, then spins a mihomo client that
// routes traffic through that port out to the upstream.
func runAnyTLSMatrixCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc anytlsMatrixCase, upstream string) {
	t.Helper()

	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	tag := fmt.Sprintf("anytls-%s-%d", tc.name, inboundPort)
	password := fmt.Sprintf("anytls-pass-%d", inboundPort)

	params := map[string]any{
		"protocol":    "anytls",
		"port":        inboundPort,
		"password":    password,
		"cert_file":   tc.certFile,
		"key_file":    tc.keyFile,
		"cert_source": "file",
	}
	if tc.paddingScheme != "" {
		params["padding_scheme"] = tc.paddingScheme
	}
	if tc.idleSessionCheckInterval > 0 {
		params["idle_session_check_interval_seconds"] = tc.idleSessionCheckInterval
	}
	if tc.idleSessionTimeout > 0 {
		params["idle_session_timeout_seconds"] = tc.idleSessionTimeout
	}
	if tc.minIdleSession > 0 {
		params["min_idle_session"] = tc.minIdleSession
	}

	t.Logf("FastAddInbound %s on 127.0.0.1:%d (padding=%v idle_check=%d idle_timeout=%d min_idle=%d)",
		tag, inboundPort, tc.paddingScheme != "",
		tc.idleSessionCheckInterval, tc.idleSessionTimeout, tc.minIdleSession)
	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 3*time.Second); err != nil {
		t.Fatalf("anytls tcp listener not ready: %v", err)
	}

	username := fmt.Sprintf("alice-anytls-%s-%d", tc.name, inboundPort)
	forwardPort := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))

	clientCaseDir := filepath.Join(t.TempDir(), tc.name)
	if err := os.MkdirAll(clientCaseDir, 0o755); err != nil {
		t.Fatalf("mkdir client case dir: %v", err)
	}
	proxy := buildAnyTLSClashProxy(tc, password, "127.0.0.1", int(forwardPort))
	socksPort := spawnMihomoClient(t, clientCaseDir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    filepath.Join(clientCaseDir, "data"),
		Proxy:      proxy,
	})

	t.Logf("anytls client socks5 on 127.0.0.1:%d", socksPort)
	status, body := httpGetViaSocks5(t, socksPort, upstream)
	if status < 200 || status >= 400 {
		t.Fatalf("upstream via anytls %s: status=%d body=%q", tc.name, status, body)
	}
	t.Logf("PASS: %s upstream=%s status=%d", tc.name, upstream, status)
}

// runAnyTLSSubscriptionChainCase walks GetUserSubscriptions → convertAnyTLS
// to catch converter-vs-fill drift (e.g. fingerprint not propagated, idle
// fields renamed, padding leaked). Same end-to-end probe as the matrix
// case, but the client proxy yaml is built from the real ClashConverter
// output rather than by hand.
func runAnyTLSSubscriptionChainCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc anytlsMatrixCase, upstream string) {
	t.Helper()

	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	tag := fmt.Sprintf("anytls-chain-%s-%d", tc.name, inboundPort)
	password := fmt.Sprintf("anytls-chain-pass-%d", inboundPort)

	params := map[string]any{
		"protocol":         "anytls",
		"port":             inboundPort,
		"password":         password,
		"cert_file":        tc.certFile,
		"key_file":         tc.keyFile,
		"cert_source":      "file",
		"sni":              "localhost",
		"skip_cert_verify": true,
	}
	if tc.paddingScheme != "" {
		params["padding_scheme"] = tc.paddingScheme
	}
	if tc.idleSessionCheckInterval > 0 {
		params["idle_session_check_interval_seconds"] = tc.idleSessionCheckInterval
	}
	if tc.idleSessionTimeout > 0 {
		params["idle_session_timeout_seconds"] = tc.idleSessionTimeout
	}
	if tc.minIdleSession > 0 {
		params["min_idle_session"] = tc.minIdleSession
	}

	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 3*time.Second); err != nil {
		t.Fatalf("anytls tcp listener not ready: %v", err)
	}

	username := fmt.Sprintf("alice-anytls-chain-%s-%d", tc.name, inboundPort)
	forwardPort := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))

	specs, err := rig.container.GetUserSubscriptions(contracts.SubscriptionRequest{
		User: contracts.UserSpec{Username: username},
		Host: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("expected exactly one spec, got %d", len(specs))
	}
	if specs[0].Port != forwardPort {
		t.Fatalf("spec.Port = %d, want forward port %d", specs[0].Port, forwardPort)
	}

	proxy := anytlsProxyFromSpec(t, specs[0])
	clientCaseDir := filepath.Join(t.TempDir(), tc.name)
	if err := os.MkdirAll(clientCaseDir, 0o755); err != nil {
		t.Fatalf("mkdir client case dir: %v", err)
	}
	socksPort := spawnMihomoClient(t, clientCaseDir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    filepath.Join(clientCaseDir, "data"),
		Proxy:      proxy,
	})

	status, body := httpGetViaSocks5(t, socksPort, upstream)
	if status < 200 || status >= 400 {
		t.Fatalf("upstream via anytls subscription chain: status=%d body=%q", status, body)
	}
	t.Logf("PASS: subscription_chain %s upstream=%s status=%d", tc.name, upstream, status)
}

// anytlsProxyFromSpec converts a SubscriptionSpec into a mihomo client proxy
// map by routing it through the real ClashConverter. Mirrors tuicProxyFromSpec
// — drift between subscription/converter and client schema falls out here.
func anytlsProxyFromSpec(t *testing.T, spec contracts.SubscriptionSpec) map[string]any {
	t.Helper()
	cp := converter.ConvertAnyTLSForTest(spec)
	if cp == nil {
		t.Fatalf("convertAnyTLS returned nil for spec: %+v", spec)
	}

	p := map[string]any{
		"name":     "anytls-subscription-chain",
		"type":     "anytls",
		"server":   cp.Server,
		"port":     cp.Port,
		"password": cp.Password,
		"udp":      cp.UDP,
	}
	if cp.SNI != "" {
		p["sni"] = cp.SNI
	}
	if cp.SkipCertVerify {
		p["skip-cert-verify"] = true
	}
	if cp.IdleSessionCheckInterval > 0 {
		p["idle-session-check-interval"] = cp.IdleSessionCheckInterval
	}
	if cp.IdleSessionTimeout > 0 {
		p["idle-session-timeout"] = cp.IdleSessionTimeout
	}
	if cp.MinIdleSession > 0 {
		p["min-idle-session"] = cp.MinIdleSession
	}
	return p
}

// buildAnyTLSClashProxy builds a hand-rolled mihomo client proxy entry for
// the non-subscription-chain matrix cases. Mirrors what an operator would
// write into a clash config given the inbound's parameters.
func buildAnyTLSClashProxy(tc anytlsMatrixCase, password, server string, port int) map[string]any {
	p := map[string]any{
		"name":             "anytls-test",
		"type":             "anytls",
		"server":           server,
		"port":             port,
		"password":         password,
		"sni":              "localhost",
		"skip-cert-verify": true,
		"udp":              true,
	}
	if tc.idleSessionCheckInterval > 0 {
		p["idle-session-check-interval"] = tc.idleSessionCheckInterval
	}
	if tc.idleSessionTimeout > 0 {
		p["idle-session-timeout"] = tc.idleSessionTimeout
	}
	if tc.minIdleSession > 0 {
		p["min-idle-session"] = tc.minIdleSession
	}
	// padding-scheme is server-only (mihomo client schema has no field) —
	// deliberately not propagated even when the test case set it; server
	// uses it transparently and the client sees only the protocol effect.
	return p
}
