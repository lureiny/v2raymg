//go:build integration

package systemtest

import (
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

// mihomoProtocolCase is one row of the MVP protocol matrix: vmess, trojan,
// shadowsocks (D4). Each case carries the FastAdd params and enough info to
// build an xray client that talks to mihomo's listener via the forward layer.
type mihomoProtocolCase struct {
	Name     string
	Protocol string
	// params is a func so each invocation gets a fresh credential (avoids a
	// stale uuid/password bleeding across retries or parallel cases).
	params func() map[string]any
}

func mihomoMVPCases() []mihomoProtocolCase {
	return []mihomoProtocolCase{
		{
			Name:     "vmess",
			Protocol: "vmess",
			params: func() map[string]any {
				return map[string]any{"uuid": uuid.NewString()}
			},
		},
		{
			Name:     "trojan",
			Protocol: "trojan",
			// P2-9 core case: plain TCP (no TLS/cert) — validates whether
			// mihomo accepts a trojan listener without a certificate AND
			// whether the xray client can handshake against it. If this
			// fails at runtime the MVP scope needs to drop to "trojan only
			// with TLS", which would require writeTempCert + a TLS stream
			// in the client (see the xray FastAdd fixture for the shape).
			params: func() map[string]any {
				return map[string]any{
					"password": "trojan-stage10-" + randHex(8),
				}
			},
		},
		{
			Name:     "shadowsocks",
			Protocol: "shadowsocks",
			params: func() map[string]any {
				// aes-256-gcm: classic AEAD cipher accepted by both mihomo
				// (Alpha 2024+) and xray. This test uses the legacy SharedCred
				// path (pre-Phase-4 record shape); Phase 4 system tests live
				// in mihomo_ss_matrix_test.go.
				return map[string]any{
					"password": "ss-stage10-" + randHex(16),
					"cipher":   "aes-256-gcm",
				}
			},
		},
	}
}

// TestMihomoProtocolMatrix validates vmess / trojan / shadowsocks end-to-end
// against a real mihomo binary, using xray as the client. For each protocol:
//
//  1. FastAddInbound creates a shared-credential listener on 127.0.0.1:port
//  2. AddUser drives the UserEvent pipeline to allocate a forward port
//  3. xray client connects SOCKS5 → <protocol> outbound → forward port →
//     mihomo listener → DIRECT → local httptest origin
//  4. HTTP GET through the tunnel must return the origin's marker
//  5. RemoveUser + subsequent request must fail (confirms the tunnel is
//     real and the port release pipeline works)
//
// Preconditions: XRAY_BIN set (for the client); MIHOMO_BIN set OR network
// available for Updater to pull Alpha.
func TestMihomoProtocolMatrix(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set (needed as protocol client)")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	const marker = "MIHOMO_CHAIN_OK"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(marker))
	}))
	defer origin.Close()
	targetURL := origin.URL
	t.Logf("local origin: %s", targetURL)

	tmpDir := t.TempDir()
	mihomoBin := ensureMihomoBin(t, tmpDir)
	rig := startMihomoContainer(t, tmpDir, mihomoBin)
	t.Logf("mihomo ready (api=%s)", rig.apiAddr)

	for _, tc := range mihomoMVPCases() {
		t.Run(tc.Name, func(t *testing.T) {
			runMihomoProtocolCase(t, rig, tc, xrayBin, tmpDir, targetURL, marker)
		})
	}
}

