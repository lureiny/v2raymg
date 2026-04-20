//go:build integration

// Integration test for the hysteria container's UDP forward path.
//
// Topology under test:
//
//	curl -> hysteria client (SOCKS5) -> forward UDPRelay -> hysteria server (loopback) -> google.com
//
// What this validates:
//  1. HysteriaContainer.Start() launches the hysteria process on loopback.
//  2. UserManager.AddUser emits UserEventAdd; HysteriaContainer.handleUserEvent
//     calls GetBindPort(Network: "udp"); ForwardManager.AddRule creates a
//     forward.UDPRelay listening on a public port and forwarding to the
//     container's loopback port.
//  3. A real hysteria2 client connects to the forward port, obtains SOCKS5,
//     and can reach the internet via the full chain.
//  4. Stopping the container makes the chain fail — proving the curl really
//     goes through the hysteria server (not a bypass / direct connection).
//
// Requires:
//   - HYSTERIA_BIN env var pointing at a hysteria2 v2.x binary.
//   - Outbound internet access to fetch https://www.google.com.
//
// Run:
//
//	HYSTERIA_BIN=/tmp/hysteria-test/hysteria \
//	  go test ./pkg/proxy/systemtest -tags=integration \
//	  -run TestHysteriaUDPForwardIntegration -v -count=1 -timeout=120s
package systemtest

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/containers/hysteria"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/forward"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// hysteriaDefaultInboundTag mirrors the private constant in the hysteria
// package; duplicated here because the constant is unexported. The test fails
// loudly if the package ever renames its default tag.
const hysteriaDefaultInboundTag = "hysteria-default"

// freeUDPPort allocates a free UDP port by binding to :0 and immediately
// closing. Mirrors the freeTCPPort helper in helpers_test.go.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("alloc UDP port: %v", err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	_ = pc.Close()
	return port
}

