//go:build ignore

// FIXME: this file uses stale UserSpec fields (Email, Protocol, Extensions, Level, Password)
// that no longer exist in contracts.UserSpec after the domain model refactor.
// Its coverage is now superseded by xray_fastadd_connectivity_test.go (14 protocol combos).
// To re-enable: update createE2EUser/createE2EClientConfig to use current UserSpec + Adapter.

package systemtest

import (
	"context"
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
	"golang.org/x/net/proxy"
)

// TestXrayE2EProtocolMatrix performs end-to-end protocol connectivity tests.
// It uses two xray instances: one as server (with dynamic inbound) and one as client.
func TestXrayE2EProtocolMatrix(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}
	if _, err := os.Stat(xrayBin); err != nil {
		t.Skipf("integration test skipped: XRAY_BIN invalid: %v", err)
	}

	// Run tests for each protocol
	protocols := []struct {
		Proto  contracts.Protocol
		UseTLS bool
		Flow   string // for VLESS
		Method string // for Shadowsocks
	}{
		{Proto: contracts.ProtocolVMess, UseTLS: false},
		{Proto: contracts.ProtocolVLess, UseTLS: false, Flow: "xtls-rprx-vision"},
		{Proto: contracts.ProtocolTrojan, UseTLS: false},
		{Proto: contracts.ProtocolShadowsocks, UseTLS: false, Method: "aes-256-gcm"},
	}

	for _, tc := range protocols {
		t.Run(string(tc.Proto), func(t *testing.T) {
			testE2EProtocol(t, xrayBin, tc.Proto, tc.UseTLS, tc.Flow, tc.Method)
		})
	}
}

// testE2EProtocol tests end-to-end connectivity for a single protocol.
func testE2EProtocol(t *testing.T, xrayBin string, proto contracts.Protocol, useTLS bool, flow, method string) {
	// Allocate ports
	serverAPIPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc API port: %v", err)
	}
	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc inbound port: %v", err)
	}
	clientSocksPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("alloc client socks port: %v", err)
	}

	// Create server config
	serverCfgPath := filepath.Join(t.TempDir(), "server.json")
	serverCfg := map[string]any{
		"log":      map[string]any{"loglevel": "debug"},
		"stats":    map[string]any{},
		"policy":   map[string]any{},
		"inbounds": []map[string]any{},
		"outbounds": []map[string]any{
			{"protocol": "freedom", "tag": "direct"},
		},
	}
	serverCfg["api"] = map[string]any{
		"tag":      "api",
		"services": []string{"HandlerService", "LoggerService", "StatsService"},
		"port":     fmt.Sprintf("%d", serverAPIPort),
		"listen":   "127.0.0.1",
		"protocol": "dokodemo-door",
		"settings": map[string]any{"address": "127.0.0.1"},
	}

	if err := writeJSONFile(serverCfgPath, serverCfg); err != nil {
		t.Fatalf("write server config: %v", err)
	}

	// Start server xray
	serverExec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: serverCfgPath,
		GRPCAPIAddress: fmt.Sprintf("127.0.0.1:%d", serverAPIPort),
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := serverExec.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	defer serverExec.Stop()

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", serverAPIPort), 15*time.Second); err != nil {
		t.Fatalf("server API not ready: %v", err)
	}
	t.Logf("Server API ready on port %d", serverAPIPort)

	// Create user
	tag := fmt.Sprintf("e2e-%s", proto)
	user := createE2EUser(proto, tag, flow, method)

	// Add inbound via gRPC
	adapter := xray.NewAdapter()
	spec := contracts.InboundSpec{
		Tag:      tag,
		Port:     uint32(inboundPort),
		Protocol: proto,
		Users:    []contracts.UserSpec{user},
	}

	nativeInbound, err := adapter.ToProvider(spec)
	if err != nil {
		t.Fatalf("ToProvider failed: %v", err)
	}

	if err := serverExec.AddInboundNative(nativeInbound.JSON); err != nil {
		t.Fatalf("AddInbound failed: %v", err)
	}
	defer serverExec.RemoveInboundNative(tag)
	t.Logf("Added %s inbound on port %d", proto, inboundPort)

	// Wait for inbound to be ready
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 10*time.Second); err != nil {
		t.Fatalf("inbound not ready: %v", err)
	}
	t.Logf("Inbound listening on port %d", inboundPort)

	// Create client config
	clientCfgPath := filepath.Join(t.TempDir(), "client.json")
	clientCfg := createE2EClientConfig(proto, user, inboundPort, clientSocksPort)

	if err := writeJSONFile(clientCfgPath, clientCfg); err != nil {
		t.Fatalf("write client config: %v", err)
	}

	// Start client xray
	clientExec, err := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: clientCfgPath,
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := clientExec.Start(); err != nil {
		t.Fatalf("start client: %v", err)
	}
	defer clientExec.Stop()

	// Wait for client socks to be ready
	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", clientSocksPort), 10*time.Second); err != nil {
		t.Fatalf("client socks not ready: %v", err)
	}
	t.Logf("Client socks ready on port %d", clientSocksPort)

	// Test connectivity
	if testSocks5Connectivity(fmt.Sprintf("127.0.0.1:%d", clientSocksPort), t) {
		t.Logf("PASS: %s end-to-end connectivity verified", proto)
	} else {
		t.Errorf("FAIL: %s end-to-end connectivity failed", proto)
	}
}