func runMihomoProtocolCase(
	t *testing.T,
	rig *mihomoTestRig,
	tc mihomoProtocolCase,
	xrayBin, tmpDir, targetURL, marker string,
) {
	t.Helper()

	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc inbound port: %v", err)
	}
	socksPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc socks port: %v", err)
	}

	// FastAdd params — protocol + port + protocol-specific credentials.
	tag := fmt.Sprintf("mihomo-%s-%d", tc.Name, inboundPort)
	params := tc.params()
	params["protocol"] = tc.Protocol
	params["port"] = inboundPort

	t.Logf("FastAddInbound %s on 127.0.0.1:%d", tc.Name, inboundPort)
	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 10*time.Second); err != nil {
		t.Fatalf("mihomo listener %d not ready: %v", inboundPort, err)
	}

	// Drive the full UserEvent pipeline — mirrors production. AddUser emits
	// UserEventAdd; the container goroutine observes it and calls
	// GetBindPort, which allocates a forward port pointing at inboundPort.
	username := fmt.Sprintf("alice-%s-%d", tc.Name, inboundPort)
	forwardPort := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))
	t.Logf("user %q allocated forward port %d -> 127.0.0.1:%d", username, forwardPort, inboundPort)

	// xray client config: SOCKS5 inbound, protocol outbound pointed at the
	// forward port. No freedom outbound — a broken tunnel must fail
	// connectivity instead of silently going direct (same design choice as
	// the xray FastAdd fixture).
	clientCfg := buildMihomoXrayClient(tc, int(forwardPort), socksPort, params)
	clientCfgPath := filepath.Join(tmpDir, fmt.Sprintf("client-%s.json", tc.Name))
	if err := writeJSONConfig(clientCfgPath, clientCfg); err != nil {
		t.Fatalf("write client config: %v", err)
	}

	cmd := exec.Command(xrayBin, "run", "-c", clientCfgPath)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start xray client: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
	if err := waitPort(socksAddr, 10*time.Second); err != nil {
		t.Fatalf("xray client socks port %d not ready: %v", socksPort, err)
	}

	// Positive path: must receive the marker via the proxy chain.
	httpClient, err := httpClientViaSocks5(socksAddr)
	if err != nil {
		t.Fatalf("httpClientViaSocks5: %v", err)
	}
	resp, err := httpClient.Get(targetURL)
	if err != nil {
		t.Fatalf("proxied GET %s: %v (chain broken?)", targetURL, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), marker) {
		t.Fatalf("proxied response missing marker %q: got %q", marker, body)
	}
	t.Logf("PASS: marker received through %s proxy chain", tc.Name)

	// Negative path: after RemoveUser, the forward rule is gone and the
	// client request must fail. Proves the release pipeline works AND the
	// positive result wasn't a false positive via some side channel.
	removeMihomoUserAndWaitForPortRelease(t, rig, username, tag)

	// Short per-connection timeout — if the rule really is released, the
	// first SYN hits a closed port and fails fast; we don't want to block
	// the whole connect timeout on every negative.
	negResp, negErr := httpClient.Get(targetURL)
	if negErr == nil {
		negResp.Body.Close()
		t.Fatalf("request unexpectedly succeeded after RemoveUser (status %d)", negResp.StatusCode)
	}
	t.Logf("PASS: request fails after RemoveUser (%v)", negErr)
}

// buildMihomoXrayClient is a mihomo-flavored sibling of xray_fastadd_
// connectivity_test.go::buildClientConfig. It keeps the SOCKS5-in →
// <protocol>-out shape but strips the TLS/Reality/transport branches that
// don't apply here: mihomo's vmess/trojan/ss listeners in MVP are all plain
// TCP without extra stream settings. Keeping this divergent from the xray
// helper lets us drop the xray fixture's Reality / ws / grpc complexity.
func buildMihomoXrayClient(tc mihomoProtocolCase, serverPort, socksPort int, params map[string]any) map[string]any {
	outbound := map[string]any{
		"tag":      "proxy",
		"protocol": tc.Protocol,
	}

	switch tc.Protocol {
	case "vmess":
		uuid, _ := params["uuid"].(string)
		outbound["settings"] = map[string]any{
			"vnext": []map[string]any{{
				"address": "127.0.0.1", "port": serverPort,
				"users": []map[string]any{{
					"id":       uuid,
					"security": "auto",
					"alterId":  0,
				}},
			}},
		}

	case "trojan":
		pw, _ := params["password"].(string)
		outbound["settings"] = map[string]any{
			"servers": []map[string]any{{
				"address": "127.0.0.1", "port": serverPort, "password": pw,
			}},
		}
		// xray's trojan outbound normally expects TLS. P2-9 is exactly the
		// question of whether xray will speak plain TCP when streamSettings
		// is absent — some xray versions force security=tls. If the
		// integration test fails here, fall back to TLS in both the mihomo
		// listener (cert/key via params) and this outbound (streamSettings:
		// security=tls, allowInsecure=true).

	case "shadowsocks":
		method, _ := params["cipher"].(string)
		pw, _ := params["password"].(string)
		outbound["settings"] = map[string]any{
			"servers": []map[string]any{{
				"address": "127.0.0.1", "port": serverPort,
				"method": method, "password": pw,
			}},
		}
	}

	return map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []map[string]any{{
			"tag":      "socks-in",
			"protocol": "socks",
			"listen":   "127.0.0.1",
			"port":     socksPort,
			"settings": map[string]any{"udp": false},
		}},
		"outbounds": []map[string]any{outbound},
	}
}

// randHex returns 2n hex characters sourced from google/uuid's crypto/rand
// backend. Cap is 32 (one UUID = 16 bytes = 32 hex chars); MVP call sites
// pass n<=16, so one UUID per call is enough.
func randHex(n int) string {
	hex := strings.ReplaceAll(uuid.NewString(), "-", "")
	if n*2 < len(hex) {
		return hex[:n*2]
	}
	return hex
}
