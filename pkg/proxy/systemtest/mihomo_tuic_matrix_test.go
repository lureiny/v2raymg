//go:build integration

package systemtest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/converter"
)

// TestMihomoTuicMatrix drives Phase 6 TUIC coverage end-to-end through a
// real mihomo server (the listener) and a real mihomo client (the outbound),
// out to a public upstream via the forward layer.
//
// Matrix scope (Phase 6 plan T-M-P6-66):
//   - tls baseline (defaults: bbr congestion control, native udp-relay)
//   - tls + bbr + reduce-rtt (zero_rtt_handshake → client reduce-rtt)
//   - tls + cubic (different congestion controller, exercises whitelist)
//
// Plus one cross-cutting subscription_chain subtest exercising
// GetUserSubscriptions → ClashConverter.convertTuic; mandatory per the
// "systemtest must walk a real subscription pipeline" memory rule.
//
// TUIC is QUIC-over-UDP; the forward rule is a UDPRelay
// (forwardNetworkForProtocol(ProtocolTUIC)→"udp"). A regression that
// returns to TCP would surface here as a connect failure.
func TestMihomoTuicMatrix(t *testing.T) {
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

	cases := []tuicMatrixCase{
		{name: "tls-baseline", certFile: certPath, keyFile: keyPath},
		{
			name:                 "tls-bbr-reducertt",
			certFile:             certPath,
			keyFile:              keyPath,
			congestionController: "bbr",
			zeroRTT:              true,
		},
		{
			// cubic exercises the non-default congestion controller path —
			// guards against silent rejection if mihomo Alpha tightens the
			// listener-side whitelist or changes how parse-time defaults
			// interact with our explicit profilegen value.
			name:                 "tls-cubic",
			certFile:             certPath,
			keyFile:              keyPath,
			congestionController: "cubic",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runTuicMatrixCase(t, rig, mihomoBin, tc, upstream)
		})
	}

	t.Run("subscription_chain_tls_bbr", func(t *testing.T) {
		runTuicSubscriptionChainCase(t, rig, mihomoBin, tuicMatrixCase{
			name:                 "subscription-chain-bbr",
			certFile:             certPath,
			keyFile:              keyPath,
			congestionController: "bbr",
			zeroRTT:              true,
		}, upstream)
	})
}

type tuicMatrixCase struct {
	name string

	certFile string
	keyFile  string

	congestionController string
	udpRelayMode         string
	zeroRTT              bool
}

// runTuicMatrixCase wires up the per-case mihomo server inbound, allocates a
// forward UDP port via the user manager, then spins a mihomo client that
// routes traffic through that port out to the upstream.
func runTuicMatrixCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc tuicMatrixCase, upstream string) {
	t.Helper()

	inboundPort := freeUDPPort(t)
	tag := fmt.Sprintf("tuic-%s-%d", tc.name, inboundPort)
	tuicUUID := uuid.NewString()
	password := fmt.Sprintf("tuic-pass-%d", inboundPort)

	params := map[string]any{
		"protocol":    "tuic",
		"port":        inboundPort,
		"uuid":        tuicUUID,
		"password":    password,
		"cert_file":   tc.certFile,
		"key_file":    tc.keyFile,
		"cert_source": "file",
	}
	if tc.congestionController != "" {
		params["congestion_controller"] = tc.congestionController
	}
	if tc.udpRelayMode != "" {
		params["udp_relay_mode"] = tc.udpRelayMode
	}
	if tc.zeroRTT {
		params["zero_rtt_handshake"] = true
	}

	t.Logf("FastAddInbound %s on 127.0.0.1:%d (cc=%q udp=%q reduce_rtt=%v)",
		tag, inboundPort, tc.congestionController, tc.udpRelayMode, tc.zeroRTT)
	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitUDPListener(fmt.Sprintf("127.0.0.1:%d", inboundPort), 3*time.Second); err != nil {
		t.Fatalf("tuic udp listener not ready: %v", err)
	}

	username := fmt.Sprintf("alice-tuic-%s-%d", tc.name, inboundPort)
	forwardPort := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))

	clientCaseDir := filepath.Join(t.TempDir(), tc.name)
	if err := os.MkdirAll(clientCaseDir, 0o755); err != nil {
		t.Fatalf("mkdir client case dir: %v", err)
	}
	proxy := buildTuicClashProxy(tc, tuicUUID, password, "127.0.0.1", int(forwardPort))
	socksPort := spawnMihomoClient(t, clientCaseDir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    filepath.Join(clientCaseDir, "data"),
		Proxy:      proxy,
	})

	t.Logf("tuic client socks5 on 127.0.0.1:%d", socksPort)
	status, body := httpGetViaSocks5(t, socksPort, upstream)
	if status < 200 || status >= 400 {
		t.Fatalf("upstream via tuic %s: status=%d body=%q", tc.name, status, body)
	}
	t.Logf("PASS: %s upstream=%s status=%d", tc.name, upstream, status)
}