// createE2EUser creates a user for end-to-end testing.
func createE2EUser(proto contracts.Protocol, tag, flow, method string) contracts.UserSpec {
	user := contracts.UserSpec{
		Email: fmt.Sprintf("test@%s.local", tag),
		Level: 0,
	}

	switch proto {
	case contracts.ProtocolVMess:
		user.Protocol = contracts.ProtocolVMess
		user.Extensions = map[string]any{
			"uuid":     "a1b2c3d4-a1b2-a1b2-a1b2-a1b2c3d4e5f6",
			"alter_id": uint32(0),
		}
	case contracts.ProtocolVLess:
		user.Protocol = contracts.ProtocolVLess
		user.Extensions = map[string]any{
			"uuid": "a1b2c3d4-a1b2-a1b2-a1b2-a1b2c3d4e5f6",
			"flow": flow,
		}
	case contracts.ProtocolTrojan:
		user.Protocol = contracts.ProtocolTrojan
		user.Extensions = map[string]any{
			"password": "test-password-123",
		}
	case contracts.ProtocolShadowsocks:
		user.Protocol = contracts.ProtocolShadowsocks
		user.Extensions = map[string]any{
			"password": "test-password-123",
			"method":   method,
		}
	}

	return user
}

// createE2EClientConfig creates client config for end-to-end testing.
func createE2EClientConfig(proto contracts.Protocol, user contracts.UserSpec, serverPort, socksPort int) map[string]any {
	cfg := map[string]any{
		"log":   map[string]any{"loglevel": "debug"},
		"stats": map[string]any{},
		"inbounds": []map[string]any{
			{
				"tag":      "socks-out",
				"protocol": "socks",
				"listen":   "127.0.0.1",
				"port":     socksPort,
				"settings": map[string]any{"udp": false},
			},
		},
		"outbounds": []map[string]any{},
	}

	var outbound map[string]any

	switch proto {
	case contracts.ProtocolVMess:
		outbound = map[string]any{
			"tag":      "proxy",
			"protocol": "vmess",
			"settings": map[string]any{
				"vnext": []map[string]any{
					{
						"address": "127.0.0.1",
						"port":    serverPort,
						"users": []map[string]any{
							{
								"id":       user.Extensions["uuid"],
								"alterId":  user.Extensions["alter_id"],
								"security": "auto",
							},
						},
					},
				},
			},
		}
	case contracts.ProtocolVLess:
		outbound = map[string]any{
			"tag":      "proxy",
			"protocol": "vless",
			"settings": map[string]any{
				"vnext": []map[string]any{
					{
						"address": "127.0.0.1",
						"port":    serverPort,
						"users": []map[string]any{
							{
								"id":       user.Extensions["uuid"],
								"flow":     user.Extensions["flow"],
								"security": "auto",
							},
						},
					},
				},
			},
		}
	case contracts.ProtocolTrojan:
		outbound = map[string]any{
			"tag":      "proxy",
			"protocol": "trojan",
			"settings": map[string]any{
				"servers": []map[string]any{
					{
						"address":  "127.0.0.1",
						"port":     serverPort,
						"password": user.Password,
						"method":   "auto",
					},
				},
			},
		}
	case contracts.ProtocolShadowsocks:
		outbound = map[string]any{
			"tag":      "proxy",
			"protocol": "shadowsocks",
			"settings": map[string]any{
				"servers": []map[string]any{
					{
						"address":  "127.0.0.1",
						"port":     serverPort,
						"password": user.Password,
						"method":   user.Extensions["method"],
					},
				},
			},
		}
	}

	cfg["outbounds"] = []map[string]any{outbound, {"protocol": "freedom", "tag": "direct"}}
	return cfg
}

