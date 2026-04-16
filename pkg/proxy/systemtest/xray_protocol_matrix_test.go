//go:build ignore
// FIXME: uses stale contracts.UserSpec / InboundSpec fields (Email, Level, Expiry, Users, Extensions).
// These were already broken before this change. Coverage superseded by xray_fastadd_connectivity_test.go.
// To re-enable: update to current contracts.UserSpec + Adapter.ToProvider pattern.

package systemtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/containers/xray"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"golang.org/x/net/proxy"
)

// ProtocolTestCase defines a test case for a single protocol.
type ProtocolTestCase struct {
	Protocol       contracts.Protocol
	Tag            string
	HasUsers       bool
	SupportsDirect bool // Whether we can test full connectivity
}

// TestXrayDynamicInbound_ProtocolMatrix tests dynamic addition of multiple protocols.
// This test verifies:
// 1. AddInbound via gRPC for each protocol
// 2. Port listening after inbound addition
// 3. Full proxy chain connectivity (for protocols that support it)
func TestXrayDynamicInbound_ProtocolMatrix(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	// Define test protocols
	protocols := []ProtocolTestCase{
		{Protocol: contracts.ProtocolSOCKS5, Tag: "test-socks5", HasUsers: false, SupportsDirect: true},
		{Protocol: contracts.ProtocolHTTP, Tag: "test-http", HasUsers: true, SupportsDirect: true},
		{Protocol: contracts.ProtocolVMess, Tag: "test-vmess", HasUsers: true, SupportsDirect: false},
		{Protocol: contracts.ProtocolVLess, Tag: "test-vless", HasUsers: true, SupportsDirect: false},
		{Protocol: contracts.ProtocolTrojan, Tag: "test-trojan", HasUsers: true, SupportsDirect: false},
		{Protocol: contracts.ProtocolShadowsocks, Tag: "test-ss", HasUsers: true, SupportsDirect: false},
	}

	// Allocate ports for each protocol
	ports := make([]int, len(protocols))
	for i := range protocols {
		p, err := freeTCPPort()
		if err != nil {
			t.Fatalf("alloc port for %s: %v", protocols[i].Protocol, err)
		}
		ports[i] = p
	}

	// Create xray config with API enabled
	apiPort := 62800
	cfgPath := filepath.Join(t.TempDir(), "xray-protocol-matrix.json")
	if err := writeMinimalXrayConfig(cfgPath, apiPort); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Create executor
	exec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: cfgPath,
		GRPCAPIAddress: fmt.Sprintf("127.0.0.1:%d", apiPort),
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}

	// Start xray
	if err := exec.Start(); err != nil {
		t.Fatalf("start xray: %v", err)
	}
	defer exec.Stop()

	// Wait for API to be ready
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", apiPort), 15*time.Second); err != nil {
		t.Fatalf("xray API not ready: %v", err)
	}

	// Create adapter for converting InboundSpec to native config
	adapter := xray.NewAdapter()

	// Test each protocol
	results := make(map[string]struct {
		AddSuccess bool
		ListenOK   bool
		ConnectOK  bool
		Error      string
	})

	for i, tc := range protocols {
		port := ports[i]
		tag := tc.Tag

		// Create InboundSpec
		spec := contracts.InboundSpec{
			Tag:      tag,
			Port:     uint32(port),
			Protocol: tc.Protocol,
		}

		// For protocols that need users, add a test user
		if tc.HasUsers {
			spec.Users = []contracts.UserSpec{
				{
					Email:  fmt.Sprintf("test@%s.local", tag),
					Level:  0,
					Expiry: "",
					Extensions: map[string]any{
						"uuid": "a1b2c3d4-a1b2-a1b2-a1b2-a1b2c3d4e5f6",
					},
				},
			}
			// Protocol-specific extensions
			switch tc.Protocol {
			case contracts.ProtocolVMess:
				spec.Users[0].Extensions["alter_id"] = uint32(0)
			case contracts.ProtocolVLess:
				spec.Users[0].Extensions["flow"] = "xtls-rprx-vision"
			case contracts.ProtocolTrojan:
				// Trojan uses password which is the same as UUID in our test
			case contracts.ProtocolShadowsocks:
				spec.Users[0].Password = "test-password"
				spec.Users[0].Extensions["method"] = "aes-256-gcm"
			case contracts.ProtocolHTTP:
				spec.Users[0].Extensions["user"] = "testuser"
				spec.Users[0].Extensions["pass"] = "testpass"
			}
		}

		// Convert to native Xray config using adapter
		nativeInbound, err := adapter.ToProvider(spec)
		if err != nil {
			results[tc.Protocol.String()] = struct {
				AddSuccess bool
				ListenOK   bool
				ConnectOK  bool
				Error      string
			}{AddSuccess: false, Error: fmt.Sprintf("ToProvider failed: %v", err)}
			t.Logf("SKIP %s: ToProvider failed: %v", tc.Protocol, err)
			continue
		}

		// Add inbound via gRPC
		if err := exec.AddInboundNative(nativeInbound.JSON); err != nil {
			results[tc.Protocol.String()] = struct {
				AddSuccess bool
				ListenOK   bool
				ConnectOK  bool
				Error      string
			}{AddSuccess: false, Error: fmt.Sprintf("AddInbound failed: %v", err)}
			t.Logf("FAIL %s: AddInbound failed: %v", tc.Protocol, err)
			continue
		}
		defer exec.RemoveInboundNative(tag)

		// Wait for port to be listening
		listenErr := waitPort(fmt.Sprintf("127.0.0.1:%d", port), 10*time.Second)
		listenOK := listenErr == nil

		connectOK := false
		if listenOK && tc.SupportsDirect {
			// Test full connectivity for protocols that support it
			switch tc.Protocol {
			case contracts.ProtocolSOCKS5:
				connectOK = testSocks5Connectivity(fmt.Sprintf("127.0.0.1:%d", port), t)
			case contracts.ProtocolHTTP:
				connectOK = testHTTPConnectivity(fmt.Sprintf("127.0.0.1:%d", port), t)
			}
		}

		results[tc.Protocol.String()] = struct {
			AddSuccess bool
			ListenOK   bool
			ConnectOK  bool
			Error      string
		}{
			AddSuccess: true,
			ListenOK:   listenOK,
			ConnectOK:  connectOK,
		}

		// Log result
		status := "PASS"
		if !listenOK {
			status = "FAIL"
		} else if tc.SupportsDirect && !connectOK {
			status = "WARN"
		}
		t.Logf("%s %s: Add=%v Listen=%v Connect=%v", status, tc.Protocol, true, listenOK, connectOK)
	}

	// Print summary
	t.Log("\n=== Protocol Matrix Summary ===")
	allPassed := true
	for proto, r := range results {
		if !r.AddSuccess || !r.ListenOK {
			allPassed = false
			t.Logf("  %s: FAILED - %s", proto, r.Error)
		} else if r.ConnectOK {
			t.Logf("  %s: PASS (full connectivity)", proto)
		} else {
			t.Logf("  %s: PASS (add + listen only)", proto)
		}
	}

	if !allPassed {
		t.Fatal("Some protocols failed to add or listen")
	}
}