// startMockHysteriaAuth runs a tiny HTTP server that answers
// /api/authHysteria2 by echoing back {ok:true,id:<auth>}. The real HTTP server
// in pkg/http matches against UserManager.ListUsersWithPasswd; for this test
// we accept any credential so we don't depend on the HTTP stack. Returns the
// listening port and a shutdown func.
func startMockHysteriaAuth(t *testing.T) (int, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen mock auth: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/authHysteria2", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Addr string `json:"addr"`
			Auth string `json:"auth"`
			TX   int64  `json:"tx"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"id": req.Auth,
		})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	port := ln.Addr().(*net.TCPAddr).Port
	return port, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}

// waitHysteriaProcessListening polls for the hysteria server's loopback UDP
// socket by trying a dummy QUIC-like packet. The server silently discards it
// (no valid QUIC header) but the OS will ECONNREFUSED if nobody owns the
// port. We approximate by checking that ListenPacket on the same port fails,
// i.e. the port is already bound by somebody.
func waitHysteriaProcessListening(loopbackPort int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", loopbackPort)
	for time.Now().Before(deadline) {
		pc, err := net.ListenPacket("udp", addr)
		if err != nil {
			// Someone else (our hysteria process) owns the port.
			return nil
		}
		_ = pc.Close()
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("hysteria process not listening on %s after %s", addr, timeout)
}

// waitForwardRule polls the ForwardManager for a rule matching the given
// (container, inbound, user) tuple.
func waitForwardRule(fm forward.ForwardManager, ruleKey string, timeout time.Duration) *forward.ForwardRule {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if rule := fm.GetRule(ruleKey); rule != nil {
			return rule
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

func TestHysteriaUDPForwardIntegration(t *testing.T) {
	hysteriaBin := os.Getenv("HYSTERIA_BIN")
	if hysteriaBin == "" {
		t.Skip("integration test skipped: HYSTERIA_BIN not set")
	}
	if _, err := os.Stat(hysteriaBin); err != nil {
		t.Skipf("integration test skipped: HYSTERIA_BIN invalid: %v", err)
	}
	if err := checkNetwork(); err != nil {
		t.Skipf("integration test skipped: no internet: %v", err)
	}

	tmpDir := t.TempDir()

	// 1. TLS materials. HysteriaContainer bypasses certMgr when CertFile &
	//    KeyFile are set, so this is the simplest path for tests.
	certPath, keyPath, _, _, err := writeTempCert(tmpDir)
	if err != nil {
		t.Fatalf("write cert: %v", err)
	}

	// 2. Mock HTTP auth endpoint. The hysteria server config generated by the
	//    container uses `auth: http -> URL: http://127.0.0.1:<httpPort>/api/authHysteria2`
	//    and blocks clients until it gets a 200 from that endpoint.
	httpPort, shutdownAuth := startMockHysteriaAuth(t)
	defer shutdownAuth()
	t.Logf("mock hysteria auth server on 127.0.0.1:%d", httpPort)

	// 3. Pre-allocate loopback ports for the hysteria process + traffic stats.
	hysteriaBackendPort := freeUDPPort(t)
	trafficStatsPort := freeTCPPort_(t)
	t.Logf("hysteria backend port (loopback UDP): %d", hysteriaBackendPort)

	// 4. Real ForwardManager. Use a narrow port range so the test doesn't
	//    conflict with other services. UDPRelay listens on the allocated port.
	fm, err := forward.NewForwardManager(forward.WithPortRange(34000, 35000))
	if err != nil {
		t.Fatalf("new forward manager: %v", err)
	}
	defer fm.Close()

	// 5. Real UserManager wired to the ForwardManager.
	um := usermanager.NewUserManager(fm, "test-node")

	// 6. Build HysteriaContainer. Direct cert path + user manager + HTTPPort
	//    exercises the production hooks (startUserEventHandler,
	//    startReconcileLoop, waitForCertAndStart → startProcess).
	cfg := hysteria.HysteriaConfig{
		BinaryPath:         hysteriaBin,
		ConfigFilePath:     filepath.Join(tmpDir, "hysteria-server.yaml"),
		Listen:             "127.0.0.1", // cosmetic: process.go hardcodes 127.0.0.1
		Port:               hysteriaBackendPort,
		Domain:             "localhost",
		CertFile:           certPath,
		KeyFile:            keyPath,
		TrafficStatsPort:   trafficStatsPort,
		TrafficStatsSecret: "testsecret",
		Version:            "app/v2.7.1",
	}
	hc, err := hysteria.NewHysteriaContainer(cfg,
		hysteria.WithUserManager(um),
		hysteria.WithHTTPPort(httpPort),
	)
	if err != nil {
		t.Fatalf("new hysteria container: %v", err)
	}

	// 7. Start the container. waitForCertAndStart runs in a background
	//    goroutine (see container.go), so Start() returns before the hysteria
	//    process is actually listening — poll for the UDP socket.
	if err := hc.Start(); err != nil {
		t.Fatalf("container start: %v", err)
	}
	defer func() {
		if err := hc.Stop(); err != nil {
			t.Logf("container stop (defer): %v", err)
		}
	}()

	if err := waitHysteriaProcessListening(hysteriaBackendPort, 15*time.Second); err != nil {
		t.Fatalf("hysteria process not ready: %v", err)
	}
	t.Logf("hysteria process up on 127.0.0.1:%d", hysteriaBackendPort)

	// 8. Add a user. UserManager emits UserEventAdd; HysteriaContainer's
	//    handleUserEvent reacts by calling GetBindPort(Network:"udp").
	const username = "udpuser"
	if err := um.AddUser(usermanager.AddUserRequest{
		Username: username,
		Password: "unused",
	}); err != nil {
		t.Fatalf("add user: %v", err)
	}

	// 9. Wait for the UDP forward rule to land in the ForwardManager. This
	//    proves the container event handler ran and the "udp" network path
	//    was honored end-to-end.
	ruleKey := string(contracts.ContainerHysteria) + ":" + hysteriaDefaultInboundTag + ":" + username
	rule := waitForwardRule(fm, ruleKey, 5*time.Second)
	if rule == nil {
		t.Fatalf("forward rule %q not created within 5s", ruleKey)
	}
	if got := rule.ResolvedNetwork(); got != forward.NetworkUDP {
		t.Fatalf("forward rule network = %q, want udp", got)
	}
	wantTarget := fmt.Sprintf("127.0.0.1:%d", hysteriaBackendPort)
	if rule.TargetAddr != wantTarget {
		t.Fatalf("forward rule target = %q, want %q", rule.TargetAddr, wantTarget)
	}
	forwardPort := rule.ListenPort
	t.Logf("forward UDP rule: 0.0.0.0:%d -> %s", forwardPort, rule.TargetAddr)

	// 10. Fetch the user's AuthToken — hysteria client auth uses that as the
	//     password, since UserManager publishes AuthToken in ListUsersWithPasswd.
	user, err := um.GetUser(username)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	// 11. Start a real hysteria client process pointing at the forward port,
	//     exposing a SOCKS5 proxy locally.
	clientSocksPort := freeTCPPort_(t)
	clientCfgPath := filepath.Join(tmpDir, "hysteria-client.yaml")
	clientCfg := fmt.Sprintf(`server: 127.0.0.1:%d
auth: %s
tls:
  sni: localhost
  insecure: true
socks5:
  listen: 127.0.0.1:%d
`, forwardPort, user.AuthToken, clientSocksPort)
	if err := os.WriteFile(clientCfgPath, []byte(clientCfg), 0600); err != nil {
		t.Fatalf("write client config: %v", err)
	}

	clientCmd := exec.Command(hysteriaBin, "client", "-c", clientCfgPath)
	clientCmd.Stdout = os.Stdout
	clientCmd.Stderr = os.Stderr
	if err := clientCmd.Start(); err != nil {
		t.Fatalf("start hysteria client: %v", err)
	}
	defer func() {
		if clientCmd.Process != nil {
			_ = clientCmd.Process.Kill()
			_, _ = clientCmd.Process.Wait()
		}
	}()

	socksAddr := fmt.Sprintf("127.0.0.1:%d", clientSocksPort)
	if err := waitPort(socksAddr, 15*time.Second); err != nil {
		t.Fatalf("client socks5 not ready: %v", err)
	}
	t.Logf("hysteria client socks5 up on %s", socksAddr)

	// 12. Positive path: curl-style GET through the full chain.
	httpClient, err := httpClientViaSocks5(socksAddr)
	if err != nil {
		t.Fatalf("build socks5 http client: %v", err)
	}
	// The first hysteria QUIC handshake can race the mock auth endpoint; retry
	// for a few seconds before declaring failure.
	var lastErr error
	var lastStatus int
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := httpClient.Get("https://www.google.com/")
		if err != nil {
			lastErr = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		lastStatus = resp.StatusCode
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			lastErr = nil
			break
		}
		lastErr = fmt.Errorf("status %d", resp.StatusCode)
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("request via full chain failed: %v (last status %d)", lastErr, lastStatus)
	}
	t.Logf("positive: curl -> socks5 -> forward -> hysteria -> internet: HTTP %d", lastStatus)

	// Snapshot traffic counter: non-zero upload/download proves bytes really
	// traversed the UDPRelay (rather than, say, the client falling back to
	// direct connection).
	snap, err := fm.GetTraffic(ruleKey, false)
	if err != nil {
		t.Fatalf("get traffic: %v", err)
	}
	if snap.Upload == 0 || snap.Download == 0 {
		t.Fatalf("UDPRelay traffic counter shows up=%d down=%d — expected both >0", snap.Upload, snap.Download)
	}
	t.Logf("UDPRelay counters: up=%d down=%d active=%d", snap.Upload, snap.Download, snap.ActiveConnections)

	// 13. Negative path: stop the container (kills the hysteria process),
	//     then reissue the same curl. It MUST fail — otherwise the previous
	//     success wasn't really going through the hysteria server.
	if err := hc.Stop(); err != nil {
		t.Fatalf("stop container: %v", err)
	}
	t.Logf("container stopped; issuing follow-up request (must fail)")

	// Use a freshly constructed client with a short timeout so we don't wait
	// out the per-request 15s default.
	failClient, err := httpClientViaSocks5(socksAddr)
	if err != nil {
		t.Fatalf("build failover client: %v", err)
	}
	failClient.Timeout = 6 * time.Second

	resp, err := failClient.Get("https://www.bing.com/")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("expected failure after container stop, got HTTP %d", resp.StatusCode)
	}
	t.Logf("negative: correctly failed after container stop: %v", err)
}

// freeTCPPort_ wraps the existing freeTCPPort helper that returns (int, error)
// and fails the test on error — keeps the test body readable.
func freeTCPPort_(t *testing.T) int {
	t.Helper()
	p, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc TCP port: %v", err)
	}
	return p
}
