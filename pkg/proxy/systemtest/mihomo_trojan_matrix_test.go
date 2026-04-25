//go:build integration

package systemtest

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/converter"
)

// TestMihomoTrojanMatrix drives Phase 3 Trojan transport/security coverage
// through a real mihomo server and a real mihomo client.
func TestMihomoTrojanMatrix(t *testing.T) {
	if os.Getenv("MIHOMO_BIN") == "" && !probeURL("https://github.com") {
		t.Skip("MIHOMO_BIN not set and github.com unreachable for Alpha download")
	}
	upstream, ok := canReachPublicUpstream(t)
	if !ok {
		t.Skipf("no public upstream reachable (%s); set SYSTEST_UPSTREAM_URL", upstream)
	}

	tmpDir := t.TempDir()
	mihomoBin := ensureMihomoBin(t, tmpDir)
	rig := startMihomoContainer(t, tmpDir, mihomoBin)

	certPath, keyPath, _, _, err := writeTempCert(rig.dataDir)
	if err != nil {
		t.Fatalf("writeTempCert: %v", err)
	}
	realityPriv, realityPub := mustGenerateRealityKeyPair(t)

	cases := []trojanMatrixCase{
		{name: "tcp-tls", transport: "tcp", security: "tls", certFile: certPath, keyFile: keyPath},
		{name: "ws-tls", transport: "ws", security: "tls", certFile: certPath, keyFile: keyPath, wsPath: "/trojan-ws"},
		{name: "grpc-tls", transport: "grpc", security: "tls", certFile: certPath, keyFile: keyPath, grpcServiceName: "TrojanTunnel"},
		{
			name:         "tcp-reality",
			transport:    "tcp",
			security:     "reality",
			realityPriv:  realityPriv,
			realityPub:   realityPub,
			realityDest:  "www.microsoft.com:443",
			realityNames: []string{"www.microsoft.com"},
			realityShort: []string{"aabbccddeeff0011"},
		},
		{
			name:         "ws-reality",
			transport:    "ws",
			security:     "reality",
			wsPath:       "/trojan-ws",
			realityPriv:  realityPriv,
			realityPub:   realityPub,
			realityDest:  "www.microsoft.com:443",
			realityNames: []string{"www.microsoft.com"},
			realityShort: []string{"1122334455667788"},
			skipReason:   "mihomo Alpha Trojan+ws+reality returns unexpected HTTP 200 during client connect, matching the VLESS/VMess ws+reality stack-order limitation",
		},
		{
			name:            "grpc-reality",
			transport:       "grpc",
			security:        "reality",
			grpcServiceName: "TrojanTunnel",
			realityPriv:     realityPriv,
			realityPub:      realityPub,
			realityDest:     "www.microsoft.com:443",
			realityNames:    []string{"www.microsoft.com"},
			realityShort:    []string{"99aabbccddeeff00"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runTrojanMatrixCase(t, rig, mihomoBin, tc, upstream)
		})
	}

	t.Run("subscription_chain_tcp_tls", func(t *testing.T) {
		runTrojanSubscriptionChainCase(t, rig, mihomoBin, trojanMatrixCase{
			name:      "subscription-chain-tcp-tls",
			transport: "tcp",
			security:  "tls",
			certFile:  certPath,
			keyFile:   keyPath,
		}, upstream)
	})
	t.Run("subscription_chain_grpc_reality", func(t *testing.T) {
		runTrojanSubscriptionChainCase(t, rig, mihomoBin, trojanMatrixCase{
			name:            "subscription-chain-grpc-reality",
			transport:       "grpc",
			security:        "reality",
			grpcServiceName: "TrojanTunnel",
			realityPriv:     realityPriv,
			realityPub:      realityPub,
			realityDest:     "www.microsoft.com:443",
			realityNames:    []string{"www.microsoft.com"},
			realityShort:    []string{"2233445566778899"},
		}, upstream)
	})
}

type trojanMatrixCase struct {
	name      string
	transport string // tcp | ws | grpc
	security  string // tls | reality

	certFile string
	keyFile  string

	realityPriv  string
	realityPub   string
	realityDest  string
	realityNames []string
	realityShort []string

	wsPath          string
	grpcServiceName string

	skipReason string
}

func runTrojanMatrixCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc trojanMatrixCase, upstream string) {
	t.Helper()
	if tc.skipReason != "" {
		t.Skip(tc.skipReason)
	}

	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	tag := fmt.Sprintf("trojan-%s-%d", tc.name, inboundPort)
	password := fmt.Sprintf("trojan-pass-%d", inboundPort)

	params := map[string]any{
		"protocol":  "trojan",
		"port":      inboundPort,
		"password":  password,
		"transport": tc.transport,
		"security":  tc.security,
	}
	switch tc.security {
	case "tls":
		params["cert_file"] = tc.certFile
		params["key_file"] = tc.keyFile
		// This matrix shares one fixture cert across TLS cases. Mark it as
		// file-owned so per-case RemoveInboundConfig does not delete it.
		params["cert_source"] = "file"
	case "reality":
		params["reality_target"] = tc.realityDest
		params["reality_server_names"] = tc.realityNames
		params["reality_short_ids"] = tc.realityShort
		params["reality_private_key"] = tc.realityPriv
		params["reality_public_key"] = tc.realityPub
	}
	if tc.wsPath != "" {
		params["ws_path"] = tc.wsPath
	}
	if tc.grpcServiceName != "" {
		params["grpc_service_name"] = tc.grpcServiceName
	}

	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 15*time.Second); err != nil {
		t.Fatalf("mihomo listener %d not ready: %v", inboundPort, err)
	}

	username := fmt.Sprintf("alice-trojan-%s-%d", tc.name, inboundPort)
	forwardPort := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))

	clientCaseDir := filepath.Join(t.TempDir(), tc.name)
	if err := os.MkdirAll(clientCaseDir, 0o755); err != nil {
		t.Fatalf("mkdir client case dir: %v", err)
	}
	proxy := buildTrojanClashProxy(tc, password, "127.0.0.1", int(forwardPort))
	socksPort := spawnMihomoClient(t, clientCaseDir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    filepath.Join(clientCaseDir, "data"),
		Proxy:      proxy,
	})

	status, body := httpGetViaSocks5(t, socksPort, upstream)
	if status < 200 || status >= 400 {
		t.Fatalf("upstream via trojan chain: status=%d body=%q", status, body)
	}
}

func runTrojanSubscriptionChainCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc trojanMatrixCase, upstream string) {
	t.Helper()

	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	tag := fmt.Sprintf("trojan-%s-%d", tc.name, inboundPort)
	password := fmt.Sprintf("trojan-pass-%d", inboundPort)

	params := map[string]any{
		"protocol":  "trojan",
		"port":      inboundPort,
		"password":  password,
		"transport": tc.transport,
		"security":  tc.security,
	}
	switch tc.security {
	case "tls":
		params["cert_file"] = tc.certFile
		params["key_file"] = tc.keyFile
		params["cert_source"] = "file"
		params["sni"] = "localhost"
		params["skip_cert_verify"] = true
	case "reality":
		params["reality_target"] = tc.realityDest
		params["reality_server_names"] = tc.realityNames
		params["reality_short_ids"] = tc.realityShort
		params["reality_private_key"] = tc.realityPriv
		params["reality_public_key"] = tc.realityPub
	}
	if tc.grpcServiceName != "" {
		params["grpc_service_name"] = tc.grpcServiceName
	}
	if tc.wsPath != "" {
		params["ws_path"] = tc.wsPath
	}

	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 15*time.Second); err != nil {
		t.Fatalf("mihomo listener %d not ready: %v", inboundPort, err)
	}

	username := fmt.Sprintf("alice-trojan-chain-%s-%d", tc.name, inboundPort)
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
	if extString(specs[0].Extensions, "security") == "reality" {
		specs[0].Extensions["utls_fingerprint"] = "chrome"
	}

	proxy := trojanProxyFromSpec(t, specs[0])
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
		t.Fatalf("upstream via trojan subscription chain: status=%d body=%q", status, body)
	}
}

func trojanProxyFromSpec(t *testing.T, spec contracts.SubscriptionSpec) map[string]any {
	t.Helper()
	cp := converter.ConvertTrojanForTest(spec)
	if cp == nil {
		t.Fatalf("convertTrojan returned nil for spec: %+v", spec)
	}
	if extString(spec.Extensions, "security") == "reality" && cp.RealityOpts == nil {
		t.Fatalf("convertTrojan reality spec did not populate RealityOpts")
	}

	p := map[string]any{
		"name":     "trojan-subscription-chain",
		"type":     "trojan",
		"server":   cp.Server,
		"port":     cp.Port,
		"password": cp.Password,
		"udp":      cp.UDP,
		"tls":      cp.TLS,
	}
	if cp.SNI != "" {
		p["sni"] = cp.SNI
	}
	if cp.SkipCertVerify {
		p["skip-cert-verify"] = true
	}
	if cp.RealityOpts != nil {
		p["reality-opts"] = map[string]any{
			"public-key": cp.RealityOpts.PublicKey,
			"short-id":   cp.RealityOpts.ShortID,
		}
	}
	if cp.ClientFingerprint != "" {
		p["client-fingerprint"] = cp.ClientFingerprint
	}
	if cp.Network != "" {
		p["network"] = cp.Network
	}
	if cp.WSOpts != nil {
		opts := map[string]any{}
		if cp.WSOpts.Path != "" {
			opts["path"] = cp.WSOpts.Path
		}
		if cp.WSOpts.Headers != nil && cp.WSOpts.Headers.Host != "" {
			opts["headers"] = map[string]any{"Host": cp.WSOpts.Headers.Host}
		}
		p["ws-opts"] = opts
	}
	if cp.GrpcOpts != nil {
		p["grpc-opts"] = map[string]any{
			"grpc-service-name": cp.GrpcOpts.GrpcServiceName,
		}
	}
	return p
}

func buildTrojanClashProxy(tc trojanMatrixCase, password, server string, port int) map[string]any {
	p := map[string]any{
		"name":     "trojan-test",
		"type":     "trojan",
		"server":   server,
		"port":     port,
		"password": password,
		"udp":      true,
		"tls":      true,
	}
	switch tc.security {
	case "tls":
		p["sni"] = "localhost"
		p["skip-cert-verify"] = true
	case "reality":
		p["sni"] = tc.realityNames[0]
		p["reality-opts"] = map[string]any{
			"public-key": tc.realityPub,
			"short-id":   tc.realityShort[0],
		}
		p["client-fingerprint"] = "chrome"
	}
	switch tc.transport {
	case "ws":
		p["network"] = "ws"
		opts := map[string]any{}
		if tc.wsPath != "" {
			opts["path"] = tc.wsPath
		}
		p["ws-opts"] = opts
	case "grpc":
		p["network"] = "grpc"
		p["grpc-opts"] = map[string]any{
			"grpc-service-name": tc.grpcServiceName,
		}
	}
	return p
}