// runTuicSubscriptionChainCase walks GetUserSubscriptions → convertTuic to
// catch converter-vs-fill drift (e.g. uuid not propagated to client config,
// reduce-rtt key dropped). Same end-to-end probe as the matrix case, but
// the client proxy yaml is built from the real ClashConverter output rather
// than by hand.
func runTuicSubscriptionChainCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc tuicMatrixCase, upstream string) {
	t.Helper()

	inboundPort := freeUDPPort(t)
	tag := fmt.Sprintf("tuic-chain-%s-%d", tc.name, inboundPort)
	tuicUUID := uuid.NewString()
	password := fmt.Sprintf("tuic-chain-pass-%d", inboundPort)

	params := map[string]any{
		"protocol":         "tuic",
		"port":             inboundPort,
		"uuid":             tuicUUID,
		"password":         password,
		"cert_file":        tc.certFile,
		"key_file":         tc.keyFile,
		"cert_source":      "file",
		"sni":              "localhost",
		"skip_cert_verify": true,
	}
	if tc.congestionController != "" {
		params["congestion_controller"] = tc.congestionController
	}
	if tc.zeroRTT {
		params["zero_rtt_handshake"] = true
	}

	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitUDPListener(fmt.Sprintf("127.0.0.1:%d", inboundPort), 3*time.Second); err != nil {
		t.Fatalf("tuic udp listener not ready: %v", err)
	}

	username := fmt.Sprintf("alice-tuic-chain-%s-%d", tc.name, inboundPort)
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

	proxy := tuicProxyFromSpec(t, specs[0])
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
		t.Fatalf("upstream via tuic subscription chain: status=%d body=%q", status, body)
	}
	t.Logf("PASS: subscription_chain %s upstream=%s status=%d", tc.name, upstream, status)
}

// tuicProxyFromSpec converts a SubscriptionSpec into a mihomo client proxy
// map by routing it through the real ClashConverter. Mirrors hy2ProxyFromSpec
// so a future drift between subscription/converter and client schema falls
// out here.
func tuicProxyFromSpec(t *testing.T, spec contracts.SubscriptionSpec) map[string]any {
	t.Helper()
	cp := converter.ConvertTuicForTest(spec)
	if cp == nil {
		t.Fatalf("convertTuic returned nil for spec: %+v", spec)
	}

	p := map[string]any{
		"name":     "tuic-subscription-chain",
		"type":     "tuic",
		"server":   cp.Server,
		"port":     cp.Port,
		"uuid":     cp.UUID,
		"password": cp.Password,
		"udp":      cp.UDP,
	}
	if cp.SNI != "" {
		p["sni"] = cp.SNI
	}
	if cp.SkipCertVerify {
		p["skip-cert-verify"] = true
	}
	if cp.CongestionController != "" {
		p["congestion-controller"] = cp.CongestionController
	}
	if cp.UDPRelayMode != "" {
		p["udp-relay-mode"] = cp.UDPRelayMode
	}
	if cp.ReduceRTT {
		p["reduce-rtt"] = true
	}
	if cp.HeartbeatInterval > 0 {
		p["heartbeat-interval"] = cp.HeartbeatInterval
	}
	if cp.DisableSNI {
		p["disable-sni"] = true
	}
	// TUIC speaks h3 ALPN on both ends by default. Force it explicitly so
	// a future converter regression that drops the default surfaces here.
	p["alpn"] = []string{"h3"}
	return p
}

// buildTuicClashProxy builds a hand-rolled mihomo client proxy entry for the
// non-subscription-chain matrix cases. Mirrors what an operator would write
// into a clash config given the inbound's parameters.
func buildTuicClashProxy(tc tuicMatrixCase, tuicUUID, password, server string, port int) map[string]any {
	p := map[string]any{
		"name":             "tuic-test",
		"type":             "tuic",
		"server":           server,
		"port":             port,
		"uuid":             tuicUUID,
		"password":         password,
		"sni":              "localhost",
		"skip-cert-verify": true,
		"alpn":             []string{"h3"},
	}
	if tc.congestionController != "" {
		p["congestion-controller"] = tc.congestionController
	}
	if tc.udpRelayMode != "" {
		p["udp-relay-mode"] = tc.udpRelayMode
	}
	if tc.zeroRTT {
		p["reduce-rtt"] = true
	}
	return p
}
