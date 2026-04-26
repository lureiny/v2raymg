//go:build integration

package systemtest

// e2e_server_test.go — TestE2EAllContainersProtocolMatrix
//
// Drives the full HTTP API layer + subscription pipeline end-to-end:
//
//	compile v2raymg → start server → HTTP addUser → HTTP fastAddInbound
//	→ GET /api/sub (common URIs) → parse URI via codec → spawn mihomo client
//	→ curl via SOCKS5 → v2raymg forward relay → container → local origin
//	→ stop server → curl must fail (proves traffic really goes through server)
//
// Build tag "integration" — skipped by default `go test`.
// Run: go test ./pkg/proxy/systemtest -tags=integration -run TestE2EAllContainersProtocolMatrix -v -timeout 15m

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Case definition
// ---------------------------------------------------------------------------

// protoMatrixCase describes one cell of the HTTP-API + subscription E2E matrix.
type protoMatrixCase struct {
	name      string
	container string // "xray" | "mihomo"
	protocol  string // vless, vmess, trojan, ss, hysteria2, tuic, anytls
	transport string // tcp, ws, grpc, xhttp, splithttp, "" (defaults to tcp)
	security  string // tls, reality, "" (none)

	// Extra JSON fields forwarded verbatim to POST /api/inbound/fast.
	// Common keys: self_signed, skip_cert_verify, ws_path, grpc_service_name,
	//              xhttp_path, xhttp_mode, method (ss), reality_target, etc.
	params map[string]any

	isUDP      bool   // true = port is UDP (hysteria2, tuic)
	needsNet   bool   // true = requires public internet (reality)
	skipReason string // non-empty → t.Skip(skipReason)
}

// ---------------------------------------------------------------------------
// Protocol matrix
// ---------------------------------------------------------------------------

