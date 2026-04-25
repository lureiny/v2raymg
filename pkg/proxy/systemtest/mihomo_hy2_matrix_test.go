//go:build integration

package systemtest

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/converter"
)

// waitUDPListener polls the given UDP port until a UDP "connect" succeeds,
// or the deadline fires. UDP has no handshake, so a successful Dial only
// proves the port can be addressed (kernel-level), not that the QUIC
// listener actually answers. Combined with FastAddInbound's REST-PATCH
// having already returned, this is a good-enough readiness gate that
// scales with machine speed instead of using a fixed 500ms sleep.
func waitUDPListener(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("udp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("udp port %s not ready after %s", addr, timeout)
}

// TestMihomoHy2Matrix drives Phase 5 Hysteria2 coverage through a real
// mihomo server (the listener side) and a real mihomo client (the outbound
// side), end-to-end through the forward layer to a public upstream.
//
// Matrix scope (Phase 5 plan T-M-P5-57):
//   - tls baseline (no obfs / no rate-limit)
//   - tls + salamander obfs (obfs + obfs_password emitted)
//   - tls + up/down (server advertises bandwidth caps)
//
// Plus one cross-cutting subscription_chain subtest exercising
// GetUserSubscriptions → ClashConverter.convertHysteria2; mandatory per the
// "systemtest must walk a real subscription pipeline" memory rule.
//
// Hysteria2 is QUIC-over-UDP at the wire level. The forward rule for hy2 is
// a UDPRelay (MihomoInbound.AddUser passes Network="udp" for ProtocolHysteria2);
// this test exercises that path, so a regression that returns to TCP would
// surface here as a connect failure.
func TestMihomoHy2Matrix(t *testing.T) {
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

	cases := []hy2MatrixCase{
		{name: "tls-baseline", certFile: certPath, keyFile: keyPath},
		{
			name:         "tls-salamander",
			certFile:     certPath,
			keyFile:      keyPath,
			obfs:         "salamander",
			obfsPassword: "obfs-shh-" + randHex(6),
		},
		{
			name:     "tls-bbr-bandwidth",
			certFile: certPath,
			keyFile:  keyPath,
			up:       "100 Mbps",
			down:     "100 Mbps",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runHy2MatrixCase(t, rig, mihomoBin, tc, upstream)
		})
	}

	t.Run("subscription_chain_tls_salamander", func(t *testing.T) {
		runHy2SubscriptionChainCase(t, rig, mihomoBin, hy2MatrixCase{
			name:         "subscription-chain-salamander",
			certFile:     certPath,
			keyFile:      keyPath,
			obfs:         "salamander",
			obfsPassword: "chain-obfs-" + randHex(6),
		}, upstream)
	})
}

type hy2MatrixCase struct {
	name string

	certFile string
	keyFile  string

	obfs         string
	obfsPassword string

	up   string
	down string
}

// runHy2MatrixCase wires up the per-case mihomo server inbound, allocates a
// forward UDP port via the user manager, then spins a mihomo client that
// routes traffic through that port out to the upstream.
func runHy2MatrixCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc hy2MatrixCase, upstream string) {
	t.Helper()

	inboundPort := freeUDPPort(t)
	tag := fmt.Sprintf("hy2-%s-%d", tc.name, inboundPort)
	password := fmt.Sprintf("hy2-pass-%d", inboundPort)

	params := map[string]any{
		"protocol":    "hysteria2",
		"port":        inboundPort,
		"password":    password,
		"cert_file":   tc.certFile,
		"key_file":    tc.keyFile,
		"cert_source": "file",
	}
	if tc.obfs != "" {
		params["obfs"] = tc.obfs
		params["obfs_password"] = tc.obfsPassword
	}
	if tc.up != "" {
		params["up"] = tc.up
	}
	if tc.down != "" {
		params["down"] = tc.down
	}

	t.Logf("FastAddInbound %s on 127.0.0.1:%d (obfs=%q up=%q down=%q)",
		tag, inboundPort, tc.obfs, tc.up, tc.down)
	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	// Hy2 is UDP/QUIC — TCP-style waitPort can't probe it. Use a
	// UDP-dial poll (machine-speed) instead of a fixed sleep; FastAddInbound
	// already waited for mihomo's REST PATCH, so the listener should be
	// up within a few iterations.
	if err := waitUDPListener(fmt.Sprintf("127.0.0.1:%d", inboundPort), 3*time.Second); err != nil {
		t.Fatalf("hy2 udp listener not ready: %v", err)
	}

	username := fmt.Sprintf("alice-hy2-%s-%d", tc.name, inboundPort)
	forwardPort := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))

	clientCaseDir := filepath.Join(t.TempDir(), tc.name)
	if err := os.MkdirAll(clientCaseDir, 0o755); err != nil {
		t.Fatalf("mkdir client case dir: %v", err)
	}
	proxy := buildHy2ClashProxy(tc, password, "127.0.0.1", int(forwardPort))
	socksPort := spawnMihomoClient(t, clientCaseDir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    filepath.Join(clientCaseDir, "data"),
		Proxy:      proxy,
	})

	t.Logf("hy2 client socks5 on 127.0.0.1:%d", socksPort)
	status, body := httpGetViaSocks5(t, socksPort, upstream)
	if status < 200 || status >= 400 {
		t.Fatalf("upstream via hy2 %s: status=%d body=%q", tc.name, status, body)
	}
	t.Logf("PASS: %s upstream=%s status=%d", tc.name, upstream, status)
}

