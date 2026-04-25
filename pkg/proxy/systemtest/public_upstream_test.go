//go:build integration

package systemtest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Plan T-M-P0-05 (docs/cosmic-leaping-moon.md Phase 0).
//
// This file hosts the systemtest helpers shared by every mihomo protocol
// matrix test:
//
//   - canReachPublicUpstream — probe a real public HTTPS endpoint (default
//     https://www.google.com/generate_204) to decide whether the curl →
//     mihomo client → mihomo server → internet chain can actually return
//     a signal. If not, matrix tests skip with a clear message rather
//     than pass silently on a local httptest fallback.
//
//   - spawnMihomoClient — spin up a separate mihomo process as a client,
//     with a single outbound proxy and mixed-port inbound for tests to
//     pipe curl (or an http.Client) through. Outbound config is supplied
//     by the caller as a clash-style proxy map so the helper stays
//     protocol-agnostic; per-protocol tests (Phase 1-7) construct the
//     right proxy map and hand it in.
//
// Philosophy: this is the **only** place that knows how to run a mihomo
// client in tests. Per-protocol matrix files build the proxy map from
// the FastAdd outcome (subscription URI or direct field extraction) and
// call into spawnMihomoClient + httpClientViaSocks5. That keeps the
// matrix files compact and the infrastructure in one auditable spot.

// defaultPublicUpstream is the canary endpoint for canReachPublicUpstream.
// generate_204 is cheap (empty body), HTTPS-only (exercises TLS / ALPN
// through the proxy), and returns a deterministic 204 status.
const defaultPublicUpstream = "https://www.google.com/generate_204"

// fallbackUpstreams are tried only when the caller did not override
// SYSTEST_UPSTREAM_URL and defaultPublicUpstream is unreachable. Covers
// the case where google.com is RST-flooded but the rest of the internet
// is up. Any override via env var takes precedence and disables fallback
// — explicit caller choice is never second-guessed.
var fallbackUpstreams = []string{
	"https://www.cloudflare.com/cdn-cgi/trace",
	"https://www.apple.com/library/test/success.html",
}

var (
	upstreamOnce   sync.Once
	upstreamChosen string
	upstreamOK     bool
)

// canReachPublicUpstream returns the URL to exercise and whether it's
// reachable. On unreachable, the second return is false and tests should
// t.Skip. Result is cached process-wide in sync.Once so the matrix runs
// don't pay the probe cost per subtest.
func canReachPublicUpstream(t *testing.T) (string, bool) {
	t.Helper()
	upstreamOnce.Do(func() {
		env := os.Getenv("SYSTEST_UPSTREAM_URL")
		if env != "" {
			upstreamChosen = env
			upstreamOK = probeURL(env)
			return
		}
		if probeURL(defaultPublicUpstream) {
			upstreamChosen = defaultPublicUpstream
			upstreamOK = true
			return
		}
		for _, u := range fallbackUpstreams {
			if probeURL(u) {
				upstreamChosen = u
				upstreamOK = true
				return
			}
		}
		// None reached — remember the default URL for the skip message so
		// CI operators know what to override.
		upstreamChosen = defaultPublicUpstream
		upstreamOK = false
	})
	if !upstreamOK {
		t.Logf("canReachPublicUpstream: no public upstream reachable (tried %s + %d fallbacks); set SYSTEST_UPSTREAM_URL to override",
			defaultPublicUpstream, len(fallbackUpstreams))
	}
	return upstreamChosen, upstreamOK
}

// probeURL returns true if the URL responds with a 2xx/3xx status. Used
// only for the reachability probe — real test traffic uses http.Client
// through the mihomo SOCKS5 and asserts the response.
//
// Retries 3x with 5 s per-attempt timeout. The sync.Once-cached result
// gates an entire matrix run, so a single transient blip (SYN drop, DNS
// jitter) during process start would otherwise skip everything.
func probeURL(u string) bool {
	for i := 0; i < 3; i++ {
		if probeURLOnce(u) {
			return true
		}
	}
	return false
}