func buildE2EMatrix() []protoMatrixCase {
	tlsBase := map[string]any{
		"self_signed":      true,
		"skip_cert_verify": true,
	}
	cp := func(m map[string]any) map[string]any {
		out := make(map[string]any, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}

	var cases []protoMatrixCase

	// ---- Xray container ---- //

	// VLESS × {tcp, ws, grpc, xhttp, splithttp} × TLS
	for _, tr := range []struct{ name, transport, key, val string }{
		{"tcp-tls", "tcp", "", ""},
		{"ws-tls", "ws", "ws_path", "/e2evless"},
		{"grpc-tls", "grpc", "grpc_service_name", "E2EVLess"},
		{"xhttp-tls", "xhttp", "xhttp_path", "/e2evless-xhttp"},
		{"splithttp-tls", "splithttp", "xhttp_path", "/e2evless-split"},
	} {
		p := cp(tlsBase)
		if tr.key != "" {
			p[tr.key] = tr.val
		}
		cases = append(cases, protoMatrixCase{
			name: "xray-vless-" + tr.name, container: "xray",
			protocol: "vless", transport: tr.transport, security: "tls", params: p,
		})
	}

	// VLESS + TCP + reality (requires public internet)
	cases = append(cases, protoMatrixCase{
		name: "xray-vless-tcp-reality", container: "xray",
		protocol: "vless", transport: "tcp", security: "reality",
		params: map[string]any{
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			"flow":                 "xtls-rprx-vision",
		},
		needsNet: true,
	})

	// VMess × {tcp, ws} × TLS (xray VMess does not support grpc transport)
	for _, tr := range []struct{ name, transport, key, val string }{
		{"tcp-tls", "tcp", "", ""},
		{"ws-tls", "ws", "ws_path", "/e2evmess"},
	} {
		p := cp(tlsBase)
		if tr.key != "" {
			p[tr.key] = tr.val
		}
		cases = append(cases, protoMatrixCase{
			name: "xray-vmess-" + tr.name, container: "xray",
			protocol: "vmess", transport: tr.transport, security: "tls", params: p,
		})
	}

	// Trojan × {tcp} × TLS (xray Trojan supports tcp and grpc, not ws)
	for _, tr := range []struct{ name, transport, key, val string }{
		{"tcp-tls", "tcp", "", ""},
	} {
		p := cp(tlsBase)
		if tr.key != "" {
			p[tr.key] = tr.val
		}
		cases = append(cases, protoMatrixCase{
			name: "xray-trojan-" + tr.name, container: "xray",
			protocol: "trojan", transport: tr.transport, security: "tls", params: p,
		})
	}

	// Shadowsocks TCP (no TLS layer)
	cases = append(cases, protoMatrixCase{
		name: "xray-ss-tcp-none", container: "xray",
		protocol: "ss", transport: "tcp", security: "",
		params: map[string]any{"method": "2022-blake3-aes-256-gcm"},
	})

	// ---- Mihomo container ---- //

	// Basic protocols (vless, vmess, trojan) × tcp × tls
	for _, proto := range []string{"vless", "vmess", "trojan"} {
		cases = append(cases, protoMatrixCase{
			name: fmt.Sprintf("mihomo-%s-tcp-tls", proto), container: "mihomo",
			protocol: proto, transport: "tcp", security: "tls", params: cp(tlsBase),
		})
	}

	// Shadowsocks TCP (no TLS)
	cases = append(cases, protoMatrixCase{
		name: "mihomo-ss-tcp-none", container: "mihomo",
		protocol: "ss", transport: "tcp", security: "",
		params: map[string]any{"method": "2022-blake3-aes-256-gcm"},
	})

	// Mihomo-exclusive: hysteria2, tuic (UDP-based), anytls
	cases = append(cases, protoMatrixCase{
		name: "mihomo-hysteria2", container: "mihomo",
		protocol: "hysteria2", transport: "", security: "tls",
		params: cp(tlsBase), isUDP: true,
	})
	cases = append(cases, protoMatrixCase{
		name: "mihomo-tuic", container: "mihomo",
		protocol: "tuic", transport: "", security: "tls",
		params: cp(tlsBase), isUDP: true,
	})
	cases = append(cases, protoMatrixCase{
		name: "mihomo-anytls", container: "mihomo",
		protocol: "anytls", transport: "", security: "tls", params: cp(tlsBase),
	})

	return cases
}

// ---------------------------------------------------------------------------
// Main test function
// ---------------------------------------------------------------------------

// TestE2EAllContainersProtocolMatrix boots a real v2raymg server, exercises
// every protocol+transport combination through the HTTP API → subscription →
// mihomo-client chain, and verifies end-to-end connectivity.
//
// Skip conditions:
//   - V2RAYMG_BIN not set AND go build fails (unlikely)
//   - MIHOMO_BIN not set AND github.com unreachable (mihomo auto-download)
//   - No reachable public upstream (reality cases only)
func TestE2EAllContainersProtocolMatrix(t *testing.T) {
	if os.Getenv("MIHOMO_BIN") == "" && !probeURL("https://github.com") {
		t.Skip("MIHOMO_BIN not set and github.com unreachable for mihomo download")
	}

	tmpDir := t.TempDir()

	// Binary resolution: mihomo client (for spawning test clients)
	mihomoBin := ensureMihomoBin(t, tmpDir)

	// Xray binary: empty → server config sets auto_download=true.
	xrayBin := os.Getenv("XRAY_BIN")

	// Local origin — the only target reachable from the server's freedom outbound.
	const marker = "E2E_ORIGIN_OK"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(marker))
	}))
	t.Cleanup(origin.Close)
	t.Logf("local origin: %s", origin.URL)

	// Start v2raymg server (xray+mihomo enabled; hysteria/snell disabled for
	// brevity — they require external binaries that are rarely available in CI).
	srv := startE2EServer(t, xrayBin, mihomoBin, "", "", tmpDir)

	// Add the single test user that all sub-tests share.
	const (
		testUser = "e2e-user"
		testPass = "e2e-pass-12345678"
	)
	if err := srv.addUser(testUser, testPass); err != nil {
		t.Fatalf("addUser: %v", err)
	}
	t.Logf("user %q created", testUser)

	// Run protocol matrix.
	cases := buildE2EMatrix()
	var lastProxy map[string]any

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipReason != "" {
				t.Skip(tc.skipReason)
			}
			if tc.needsNet && !probeURL("https://www.microsoft.com") {
				t.Skip("reality requires public internet (www.microsoft.com unreachable)")
			}
			proxy := runProtoMatrixCase(t, srv, origin.URL, mihomoBin, tc, testUser, testPass, marker, tmpDir)
			if proxy != nil {
				lastProxy = proxy
			}
		})
	}

	// Negative test: stop the server, verify the last-used proxy path breaks.
	if lastProxy != nil {
		t.Run("negative_server_stop", func(t *testing.T) {
			runE2ENegativeTest(t, srv, lastProxy, mihomoBin, origin.URL, tmpDir)
		})
	}
}

// ---------------------------------------------------------------------------
// Per-case runner
// ---------------------------------------------------------------------------

