//go:build integration

package systemtest

import (
	"encoding/json"
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

	"github.com/lureiny/v2raymg/pkg/proxy/containers/xray"
)

// fastAddCase describes a single FastAdd + E2E connectivity test.
type fastAddCase struct {
	Name        string
	Protocol    string
	Params      map[string]any // FastAdd params (excluding tag/port)
	NeedsCert   bool           // needs TLS cert files
	UsesReality bool           // uses Reality (needs external network for dest)
}

func allFastAddCases() []fastAddCase {
	return []fastAddCase{
		// --- VLESS ---
		{Name: "vless-tcp-tls", Protocol: "vless", NeedsCert: true, Params: map[string]any{
			"transport": "tcp", "security": "tls", "server_name": "localhost",
		}},
		{Name: "vless-tcp-reality", Protocol: "vless", UsesReality: true, Params: map[string]any{
			"transport": "tcp", "security": "reality",
			"server_name": "www.microsoft.com", "reality_target": "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
		}},
		{Name: "vless-ws-tls", Protocol: "vless", NeedsCert: true, Params: map[string]any{
			"transport": "ws", "security": "tls", "server_name": "localhost",
			"ws_path": "/wstest",
		}},
		{Name: "vless-grpc-tls", Protocol: "vless", NeedsCert: true, Params: map[string]any{
			"transport": "grpc", "security": "tls", "server_name": "localhost",
			"grpc_service_name": "testgrpc", "alpn": []string{"h2"},
		}},
		{Name: "vless-grpc-reality", Protocol: "vless", UsesReality: true, Params: map[string]any{
			"transport": "grpc", "security": "reality",
			"server_name": "www.microsoft.com", "reality_target": "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			"grpc_service_name":    "testgrpc",
		}},
		{Name: "vless-httpupgrade-tls", Protocol: "vless", NeedsCert: true, Params: map[string]any{
			"transport": "httpupgrade", "security": "tls", "server_name": "localhost",
			"httpupgrade_path": "/upgrade", "alpn": []string{"http/1.1"},
		}},
		{Name: "vless-xhttp-tls", Protocol: "vless", NeedsCert: true, Params: map[string]any{
			"transport": "xhttp", "security": "tls", "server_name": "localhost",
			"xhttp_path": "/xhttptest", "xhttp_mode": "auto",
		}},
		{Name: "vless-xhttp-reality", Protocol: "vless", UsesReality: true, Params: map[string]any{
			"transport": "xhttp", "security": "reality",
			"server_name": "www.microsoft.com", "reality_target": "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			"xhttp_path":           "/xhttptest",
		}},
		{Name: "vless-splithttp-tls", Protocol: "vless", NeedsCert: true, Params: map[string]any{
			"transport": "splithttp", "security": "tls", "server_name": "localhost",
			"xhttp_path": "/splittest",
		}},
		{Name: "vless-splithttp-reality", Protocol: "vless", UsesReality: true, Params: map[string]any{
			"transport": "splithttp", "security": "reality",
			"server_name": "www.microsoft.com", "reality_target": "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			"xhttp_path":           "/splittest",
		}},

		// --- VMess ---
		{Name: "vmess-tcp-tls", Protocol: "vmess", NeedsCert: true, Params: map[string]any{
			"transport": "tcp", "security": "tls", "server_name": "localhost",
		}},
		{Name: "vmess-ws-tls", Protocol: "vmess", NeedsCert: true, Params: map[string]any{
			"transport": "ws", "security": "tls", "server_name": "localhost",
			"ws_path": "/vmessws",
		}},

		// --- Trojan ---
		{Name: "trojan-tcp-tls", Protocol: "trojan", NeedsCert: true, Params: map[string]any{
			"transport": "tcp", "security": "tls", "server_name": "localhost",
		}},

		// --- Shadowsocks ---
		{Name: "ss-tcp-none", Protocol: "shadowsocks", Params: map[string]any{
			"transport": "tcp", "security": "none",
			"method": "2022-blake3-aes-256-gcm",
		}},
	}
}