// testSocks5Connectivity tests socks5 proxy connectivity.
func testSocks5Connectivity(addr string, t *testing.T) bool {
	client, err := httpClientViaSocks5(addr)
	if err != nil {
		t.Logf("SOCKS5 client build failed: %v", err)
		return false
	}

	resp, err := client.Get("http://example.com")
	if err != nil {
		t.Logf("SOCKS5 connection failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		t.Logf("SOCKS5 unexpected status: %d", resp.StatusCode)
		return false
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Example Domain") {
		t.Logf("SOCKS5 unexpected response")
		return false
	}

	return true
}

// testHTTPConnectivity tests HTTP proxy connectivity.
func testHTTPConnectivity(addr string, t *testing.T) bool {
	// HTTP proxy uses CONNECT method
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: func(req *http.Request) (*url.URL, error) {
				return url.Parse("http://" + addr)
			},
		},
		Timeout: 15 * time.Second,
	}

	resp, err := client.Get("http://example.com")
	if err != nil {
		t.Logf("HTTP proxy connection failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		t.Logf("HTTP proxy unexpected status: %d", resp.StatusCode)
		return false
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Example Domain") {
		t.Logf("HTTP proxy unexpected response")
		return false
	}

	return true
}

// TestXrayDynamicInbound_WithUser tests adding user to dynamic inbound.
func TestXrayDynamicInbound_WithUser(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	// Allocate port
	vmessPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc port: %v", err)
	}

	// Create xray config
	apiPort := 62801
	cfgPath := filepath.Join(t.TempDir(), "xray-with-user.json")
	if err := writeMinimalXrayConfig(cfgPath, apiPort); err != nil {
		t.Fatalf("write config: %v", err)
	}

	exec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: cfgPath,
		GRPCAPIAddress: fmt.Sprintf("127.0.0.1:%d", apiPort),
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}

	if err := exec.Start(); err != nil {
		t.Fatalf("start xray: %v", err)
	}
	defer exec.Stop()

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", apiPort), 15*time.Second); err != nil {
		t.Fatalf("xray API not ready: %v", err)
	}

	// Create VMess inbound with empty users
	adapter := xray.NewAdapter()
	spec := contracts.InboundSpec{
		Tag:      "vmess-with-user",
		Port:     uint32(vmessPort),
		Protocol: contracts.ProtocolVMess,
	}

	nativeInbound, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("ToProvider failed: %v", err)
	}

	// Add inbound
	if err := exec.AddInboundNative(nativeInbound.JSON); err != nil {
		t.Fatalf("AddInbound failed: %v", err)
	}
	defer exec.RemoveInboundNative("vmess-with-user")

	// Wait for port
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", vmessPort), 10*time.Second); err != nil {
		t.Fatalf("VMess inbound not ready: %v", err)
	}

	// Add user via gRPC
	testUser := contracts.UserSpec{
		Email:    "test@example.com",
		Level:    0,
		Protocol: contracts.ProtocolVMess,
		Extensions: map[string]any{
			"uuid":     "a1b2c3d4-a1b2-a1b2-a1b2-a1b2c3d4e5f6",
			"alter_id": uint32(0),
		},
	}

	if err := exec.AddUser("vmess-with-user", testUser); err != nil {
		t.Fatalf("AddUser failed: %v", err)
	}

	t.Logf("Successfully added VMess inbound with user on port %d", vmessPort)
}

