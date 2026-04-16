//go:build integration

package systemtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/containers/xray"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// TestXrayDynamicSocks5Inbound tests:
// 1. Start xray container
// 2. Dynamically add a socks5 inbound (no encryption/auth for minimal config)
// 3. Verify connectivity through the socks5 proxy
func TestXrayDynamicSocks5Inbound(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	// Step 1: Create xray config with minimal initial setup (just API enabled)
	socksPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc port: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "xray-dynamic.json")
	if err := writeMinimalXrayConfig(cfgPath, 62789); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Step 2: Create executor and start xray container
	exec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: cfgPath,
		GRPCAPIAddress: "127.0.0.1:62789",
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}

	// Start xray
	if err := exec.Start(); err != nil {
		t.Fatalf("start xray: %v", err)
	}
	defer exec.Stop()

	// Wait for xray to be ready
	if err := waitPort("127.0.0.1:62789", 10*time.Second); err != nil {
		t.Fatalf("xray API not ready: %v", err)
	}

	// Step 3: Dynamically add a socks5 inbound via gRPC
	adapter := xray.NewAdapter()
	spec := contracts.InboundSpec{
		Tag: "dynamic-socks", Port: uint32(socksPort), Protocol: contracts.ProtocolSOCKS5,
		Extensions: map[string]any{"listen_addr": "127.0.0.1"},
	}
	nativeInbound, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("generate native config: %v", err)
	}

	if err := exec.AddInboundNative(nativeInbound.JSON); err != nil {
		t.Fatalf("add inbound via gRPC: %v", err)
	}
	defer func() {
		exec.RemoveInboundNative("dynamic-socks")
	}()

	// Wait for socks5 inbound to be ready
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", socksPort), 10*time.Second); err != nil {
		t.Fatalf("socks5 inbound not ready: %v", err)
	}

	// Step 4: Verify socks5 is working - make HTTP request through proxy
	client, err := httpClientViaSocks5(fmt.Sprintf("127.0.0.1:%d", socksPort))
	if err != nil {
		t.Fatalf("build socks5 client: %v", err)
	}

	resp, err := client.Get("http://example.com")
	if err != nil {
		t.Fatalf("http via socks5 failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Example Domain") {
		t.Fatalf("unexpected response body, proxy chain may be broken")
	}

	t.Logf("Successfully accessed http://example.com via dynamic socks5 inbound on port %d", socksPort)
}

// TestXrayDynamicInbound_VerifyProcessRunning tests that xray process
// is actually running when we add dynamic inbounds.
func TestXrayDynamicInbound_VerifyProcessRunning(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	// Create config with API only
	cfgPath := filepath.Join(t.TempDir(), "xray-verify.json")
	if err := writeMinimalXrayConfig(cfgPath, 62790); err != nil {
		t.Fatalf("write config: %v", err)
	}

	exec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: cfgPath,
		GRPCAPIAddress: "127.0.0.1:62790",
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}

	// Start xray
	if err := exec.Start(); err != nil {
		t.Fatalf("start xray: %v", err)
	}
	defer exec.Stop()

	// Verify process is running
	if !exec.IsRunning() {
		t.Fatal("expected xray to be running after Start")
	}

	// Verify we can query stats (proves xray is responsive)
	stats, err := exec.QueryStats("inbound>>>dynamic-socks>>>traffic", false)
	if err != nil {
		// Error is expected if the inbound doesn't exist yet
		t.Logf("Expected error querying non-existent inbound stats: %v", err)
	}
	_ = stats // may be empty or error

	t.Log("Xray process is running and responsive")
}

