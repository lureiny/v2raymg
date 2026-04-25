//go:build integration

package systemtest

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/converter"
)

// randBase64Key returns a standard base64-encoded n-byte random key suitable
// for use as a SIP022 (2022-blake3-*) shadowsocks password.
func randBase64Key(n int) string {
	key := make([]byte, n)
	if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return base64.StdEncoding.EncodeToString(key)
}

// TestMihomoSSMatrix drives Phase 4 Shadowsocks cipher coverage through a
// real mihomo server and a real mihomo client.
//
// Matrix scope: classic AEAD ciphers (aes-256-gcm / chacha20-ietf-poly1305)
// and AEAD-2022 ciphers (2022-blake3-aes-256-gcm / 2022-blake3-aes-128-gcm).
// The plugin cases (obfs / v2ray-plugin / shadow-tls) require external
// infrastructure and are skipped here — covered in unit tests.
//
// The cross-cutting subscription_chain subtest validates that
// GetUserSubscriptions → ClashConverter.convertShadowsocks round-trips
// cleanly; any converter-vs-fill drift (e.g. UDP not propagated) surfaces
// here.
func TestMihomoSSMatrix(t *testing.T) {
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

	cases := []ssMatrixCase{
		{name: "aes-256-gcm", cipher: "aes-256-gcm", password: "mihomo-ss-aes256-" + randHex(8)},
		{name: "chacha20-ietf-poly1305", cipher: "chacha20-ietf-poly1305", password: "mihomo-ss-chacha-" + randHex(8)},
		{name: "2022-aes-256-gcm", cipher: "2022-blake3-aes-256-gcm", password: randBase64Key(32)},
		{name: "2022-aes-128-gcm", cipher: "2022-blake3-aes-128-gcm", password: randBase64Key(16)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runSSMatrixCase(t, rig, mihomoBin, tc, upstream)
		})
	}

	// Cross-cutting: verify the real GetUserSubscriptions → convertShadowsocks
	// subscription chain for a 2022 cipher node. Catches converter-vs-fill
	// drift (e.g. UDP flag, cipher default) that the per-case tests above
	// would miss because they use hand-written params.
	t.Run("subscription_chain_2022", func(t *testing.T) {
		runSSSubscriptionChainCase(t, rig, mihomoBin, ssMatrixCase{
			name:     "subscription-chain-2022-aes-256",
			cipher:   "2022-blake3-aes-256-gcm",
			password: randBase64Key(32),
		}, upstream)
	})
}

type ssMatrixCase struct {
	name     string
	cipher   string
	password string
}

func runSSMatrixCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc ssMatrixCase, upstream string) {
	t.Helper()

	if tc.name != "" {
		// Inline skip guard for shadow-tls (needs external server).
		if tc.name == "shadow-tls" {
			t.Skip("shadow-tls requires external shadow-tls server")
		}
	}

	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	tag := fmt.Sprintf("ss-%s-%d", tc.name, inboundPort)

	params := map[string]any{
		"protocol": "shadowsocks",
		"port":     inboundPort,
		"password": tc.password,
		"cipher":   tc.cipher,
	}

	t.Logf("FastAddInbound %s (cipher=%s) on 127.0.0.1:%d", tag, tc.cipher, inboundPort)
	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 15*time.Second); err != nil {
		t.Fatalf("mihomo SS listener %d not ready: %v", inboundPort, err)
	}

	username := fmt.Sprintf("alice-ss-%d", inboundPort)
	forwardPort := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))

	proxy := map[string]any{
		"name":     tag,
		"type":     "ss",
		"server":   "127.0.0.1",
		"port":     int(forwardPort),
		"password": tc.password,
		"cipher":   tc.cipher,
		"udp":      true,
	}

	clientCaseDir := filepath.Join(t.TempDir(), tc.name)
	if err := os.MkdirAll(clientCaseDir, 0o755); err != nil {
		t.Fatalf("mkdir client case dir: %v", err)
	}
	socksPort := spawnMihomoClient(t, clientCaseDir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    filepath.Join(clientCaseDir, "data"),
		Proxy:      proxy,
	})

	t.Logf("SS client listening socks5 on 127.0.0.1:%d", socksPort)
	status, body := httpGetViaSocks5(t, socksPort, upstream)
	if status < 200 || status >= 400 {
		t.Fatalf("upstream via SS %s: status=%d body=%q", tc.name, status, body)
	}
	t.Logf("PASS: %s upstream=%s status=%d", tc.name, upstream, status)
}

// runSSSubscriptionChainCase exercises the full
// GetUserSubscriptions → ClashConverter.convertShadowsocks path to catch
// converter-vs-fill drift (e.g. UDP not propagated, default cipher mismatch).
func runSSSubscriptionChainCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc ssMatrixCase, upstream string) {
	t.Helper()

	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	tag := fmt.Sprintf("ss-chain-%s-%d", tc.name, inboundPort)

	params := map[string]any{
		"protocol": "shadowsocks",
		"port":     inboundPort,
		"password": tc.password,
		"cipher":   tc.cipher,
	}

	t.Logf("FastAddInbound %s (cipher=%s) on 127.0.0.1:%d", tag, tc.cipher, inboundPort)
	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 15*time.Second); err != nil {
		t.Fatalf("mihomo SS listener %d not ready: %v", inboundPort, err)
	}

	username := fmt.Sprintf("alice-ss-chain-%d", inboundPort)
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

	proxy := ssProxyFromSpec(t, specs[0])

	clientCaseDir := filepath.Join(t.TempDir(), tc.name+"-chain")
	if err := os.MkdirAll(clientCaseDir, 0o755); err != nil {
		t.Fatalf("mkdir client case dir: %v", err)
	}
	socksPort := spawnMihomoClient(t, clientCaseDir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    filepath.Join(clientCaseDir, "data"),
		Proxy:      proxy,
	})

	t.Logf("subscription-chain SS client listening socks5 on 127.0.0.1:%d", socksPort)
	status, body := httpGetViaSocks5(t, socksPort, upstream)
	if status < 200 || status >= 400 {
		t.Fatalf("upstream via subscription-chain SS: status=%d body=%q", status, body)
	}
	t.Logf("PASS: subscription_chain %s upstream=%s status=%d", tc.name, upstream, status)
}

// ssProxyFromSpec converts a SubscriptionSpec into a mihomo client outbound
// proxy map by routing it through the real ClashConverter, catching any
// converter-vs-fill drift.
func ssProxyFromSpec(t *testing.T, spec contracts.SubscriptionSpec) map[string]any {
	t.Helper()
	cp := converter.ConvertSSForTest(spec)
	if cp == nil {
		t.Fatalf("convertShadowsocks returned nil for spec: %+v", spec)
	}

	p := map[string]any{
		"name":     "ss-subscription-chain",
		"type":     "ss",
		"server":   cp.Server,
		"port":     cp.Port,
		"password": cp.Password,
		"cipher":   cp.Cipher,
		"udp":      cp.UDP,
	}
	if cp.Plugin != "" {
		p["plugin"] = cp.Plugin
	}
	return p
}