// TestXrayDynamicInbound_Concurrent adds multiple inbounds concurrently.
func TestXrayDynamicInbound_Concurrent(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	// Create xray config
	apiPort := 62802
	cfgPath := filepath.Join(t.TempDir(), "xray-concurrent.json")
	if err := writeMinimalXrayConfig(cfgPath, apiPort); err != nil {
		t.Fatalf("write config: %v", err)
	}

	exec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: cfgPath,
		GRPCAPIAddress: fmt.Sprintf("127.0.0.1:%d", apiPort),
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}

	if err := exec.Start(); err != nil {
		t.Fatalf("start xray: %v", err)
	}
	defer exec.Stop()

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", apiPort), 15*time.Second); err != nil {
		t.Fatalf("xray API not ready: %v", err)
	}

	// Concurrently add socks5 inbounds
	adapter := xray.NewAdapter()
	numInbounds := 5
	ports := make([]int, numInbounds)
	for i := 0; i < numInbounds; i++ {
		p, err := freeTCPPort()
		if err != nil {
			t.Fatalf("alloc port: %v", err)
		}
		ports[i] = p
	}

	// Add inbounds concurrently
	errCh := make(chan error, numInbounds)
	for i := 0; i < numInbounds; i++ {
		go func(idx int) {
			tag := fmt.Sprintf("concurrent-socks-%d", idx)
			spec := contracts.InboundSpec{
				Tag:      tag,
				Port:     uint32(ports[idx]),
				Protocol: contracts.ProtocolSOCKS5,
			}

			nativeInbound, err := adapter.ToProvider(spec)
			if err != nil {
				errCh <- fmt.Errorf("ToProvider failed for %s: %v", tag, err)
				return
			}

			if err := exec.AddInboundNative(nativeInbound.JSON); err != nil {
				errCh <- fmt.Errorf("AddInbound failed for %s: %v", tag, err)
				return
			}

			errCh <- nil
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < numInbounds; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("Concurrent add failed: %v", err)
		}
	}

	// Verify all ports are listening
	for i := 0; i < numInbounds; i++ {
		tag := fmt.Sprintf("concurrent-socks-%d", i)
		port := ports[i]
		if err := waitPort(fmt.Sprintf("127.0.0.1:%d", port), 10*time.Second); err != nil {
			t.Errorf("Port %d not listening for %s: %v", port, tag, err)
		}
		defer exec.RemoveInboundNative(tag)
	}

	// List all inbounds
	inbounds := exec.ListInboundConfigs()
	t.Logf("Total inbounds after concurrent add: %d", len(inbounds))

	if len(inbounds) < numInbounds {
		t.Errorf("Expected at least %d inbounds, got %d", numInbounds, len(inbounds))
	}
}