// writeJSONFile writes a JSON file.
func writeJSONFile(path string, data any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

// TestXrayE2E_WithVMess tests VMess specifically with detailed logging.
func TestXrayE2E_WithVMess(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}

	testE2EProtocol(t, xrayBin, contracts.ProtocolVMess, false, "", "")
}

// TestXrayE2E_WithVLESS tests VLESS specifically.
func TestXrayE2E_WithVLESS(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}

	testE2EProtocol(t, xrayBin, contracts.ProtocolVLess, false, "xtls-rprx-vision", "")
}

// TestXrayE2E_WithTrojan tests Trojan specifically.
func TestXrayE2E_WithTrojan(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}

	testE2EProtocol(t, xrayBin, contracts.ProtocolTrojan, false, "", "")
}

// TestXrayE2E_WithShadowsocks tests Shadowsocks specifically.
func TestXrayE2E_WithShadowsocks(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}

	testE2EProtocol(t, xrayBin, contracts.ProtocolShadowsocks, false, "", "aes-256-gcm")
}

// TestXrayE2E_SOCKS5AndHTTP tests SOCKS5 and HTTP as baseline.
func TestXrayE2E_SOCKS5AndHTTP(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}

	// Test SOCKS5
	t.Run("SOCKS5", func(t *testing.T) {
		testE2EProtocol(t, xrayBin, contracts.ProtocolSOCKS5, false, "", "")
	})

	// Test HTTP with auth
	t.Run("HTTP", func(t *testing.T) {
		testE2EHTTPWithAuth(t, xrayBin)
	})
}

// testE2EHTTPWithAuth tests HTTP proxy with authentication.
func testE2EHTTPWithAuth(t *testing.T, xrayBin string) {
	serverAPIPort, _ := freeTCPPort()
	inboundPort, _ := freeTCPPort()
	clientSocksPort, _ := freeTCPPort()

	// Server config
	serverCfgPath := filepath.Join(t.TempDir(), "server.json")
	serverCfg := map[string]any{
		"log":      map[string]any{"loglevel": "debug"},
		"stats":    map[string]any{},
		"policy":   map[string]any{},
		"inbounds": []map[string]any{},
		"outbounds": []map[string]any{
			{"protocol": "freedom", "tag": "direct"},
		},
	}
	serverCfg["api"] = map[string]any{
		"tag":      "api",
		"services": []string{"HandlerService", "LoggerService", "StatsService"},
		"port":     fmt.Sprintf("%d", serverAPIPort),
		"listen":   "127.0.0.1",
		"protocol": "dokodemo-door",
		"settings": map[string]any{"address": "127.0.0.1"},
	}

	writeJSONFile(serverCfgPath, serverCfg)

	serverExec, _ := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: serverCfgPath,
		GRPCAPIAddress: fmt.Sprintf("127.0.0.1:%d", serverAPIPort),
	})
	serverExec.Start()
	defer serverExec.Stop()

	waitPort(fmt.Sprintf("127.0.0.1:%d", serverAPIPort), 15*time.Second)

	// Add HTTP inbound
	adapter := xray.NewAdapter()
	spec := contracts.InboundSpec{
		Tag:      "http-test",
		Port:     uint32(inboundPort),
		Protocol: contracts.ProtocolHTTP,
	}

	nativeInbound, _ := adapter.ToProvider(spec)
	serverExec.AddInboundNative(nativeInbound.JSON)
	defer serverExec.RemoveInboundNative("http-test")

	// Add user
	user := contracts.UserSpec{
		Email:      "test@http.local",
		Level:      0,
		Protocol:   contracts.ProtocolHTTP,
		Extensions: map[string]any{"user": "testuser", "pass": "testpass123"},
	}
	serverExec.AddUser("http-test", user)

	waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 10*time.Second)

	// Client socks to HTTP - need a way to connect to HTTP proxy
	// For now, just verify the inbound is working
	t.Logf("HTTP inbound ready on port %d with auth", inboundPort)
}