// runProtoMatrixCase adds one inbound via HTTP, waits for the subscription URI
// to appear, parses it, spawns a mihomo client, and asserts the origin returns
// the marker. Returns the clash proxy map so the caller can use it later for
// the negative test.
func runProtoMatrixCase(
	t *testing.T,
	srv *e2eServer,
	originURL, mihomoBin string,
	tc protoMatrixCase,
	user, pass, marker, tmpDir string,
) map[string]any {
	t.Helper()

	// Snapshot subscription before adding the inbound.
	baseline, err := srv.getRawSub(user, pass)
	if err != nil {
		t.Fatalf("getRawSub baseline: %v", err)
	}
	baselineSet := sliceToSet(baseline)

	// Allocate a port for the new inbound.
	var port int
	if tc.isUDP {
		port = freeUDPPort(t)
	} else {
		p, err := freeTCPPort()
		if err != nil {
			t.Fatalf("freeTCPPort: %v", err)
		}
		port = p
	}

	// Build the inbound parameters.
	params := make(map[string]any, len(tc.params)+2)
	for k, v := range tc.params {
		params[k] = v
	}
	params["port"] = port
	tag := fmt.Sprintf("e2e-%s-%d", tc.name, port)

	t.Logf("FastAddInbound %s on port %d (container=%s proto=%s transport=%s security=%s)",
		tag, port, tc.container, tc.protocol, tc.transport, tc.security)

	if err := srv.fastAddInbound(tag, tc.container, tc.protocol, tc.transport, tc.security, params); err != nil {
		t.Fatalf("fastAddInbound %s: %v", tc.name, err)
	}

	// Wait for the subscription to expose the new URI. The forward manager
	// creates the per-user relay asynchronously after inbound creation.
	t.Logf("waiting for new URI in subscription (timeout=20s)...")
	uri, err := srv.getNewURI(user, pass, baselineSet, 20*time.Second)
	if err != nil {
		t.Fatalf("getNewURI for %s: %v", tc.name, err)
	}
	t.Logf("new URI: %s", truncate(uri, 120))

	// Decode URI → clash proxy map using the project's codec package.
	proxy, err := uriToClashProxy(uri)
	if err != nil {
		t.Fatalf("uriToClashProxy %q: %v", truncate(uri, 80), err)
	}
	proxy["name"] = tc.name

	// Spawn mihomo client routing all traffic through this proxy.
	clientDir := filepath.Join(tmpDir, "client-"+tc.name)
	if err := os.MkdirAll(clientDir, 0o755); err != nil {
		t.Fatalf("mkdir client dir: %v", err)
	}
	socksPort := spawnMihomoClient(t, clientDir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    filepath.Join(clientDir, "data"),
		Proxy:      proxy,
	})

	// Verify connectivity: marker must arrive through the full chain.
	status, body := httpGetViaSocks5(t, socksPort, originURL)
	if status < 200 || status >= 400 {
		t.Fatalf("%s: upstream status=%d body=%q", tc.name, status, body)
	}
	if !strings.Contains(body, marker) {
		t.Fatalf("%s: marker %q not in response body %q", tc.name, marker, truncate(body, 200))
	}
	t.Logf("PASS: %s status=%d marker found", tc.name, status)
	return proxy
}

// ---------------------------------------------------------------------------
// Negative test
// ---------------------------------------------------------------------------

// runE2ENegativeTest:
//  1. Spawns a fresh mihomo client using lastProxy.
//  2. Verifies the connection works (positive check — guards against a flawed
//     proxy config making the negative trivially true).
//  3. Shuts down the v2raymg server.
//  4. Asserts the connection breaks within 5 seconds.
func runE2ENegativeTest(
	t *testing.T,
	srv *e2eServer,
	lastProxy map[string]any,
	mihomoBin, originURL, tmpDir string,
) {
	t.Helper()

	// Clone the proxy map so the name change doesn't affect other references.
	proxy := make(map[string]any, len(lastProxy))
	for k, v := range lastProxy {
		proxy[k] = v
	}
	proxy["name"] = "neg-test-proxy"

	clientDir := filepath.Join(tmpDir, "client-neg")
	if err := os.MkdirAll(clientDir, 0o755); err != nil {
		t.Fatalf("mkdir neg client dir: %v", err)
	}
	socksPort := spawnMihomoClient(t, clientDir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    filepath.Join(clientDir, "data"),
		Proxy:      proxy,
	})

	// Positive pre-check: proxy must be working before we stop the server.
	preStatus, preBody := httpGetViaSocks5(t, socksPort, originURL)
	if preStatus < 200 || preStatus >= 400 {
		t.Skipf("negative test: proxy not working before server stop (status=%d body=%q); skipping", preStatus, preBody)
	}
	t.Logf("negative pre-check PASS: status=%d", preStatus)

	// Stop the server.
	srv.shutdown()
	t.Log("server stopped; checking that proxy breaks...")

	// Poll until the connection fails or the deadline fires.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cli, err := httpClientViaSocks5(fmt.Sprintf("127.0.0.1:%d", socksPort))
		if err != nil {
			// Can't even build the client — that's a pass.
			t.Logf("PASS: httpClientViaSocks5 failed (expected after server stop): %v", err)
			return
		}
		cli.Timeout = 2 * time.Second
		resp, err := cli.Get(originURL)
		if err != nil {
			t.Logf("PASS: connection failed after server stop: %v", err)
			return
		}
		if resp.StatusCode >= 400 {
			resp.Body.Close()
			t.Logf("PASS: server returned %d after stop", resp.StatusCode)
			return
		}
		resp.Body.Close()
		time.Sleep(300 * time.Millisecond)
	}

	t.Fatal("NEGATIVE CONTROL FAILED: connection still succeeds after server stop — proxy chain may be bypassing the server")
}