// TestXrayInbound_AddRemoveCycle tests adding and removing inbounds in cycle.
func TestXrayInbound_AddRemoveCycle(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	apiPort := 62803
	cfgPath := filepath.Join(t.TempDir(), "xray-cycle.json")
	if err := writeMinimalXrayConfig(cfgPath, apiPort); err != nil {
		t.Fatalf("write config: %v", err)
	}

	exec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: cfgPath,
		GRPCAPIAddress: fmt.Sprintf("127.0.0.1:%d", apiPort),
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}

	if err := exec.Start(); err != nil {
		t.Fatalf("start xray: %v", err)
	}
	defer exec.Stop()

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", apiPort), 15*time.Second); err != nil {
		t.Fatalf("xray API not ready: %v", err)
	}

	adapter := xray.NewAdapter()

	// Perform add/remove cycle
	for cycle := 0; cycle < 3; cycle++ {
		port, err := freeTCPPort()
		if err != nil {
			t.Fatalf("alloc port: %v", err)
		}

		tag := fmt.Sprintf("cycle-socks-%d", cycle)
		spec := contracts.InboundSpec{
			Tag:      tag,
			Port:     uint32(port),
			Protocol: contracts.ProtocolSOCKS5,
		}

		nativeInbound, err := adapter.ToProvider(spec)
		if err != nil {
			t.Fatalf("ToProvider failed: %v", err)
		}

		// Add
		if err := exec.AddInboundNative(nativeInbound.JSON); err != nil {
			t.Fatalf("AddInbound failed on cycle %d: %v", cycle, err)
		}

		// Verify listening
		if err := waitPort(fmt.Sprintf("127.0.0.1:%d", port), 5*time.Second); err != nil {
			t.Errorf("Port not listening on cycle %d: %v", cycle, err)
		}

		// Remove
		if err := exec.RemoveInboundNative(tag); err != nil {
			t.Fatalf("RemoveInbound failed on cycle %d: %v", cycle, err)
		}

		// Verify port closed
		time.Sleep(500 * time.Millisecond)
		if isPortOpen(fmt.Sprintf("127.0.0.1:%d", port)) {
			t.Errorf("Port still open after removal on cycle %d", cycle)
		}

		t.Logf("Completed cycle %d", cycle)
	}
}