// TestXrayE2E_VerifyInboundList verifies that added inbounds are correctly listed.
func TestXrayE2E_VerifyInboundList(t *testing.T) {
	xrayBin := os.Getenv("XRAY_BIN")
	if xrayBin == "" {
		t.Skip("integration test skipped: XRAY_BIN not set")
	}

	serverAPIPort, _ := freeTCPPort()

	serverCfgPath := filepath.Join(t.TempDir(), "server.json")
	serverCfg := map[string]any{
		"log":       map[string]any{"loglevel": "warning"},
		"stats":     map[string]any{},
		"policy":    map[string]any{},
		"inbounds":  []map[string]any{},
		"outbounds": []map[string]any{{"protocol": "freedom", "tag": "direct"}},
	}
	serverCfg["api"] = map[string]any{
		"tag":      "api",
		"services": []string{"HandlerService", "LoggerService", "StatsService"},
		"port":     fmt.Sprintf("%d", serverAPIPort),
		"listen":   "127.0.0.1",
		"protocol": "dokodemo-door",
		"settings": map[string]any{"address": "127.0.0.1"},
	}

	writeJSONFile(serverCfgPath, serverCfg)

	serverExec, _ := xray.NewExecutor(xray.ExecutorConfig{
		BinaryPath:     xrayBin,
		ConfigFilePath: serverCfgPath,
		GRPCAPIAddress: fmt.Sprintf("127.0.0.1:%d", serverAPIPort),
	})
	serverExec.Start()
	defer serverExec.Stop()

	waitPort(fmt.Sprintf("127.0.0.1:%d", serverAPIPort), 15*time.Second)

	// Add multiple inbounds
	adapter := xray.NewAdapter()

	protocols := []contracts.Protocol{
		contracts.ProtocolSOCKS5,
		contracts.ProtocolVMess,
		contracts.ProtocolVLess,
		contracts.ProtocolTrojan,
		contracts.ProtocolShadowsocks,
	}

	for i, proto := range protocols {
		port, _ := freeTCPPort()
		tag := fmt.Sprintf("verify-%s-%d", proto, i)

		spec := contracts.InboundSpec{
			Tag:      tag,
			Port:     uint32(port),
			Protocol: proto,
		}

		nativeInbound, _ := adapter.ToProvider(spec)
		serverExec.AddInboundNative(nativeInbound.JSON)

		waitPort(fmt.Sprintf("127.0.0.1:%d", port), 10*time.Second)
	}

	// List inbounds
	inbounds := serverExec.ListInboundConfigs()
	t.Logf("Total inbounds: %d", len(inbounds))

	if len(inbounds) < len(protocols) {
		t.Errorf("Expected at least %d inbounds, got %d", len(protocols), len(inbounds))
	}

	// Check each protocol is present
	for _, proto := range protocols {
		found := false
		for _, in := range inbounds {
			if in.Protocol() == proto {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Protocol %s not found in inbound list", proto)
		} else {
			t.Logf("Found %s in inbound list", proto)
		}
	}
}