// runHy2SubscriptionChainCase walks GetUserSubscriptions → convertHysteria2
// to catch converter-vs-fill drift (e.g. obfs not propagated to client config,
// up/down lost). Same end-to-end probe as the matrix case, but the client
// proxy yaml is built from the real ClashConverter output instead of by hand.
func runHy2SubscriptionChainCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc hy2MatrixCase, upstream string) {
	t.Helper()

	inboundPort := freeUDPPort(t)
	tag := fmt.Sprintf("hy2-chain-%s-%d", tc.name, inboundPort)
	password := fmt.Sprintf("hy2-chain-pass-%d", inboundPort)

	params := map[string]any{
		"protocol":    "hysteria2",
		"port":        inboundPort,
		"password":    password,
		"cert_file":   tc.certFile,
		"key_file":    tc.keyFile,
		"cert_source": "file",
		"sni":         "localhost",
		// self-signed cert generated by writeTempCert: client must skip
		// chain validation. Mark the spec so convertHysteria2 emits
		// skip-cert-verify.
		"skip_cert_verify": true,
	}
	if tc.obfs != "" {
		params["obfs"] = tc.obfs
		params["obfs_password"] = tc.obfsPassword
	}
	if tc.up != "" {
		params["up"] = tc.up
	}
	if tc.down != "" {
		params["down"] = tc.down
	}

	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitUDPListener(fmt.Sprintf("127.0.0.1:%d", inboundPort), 3*time.Second); err != nil {
		t.Fatalf("hy2 udp listener not ready: %v", err)
	}

	username := fmt.Sprintf("alice-hy2-chain-%s-%d", tc.name, inboundPort)
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

	proxy := hy2ProxyFromSpec(t, specs[0])
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
		t.Fatalf("upstream via hy2 subscription chain: status=%d body=%q", status, body)
	}
	t.Logf("PASS: subscription_chain %s upstream=%s status=%d", tc.name, upstream, status)
}

// hy2ProxyFromSpec converts a SubscriptionSpec into a mihomo client proxy
// map by routing it through the real ClashConverter.
func hy2ProxyFromSpec(t *testing.T, spec contracts.SubscriptionSpec) map[string]any {
	t.Helper()
	cp := converter.ConvertHysteria2ForTest(spec)
	if cp == nil {
		t.Fatalf("convertHysteria2 returned nil for spec: %+v", spec)
	}

	p := map[string]any{
		"name":     "hy2-subscription-chain",
		"type":     "hysteria2",
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
	if cp.Obfs != "" {
		p["obfs"] = cp.Obfs
		if cp.ObfsPassword != "" {
			p["obfs-password"] = cp.ObfsPassword
		}
	}
	if cp.Up != "" {
		p["up"] = cp.Up
	}
	if cp.Down != "" {
		p["down"] = cp.Down
	}
	// hy2 must speak h3 ALPN — both ends ship h3 by default. Force it
	// here so a future converter change that drops the default would
	// surface in this test rather than only in production.
	p["alpn"] = []string{"h3"}
	return p
}

// buildHy2ClashProxy builds a hand-rolled mihomo client proxy entry for the
// non-subscription-chain matrix cases. Mirrors what the operator would write
// into a clash config given the inbound's parameters.
//
// `alpn: [h3]` is set explicitly even though both the converter and mihomo
// itself default to h3 — keeping it explicit means a future regression
// where a client side stops defaulting will still fail predictably here
// rather than silently negotiating something else. Same rationale as
// hy2ProxyFromSpec.
func buildHy2ClashProxy(tc hy2MatrixCase, password, server string, port int) map[string]any {
	p := map[string]any{
		"name":             "hy2-test",
		"type":             "hysteria2",
		"server":           server,
		"port":             port,
		"password":         password,
		"sni":              "localhost",
		"skip-cert-verify": true,
		"alpn":             []string{"h3"},
	}
	if tc.obfs != "" {
		p["obfs"] = tc.obfs
		if tc.obfsPassword != "" {
			p["obfs-password"] = tc.obfsPassword
		}
	}
	if tc.up != "" {
		p["up"] = tc.up
	}
	if tc.down != "" {
		p["down"] = tc.down
	}
	return p
}