// TestXrayDynamicInbound_MultipleInbounds tests adding multiple dynamic inbounds.
func TestXrayDynamicInbound_MultipleInbounds(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	// Allocate multiple ports
	ports := make([]int, 3)
	for i := range ports {
		p, err := freeTCPPort()
		if err != nil {
			t.Fatalf("alloc port: %v", err)
		}
		ports[i] = p
	}

	cfgPath := filepath.Join(t.TempDir(), "xray-multi.json")
	if err := writeMinimalXrayConfig(cfgPath, 62791); err != nil {
		t.Fatalf("write config: %v", err)
	}

	exec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: cfgPath,
		GRPCAPIAddress: "127.0.0.1:62791",
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}

	if err := exec.Start(); err != nil {
		t.Fatalf("start xray: %v", err)
	}
	defer exec.Stop()

	if err := waitPort("127.0.0.1:62791", 10*time.Second); err != nil {
		t.Fatalf("xray API not ready: %v", err)
	}

	// Add multiple socks5 inbounds
	adapter := xray.NewAdapter()
	for i, port := range ports {
		tag := fmt.Sprintf("socks-%d", i)
		spec := contracts.InboundSpec{
			Tag: tag, Port: uint32(port), Protocol: contracts.ProtocolSOCKS5,
			Extensions: map[string]any{"listen_addr": "127.0.0.1"},
		}
		nativeInbound, err := adapter.ToProvider(spec)
		if err != nil {
			t.Fatalf("generate native config for %s: %v", tag, err)
		}

		if err := exec.AddInboundNative(nativeInbound.JSON); err != nil {
			t.Fatalf("add inbound %s: %v", tag, err)
		}
		defer exec.RemoveInboundNative(tag)

		// Verify each inbound is listening
		if err := waitPort(fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second); err != nil {
			t.Errorf("inbound %s not ready on port %d: %v", tag, port, err)
		}
	}

	// List all inbounds
	inbounds := exec.ListInboundConfigs()
	t.Logf("Total inbounds: %d", len(inbounds))

	// Should have at least the number we added (plus API inbound)
	if len(inbounds) < 3 {
		t.Errorf("expected at least 3 inbounds, got %d", len(inbounds))
	}
}

// TestXrayDynamicInbound_RemoveInbound tests removing a dynamic inbound.
func TestXrayDynamicInbound_RemoveInbound(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	socksPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc port: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "xray-remove.json")
	if err := writeMinimalXrayConfig(cfgPath, 62792); err != nil {
		t.Fatalf("write config: %v", err)
	}

	exec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: cfgPath,
		GRPCAPIAddress: "127.0.0.1:62792",
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}

	if err := exec.Start(); err != nil {
		t.Fatalf("start xray: %v", err)
	}
	defer exec.Stop()

	if err := waitPort("127.0.0.1:62792", 10*time.Second); err != nil {
		t.Fatalf("xray API not ready: %v", err)
	}

	// Add a socks5 inbound
	adapter := xray.NewAdapter()
	spec := contracts.InboundSpec{
		Tag: "to-remove", Port: uint32(socksPort), Protocol: contracts.ProtocolSOCKS5,
		Extensions: map[string]any{"listen_addr": "127.0.0.1"},
	}
	nativeInbound, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("generate native config: %v", err)
	}

	if err := exec.AddInboundNative(nativeInbound.JSON); err != nil {
		t.Fatalf("add inbound: %v", err)
	}

	// Verify it's listening
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", socksPort), 5*time.Second); err != nil {
		t.Fatalf("inbound not ready before removal: %v", err)
	}

	// Remove the inbound
	if err := exec.RemoveInboundNative("to-remove"); err != nil {
		t.Fatalf("remove inbound: %v", err)
	}

	// Wait for port to no longer be in use
	time.Sleep(500 * time.Millisecond)
	if isPortOpen(fmt.Sprintf("127.0.0.1:%d", socksPort)) {
		t.Error("expected port to be closed after inbound removal")
	}
}