// TestFastAddConnectivity tests all FastAdd protocol combinations end-to-end.
// It is fully self-contained: xray binary is auto-downloaded, no XRAY_BIN needed.
//
// Verification strategy:
//   - A local httptest server is the ONLY test target (not google.com).
//   - The client xray config has NO freedom outbound — the proxy outbound is the sole exit.
//   - If the client→server tunnel is broken, the client has no path to the target → test fails.
//   - Reality tests additionally need external network (for server-side TLS dest handshake).
func TestFastAddConnectivity(t *testing.T) {
	// Start local HTTP server as the test target.
	// Only reachable from server xray's freedom outbound on localhost.
	const marker = "FASTADD_CHAIN_OK"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(marker))
	}))
	defer origin.Close()
	targetURL := origin.URL // e.g. "http://127.0.0.1:xxxxx"
	t.Logf("Local test target: %s", targetURL)

	// Reality tests require external network (dest handshake). Check once.
	hasNetwork := checkNetwork() == nil

	tmpDir := t.TempDir()

	// Prepare TLS cert
	certPath, keyPath, _, _, err := writeTempCert(tmpDir)
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	// Prepare server xray binary path and config
	xrayBin := filepath.Join(tmpDir, "xray")
	apiPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc API port: %v", err)
	}
	serverCfgPath := filepath.Join(tmpDir, "server.json")
	if err := writeMinimalXrayConfig(serverCfgPath, apiPort); err != nil {
		t.Fatalf("write server config: %v", err)
	}

	// Create server Executor with AutoDownload — binary will be downloaded on Start()
	serverExec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: serverCfgPath,
		GRPCAPIAddress: fmt.Sprintf("127.0.0.1:%d", apiPort),
		AutoDownload:   true,
		UserManager:    newTestUserManager(),
	})
	if err != nil {
		t.Fatalf("create server executor: %v", err)
	}

	t.Log("Starting server xray (will auto-download binary if needed)...")
	if err := serverExec.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer serverExec.Stop()

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", apiPort), 30*time.Second); err != nil {
		t.Fatalf("server API not ready: %v", err)
	}
	t.Logf("Server ready (API port %d, binary %s)", apiPort, xrayBin)

	// Run each protocol combination: positive test + negative control
	for _, tc := range allFastAddCases() {
		t.Run(tc.Name, func(t *testing.T) {
			if tc.UsesReality && !hasNetwork {
				t.Skip("Reality requires external network for dest handshake")
			}
			// Positive: real server → must succeed
			runFastAddSubtest(t, tc, xrayBin, serverExec, certPath, keyPath, tmpDir, targetURL, marker)
			// Negative: dead server port → must fail (proves chain is real)
			runNegativeControl(t, tc, xrayBin, tmpDir, targetURL)
		})
	}
}

func runFastAddSubtest(
	t *testing.T,
	tc fastAddCase,
	xrayBin string,
	serverExec *xray.Executor,
	certPath, keyPath, tmpDir string,
	targetURL, marker string,
) {
	// Allocate ports
	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc inbound port: %v", err)
	}
	socksPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc socks port: %v", err)
	}

	// Build FastAdd params
	tag := fmt.Sprintf("fa-%s-%d", tc.Name, inboundPort)
	params := make(map[string]any)
	for k, v := range tc.Params {
		params[k] = v
	}
	params["protocol"] = tc.Protocol
	params["port"] = inboundPort

	// Inject cert paths for TLS cases
	if tc.NeedsCert {
		params["certificateFile"] = certPath
		params["keyFile"] = keyPath
	}

	// FastAdd on server
	t.Logf("FastAdd %s on port %d", tc.Name, inboundPort)
	if err := serverExec.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound failed: %v", err)
	}
	defer serverExec.RemoveInboundNative(tag)

	// Wait for inbound port
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 10*time.Second); err != nil {
		t.Fatalf("inbound port %d not ready: %v", inboundPort, err)
	}

	// Get inbound info for client config (UUID, Reality keys, etc.)
	inboundInfo, err := serverExec.GetInboundConfig(tag)
	if err != nil {
		t.Fatalf("GetInboundConfig: %v", err)
	}
	extra := inboundInfo.Extra()

	// Build and write client config
	clientCfg := buildClientConfig(tc, inboundPort, socksPort, extra)
	clientCfgPath := filepath.Join(tmpDir, fmt.Sprintf("client-%s.json", tc.Name))
	if err := writeJSONConfig(clientCfgPath, clientCfg); err != nil {
		t.Fatalf("write client config: %v", err)
	}

	// Start client xray process (uses the same binary auto-downloaded by server)
	cmd := exec.Command(xrayBin, "run", "-c", clientCfgPath)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start client xray: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	// Wait for socks port
	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitPort(socksAddr, 10*time.Second); err != nil {
		t.Fatalf("client socks port %d not ready: %v", socksPort, err)
	}

	// Test connectivity: HTTP GET via SOCKS5 → client xray → server xray → local httptest
	// The client has NO freedom outbound, so if the tunnel breaks, request MUST fail.
	client, err := httpClientViaSocks5(socksAddr)
	if err != nil {
		t.Fatalf("create socks5 client: %v", err)
	}
	resp, err := client.Get(targetURL)
	if err != nil {
		t.Fatalf("HTTP GET failed (chain broken?): %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), marker) {
		t.Fatalf("response does not contain marker %q — traffic did not reach local server: %s", marker, string(body))
	}

	t.Logf("PASS (marker received through proxy chain)")
}