func probeURLOnce(u string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

// mihomoClientConfig captures the client process's runtime knobs.
type mihomoClientConfig struct {
	// BinaryPath overrides the default ensureMihomoBin resolution. When
	// empty, tests that want to reuse the server binary should pass the
	// server's binary path; standalone clients can set MIHOMO_CLIENT_BIN.
	BinaryPath string
	// DataDir is mihomo's -d argument. One per client is recommended.
	DataDir string
	// Proxy is the clash-style single proxy entry this client routes
	// through. The helper wraps it with `mode: global` so every request
	// goes through this one outbound.
	Proxy map[string]any
	// ExtraProxies lets a caller attach additional outbounds (rare; used
	// for tests that chain through multiple mihomo hops).
	ExtraProxies []map[string]any
}

// spawnMihomoClient boots a mihomo process configured as a client. Returns
// the local mixed-port (SOCKS5+HTTP) and registers cleanup. Caller is
// expected to use the mixed-port via httpClientViaSocks5 or os/exec
// curl -x socks5://127.0.0.1:<port>.
//
// Readiness: returns once the mixed-port listens. Does NOT probe the
// outbound connectivity — that's the test's job.
func spawnMihomoClient(t *testing.T, tmpDir string, cfg mihomoClientConfig) (mixedPort int) {
	t.Helper()

	bin := cfg.BinaryPath
	if bin == "" {
		if env := os.Getenv("MIHOMO_CLIENT_BIN"); env != "" {
			bin = env
		} else {
			bin = ensureMihomoBin(t, tmpDir)
		}
	}

	dataDir := cfg.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(tmpDir, "mihomo-client-data")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir client dataDir: %v", err)
	}

	port, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort for mihomo client mixed-port: %v", err)
	}
	apiPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort for mihomo client external-controller: %v", err)
	}

	proxyName, _ := cfg.Proxy["name"].(string)
	if proxyName == "" {
		proxyName = "client-outbound"
		cfg.Proxy["name"] = proxyName
	}

	proxies := []map[string]any{cfg.Proxy}
	proxies = append(proxies, cfg.ExtraProxies...)

	// mode: global + single-entry proxy-group routes every packet through
	// proxyName. An explicit proxy-group with this one proxy avoids
	// mihomo's "GLOBAL not routable" error in some Alpha builds.
	configMap := map[string]any{
		"mixed-port":          port,
		"external-controller": fmt.Sprintf("127.0.0.1:%d", apiPort),
		"log-level":           "info",
		"mode":                "global",
		"allow-lan":           false,
		"proxies":             proxies,
		"proxy-groups": []map[string]any{
			{
				"name":    "GLOBAL",
				"type":    "select",
				"proxies": []string{proxyName},
			},
		},
		"rules": []string{"MATCH,GLOBAL"},
	}

	configPath := filepath.Join(tmpDir, fmt.Sprintf("mihomo-client-%d.yaml", port))
	body, err := yaml.Marshal(configMap)
	if err != nil {
		t.Fatalf("marshal client config: %v", err)
	}
	if err := os.WriteFile(configPath, body, 0o644); err != nil {
		t.Fatalf("write client config: %v", err)
	}

	cmd := exec.Command(bin, "-d", dataDir, "-f", configPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mihomo client: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		// Surface client logs only on test failure — noisy otherwise.
		if t.Failed() {
			if out := stdout.String(); out != "" {
				t.Logf("mihomo client stdout:\n%s", truncate(out, 4000))
			}
			if out := stderr.String(); out != "" {
				t.Logf("mihomo client stderr:\n%s", truncate(out, 4000))
			}
		}
	})

	mixedAddr := fmt.Sprintf("127.0.0.1:%d", port)
	if err := waitPort(mixedAddr, 10*time.Second); err != nil {
		t.Fatalf("mihomo client mixed-port never ready: %v\nstderr:\n%s", err, stderr.String())
	}

	return port
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n…[truncated]"
}

// httpGetViaSocks5 is a convenience that builds a SOCKS5-routed HTTP
// client and GETs the URL. Returns status code and a trimmed body prefix
// for diagnostics. Tests typically just check status == expected.
func httpGetViaSocks5(t *testing.T, socksPort int, url string) (status int, bodyPrefix string) {
	t.Helper()
	cli, err := httpClientViaSocks5(fmt.Sprintf("127.0.0.1:%d", socksPort))
	if err != nil {
		t.Fatalf("httpClientViaSocks5: %v", err)
	}
	resp, err := cli.Get(url)
	if err != nil {
		t.Fatalf("GET %s via socks5://127.0.0.1:%d: %v", url, socksPort, err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 200)
	n, _ := io.ReadFull(resp.Body, buf)
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(buf[:n]))
}

// TestPublicUpstreamFrameworkSmoke exercises the framework end-to-end
// against a directly-run mihomo client pointed at a public upstream. No
// v2raymg server in the chain — this is the Phase 0 canary. Protocol
// matrix tests in Phase 1+ layer their server on top.
//
// Skips silently when MIHOMO_BIN/MIHOMO_CLIENT_BIN isn't set AND the
// Alpha fallback fetch isn't viable, so CI without egress doesn't fail.
func TestPublicUpstreamFrameworkSmoke(t *testing.T) {
	upstream, ok := canReachPublicUpstream(t)
	if !ok {
		t.Skipf("no public upstream reachable (%s); set SYSTEST_UPSTREAM_URL to override", upstream)
	}

	// MIHOMO_BIN / MIHOMO_CLIENT_BIN not set and no network for Alpha
	// download will fail the smoke. That's acceptable — the smoke is a
	// developer sanity check, not a regression gate. Matrix tests use
	// the same primitives and will skip identically.
	if os.Getenv("MIHOMO_BIN") == "" && os.Getenv("MIHOMO_CLIENT_BIN") == "" {
		if !probeURL("https://github.com") {
			t.Skip("no MIHOMO_BIN and GitHub unreachable for Alpha download; set MIHOMO_BIN to run")
		}
	}

	tmpDir := t.TempDir()
	// A DIRECT proxy: mihomo's built-in "direct" outbound is the simplest
	// possible way to prove the client plumbing works without needing a
	// v2raymg server. Any protocol-specific matrix replaces this with a
	// concrete outbound proxy map.
	port := spawnMihomoClient(t, tmpDir, mihomoClientConfig{
		Proxy: map[string]any{
			"name": "DIRECT-CANARY",
			"type": "direct",
		},
	})

	status, body := httpGetViaSocks5(t, port, upstream)
	if status < 200 || status >= 400 {
		t.Fatalf("public upstream via DIRECT proxy: status=%d body=%q", status, body)
	}
	t.Logf("smoke: %s returned %d via mihomo DIRECT outbound at 127.0.0.1:%d", upstream, status, port)
}