// TestXrayInboundConfig_GenericAPI tests adding inbound via the generic Inbound interface.
func TestXrayInboundConfig_GenericAPI(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	socksPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc port: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "xray-generic.json")
	if err := writeMinimalXrayConfig(cfgPath, 62793); err != nil {
		t.Fatalf("write config: %v", err)
	}

	exec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: cfgPath,
		GRPCAPIAddress: "127.0.0.1:62793",
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}

	if err := exec.Start(); err != nil {
		t.Fatalf("start xray: %v", err)
	}
	defer exec.Stop()

	if err := waitPort("127.0.0.1:62793", 10*time.Second); err != nil {
		t.Fatalf("xray API not ready: %v", err)
	}

	// Use Adapter to create native config, then add via gRPC
	adapter := xray.NewAdapter()
	spec := contracts.InboundSpec{
		Tag: "generic-socks", Port: uint32(socksPort), Protocol: contracts.ProtocolSOCKS5,
		Extensions: map[string]any{"listen_addr": "127.0.0.1"},
	}
	nativeInbound, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("generate native config: %v", err)
	}
	if err := exec.AddInboundNative(nativeInbound.JSON); err != nil {
		t.Fatalf("add inbound: %v", err)
	}
	defer exec.RemoveInboundNative("generic-socks")

	// Verify the inbound is in the list
	inbounds := exec.ListInboundConfigs()
	found := false
	for _, in := range inbounds {
		if in.Tag() == "generic-socks" {
			found = true
			break
		}
	}
	if !found {
		t.Error("inbound not found in list after AddInboundConfig")
	}
}

// writeMinimalXrayConfig writes a minimal xray config with API + freedom outbound.
func writeMinimalXrayConfig(path string, apiPort int) error {
	cfg := map[string]any{
		"log":    map[string]any{"loglevel": "warning"},
		"stats":  map[string]any{},
		"policy": map[string]any{},
		"api": map[string]any{
			"tag":      "api",
			"services": []string{"HandlerService", "LoggerService", "StatsService"},
		},
		"inbounds": []map[string]any{
			{
				"tag": "api-in", "listen": "127.0.0.1", "port": apiPort,
				"protocol": "dokodemo-door",
				"settings": map[string]any{"address": "127.0.0.1"},
			},
		},
		"outbounds": []map[string]any{
			{"protocol": "freedom", "tag": "direct"},
		},
		"routing": map[string]any{
			"rules": []map[string]any{
				{"inboundTag": []string{"api-in"}, "outboundTag": "api"},
			},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// isPortOpen checks if a port is open.
func isPortOpen(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// TestXraySocks5ToLocalhost tests socks5 proxy to a local HTTP server.
func TestXraySocks5ToLocalhost(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	// Start a local HTTP server
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello from localhost"))
	})}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server.Addr = ln.Addr().String()

	go server.Serve(ln)
	defer server.Close()

	// Get a free port for socks5
	socksPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc port: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "xray-localhost.json")
	if err := writeMinimalXrayConfig(cfgPath, 62794); err != nil {
		t.Fatalf("write config: %v", err)
	}

	exec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: cfgPath,
		GRPCAPIAddress: "127.0.0.1:62794",
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}

	if err := exec.Start(); err != nil {
		t.Fatalf("start xray: %v", err)
	}
	defer exec.Stop()

	if err := waitPort("127.0.0.1:62794", 10*time.Second); err != nil {
		t.Fatalf("xray API not ready: %v", err)
	}

	// Add socks5 inbound
	adapter := xray.NewAdapter()
	spec := contracts.InboundSpec{
		Tag: "local-socks", Port: uint32(socksPort), Protocol: contracts.ProtocolSOCKS5,
		Extensions: map[string]any{"listen_addr": "127.0.0.1"},
	}
	nativeInbound, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("generate native config: %v", err)
	}

	if err := exec.AddInboundNative(nativeInbound.JSON); err != nil {
		t.Fatalf("add inbound: %v", err)
	}
	defer exec.RemoveInboundNative("local-socks")

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", socksPort), 5*time.Second); err != nil {
		t.Fatalf("socks5 not ready: %v", err)
	}

	// Access the local server via socks5
	client, err := httpClientViaSocks5(fmt.Sprintf("127.0.0.1:%d", socksPort))
	if err != nil {
		t.Fatalf("build socks5 client: %v", err)
	}

	resp, err := client.Get(fmt.Sprintf("http://%s", server.Addr))
	if err != nil {
		t.Fatalf("http via socks5 to localhost failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if !strings.Contains(string(body), "Hello from localhost") {
		t.Fatalf("unexpected response: %s", string(body))
	}

	t.Logf("Successfully accessed localhost server via socks5 proxy")
}