// runNegativeControl proves that the same protocol combo FAILS when the proxy
// outbound points to a port where no server is listening.
// This confirms the positive test's success is not a false positive.
func runNegativeControl(t *testing.T, tc fastAddCase, xrayBin, tmpDir, targetURL string) {
	t.Helper()
	socksPort, _ := freeTCPPort()
	deadPort, _ := freeTCPPort() // allocated but nothing listening

	// Build minimal valid extra so xray client can start (valid config structure)
	// but the server port is dead so the tunnel cannot establish.
	extra := map[string]any{}
	switch tc.Protocol {
	case "vless", "vmess":
		extra["users"] = []map[string]any{{"uuid": "00000000-0000-0000-0000-000000000000"}}
	case "trojan":
		extra["users"] = []map[string]any{{"password": "dummy"}}
	case "shadowsocks":
		// 2022 methods need a valid base64 key (32 bytes for aes-256)
		fakeKey := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
		extra["users"] = []map[string]any{{"password": fakeKey, "method": "2022-blake3-aes-256-gcm"}}
		extra["method"] = "2022-blake3-aes-256-gcm"
		extra["password"] = fakeKey
	}
	// Reality needs a valid publicKey for client to start
	if tc.UsesReality {
		extra["reality_public_key"] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		extra["reality_short_ids"] = []string{"0123456789abcdef"}
	}

	clientCfg := buildClientConfig(tc, deadPort, socksPort, extra)
	clientCfgPath := filepath.Join(tmpDir, fmt.Sprintf("client-neg-%s.json", tc.Name))
	writeJSONConfig(clientCfgPath, clientCfg)

	cmd := exec.Command(xrayBin, "run", "-c", clientCfgPath)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Logf("negative: client start failed (acceptable): %v", err)
		return
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()

	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitPort(socksAddr, 5*time.Second); err != nil {
		t.Logf("negative: socks not ready (acceptable): %v", err)
		return
	}

	client, _ := httpClientViaSocks5(socksAddr)
	_, err := client.Get(targetURL)
	if err == nil {
		t.Fatal("NEGATIVE CONTROL FAILED: request succeeded through dead proxy port — test is not trustworthy")
	}
	t.Logf("negative control OK: dead proxy → %v", err)
}

// ---------------------------------------------------------------------------
// Client config builders
// ---------------------------------------------------------------------------

func buildClientConfig(tc fastAddCase, serverPort, socksPort int, extra map[string]any) map[string]any {
	outbound := buildClientOutbound(tc, serverPort, extra)
	// Only the proxy outbound — NO freedom/direct outbound.
	// This guarantees all traffic MUST go through the server xray tunnel.
	// If the tunnel is broken, requests fail instead of silently going direct.
	return map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []map[string]any{{
			"tag": "socks-in", "protocol": "socks",
			"listen": "127.0.0.1", "port": socksPort,
			"settings": map[string]any{"udp": false},
		}},
		"outbounds": []map[string]any{outbound},
	}
}

func buildClientOutbound(tc fastAddCase, serverPort int, extra map[string]any) map[string]any {
	outbound := map[string]any{
		"tag":      "proxy",
		"protocol": tc.Protocol,
	}

	// Protocol-specific settings
	switch tc.Protocol {
	case "vless":
		uuid := extractUUID(extra)
		flow := extractFlow(extra)
		user := map[string]any{"id": uuid, "encryption": "none"}
		if flow != "" {
			user["flow"] = flow
		}
		outbound["settings"] = map[string]any{
			"vnext": []map[string]any{{
				"address": "127.0.0.1", "port": serverPort,
				"users": []map[string]any{user},
			}},
		}

	case "vmess":
		uuid := extractUUID(extra)
		outbound["settings"] = map[string]any{
			"vnext": []map[string]any{{
				"address": "127.0.0.1", "port": serverPort,
				"users": []map[string]any{{"id": uuid, "security": "auto", "alterId": 0}},
			}},
		}

	case "trojan":
		password := extractPassword(extra)
		outbound["settings"] = map[string]any{
			"servers": []map[string]any{{
				"address": "127.0.0.1", "port": serverPort, "password": password,
			}},
		}

	case "shadowsocks":
		method, password := extractSSCredentials(extra)
		outbound["settings"] = map[string]any{
			"servers": []map[string]any{{
				"address": "127.0.0.1", "port": serverPort,
				"method": method, "password": password,
			}},
		}
	}

	// Stream settings
	if stream := buildClientStream(tc, extra); stream != nil {
		outbound["streamSettings"] = stream
	}

	return outbound
}

func buildClientStream(tc fastAddCase, extra map[string]any) map[string]any {
	transport := extStr(tc.Params, "transport")
	security := extStr(tc.Params, "security")

	if transport == "" {
		transport = "tcp"
	}
	// No stream settings for plain TCP
	if (security == "" || security == "none") && transport == "tcp" {
		return nil
	}

	stream := map[string]any{"network": transport}

	// Security layer
	switch security {
	case "tls":
		stream["security"] = "tls"
		tlsSettings := map[string]any{
			"allowInsecure": true,
			"serverName":    "localhost",
		}
		if alpn, ok := tc.Params["alpn"].([]string); ok {
			tlsSettings["alpn"] = alpn
		}
		stream["tlsSettings"] = tlsSettings

	case "reality":
		stream["security"] = "reality"
		sni := extStr(tc.Params, "server_name")
		pubKey := extStr(extra, "reality_public_key")
		shortID := ""
		if ids, ok := extra["reality_short_ids"].([]string); ok && len(ids) > 0 {
			shortID = ids[0]
		}
		stream["realitySettings"] = map[string]any{
			"serverName":  sni,
			"publicKey":   pubKey,
			"shortId":     shortID,
			"fingerprint": "chrome",
		}
	}

	// Transport-specific settings
	switch transport {
	case "ws":
		ws := map[string]any{}
		if p := firstNonEmpty(extStr(tc.Params, "ws_path"), extStr(extra, "ws_path")); p != "" {
			ws["path"] = p
		}
		stream["wsSettings"] = ws

	case "grpc":
		grpc := map[string]any{}
		if sn := firstNonEmpty(extStr(tc.Params, "grpc_service_name"), extStr(extra, "grpc_service_name")); sn != "" {
			grpc["serviceName"] = sn
		}
		stream["grpcSettings"] = grpc

	case "httpupgrade":
		hu := map[string]any{}
		if p := firstNonEmpty(extStr(tc.Params, "httpupgrade_path"), extStr(extra, "httpupgrade_path")); p != "" {
			hu["path"] = p
		}
		stream["httpupgradeSettings"] = hu

	case "xhttp", "splithttp":
		sh := map[string]any{}
		if p := firstNonEmpty(extStr(tc.Params, "xhttp_path"), extStr(extra, "xhttp_path")); p != "" {
			sh["path"] = p
		}
		stream["splithttpSettings"] = sh
	}

	return stream
}

// ---------------------------------------------------------------------------
// Credential extractors
// ---------------------------------------------------------------------------

func extractUUID(extra map[string]any) string {
	if users, ok := extra["users"].([]map[string]any); ok && len(users) > 0 {
		if uuid, ok := users[0]["uuid"].(string); ok {
			return uuid
		}
	}
	return extStr(extra, "uuid")
}

func extractFlow(extra map[string]any) string {
	if users, ok := extra["users"].([]map[string]any); ok && len(users) > 0 {
		if flow, ok := users[0]["flow"].(string); ok {
			return flow
		}
	}
	return ""
}

func extractPassword(extra map[string]any) string {
	if users, ok := extra["users"].([]map[string]any); ok && len(users) > 0 {
		if pw, ok := users[0]["password"].(string); ok {
			return pw
		}
	}
	return extStr(extra, "password")
}

func extractSSCredentials(extra map[string]any) (method, password string) {
	if users, ok := extra["users"].([]map[string]any); ok && len(users) > 0 {
		password, _ = users[0]["password"].(string)
		method, _ = users[0]["method"].(string)
	}
	if password == "" {
		password = extStr(extra, "password")
	}
	if method == "" {
		method = extStr(extra, "method")
	}
	return
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func extStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func writeJSONConfig(path string, data any) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
