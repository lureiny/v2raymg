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

// TestMihomoVMessMatrix drives a real VMess transport × security matrix
// through the full stack:
//
//	curl (Go http.Client via SOCKS5)
//	  → mihomo CLIENT (spawnMihomoClient, outbound=vmess)
//	    → forward port (ForwardManager)
//	      → mihomo SERVER (startMihomoContainer + FastAddInbound vmess)
//	        → DIRECT outbound
//	          → public upstream (SYSTEST_UPSTREAM_URL, default google.com)
//
// Phase 2 T-M-P2-24. Build tag "integration" — skipped by default `go test`.
//
// Matrix scope (7 cases): mihomo Alpha's VmessOption exposes ws-path /
// grpc-service-name + TLS/reality, so transport is limited to tcp/ws/grpc
// and security to none/tls/reality.
//
// Skip conditions:
//   - MIHOMO_BIN unset AND no GitHub egress → skip
//   - No public upstream reachable → skip (not fail)
//
// ws-reality follows the VLESS precedent — Phase 1 documented that mihomo
// Alpha VLESS+ws+reality has a client/server stack-order mismatch.
// VMess+ws+reality uses the same mihomo internal plumbing so the same
// failure mode is expected; runs through the matrix with skipReason
// pre-set, and gets re-evaluated when mihomo upstream publishes a fix.
func TestMihomoVMessMatrix(t *testing.T) {
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

	certPath, keyPath, _, _, err := writeTempCert(rig.dataDir)
	if err != nil {
		t.Fatalf("writeTempCert: %v", err)
	}

	realityPriv, realityPub := mustGenerateRealityKeyPair(t)
	t.Logf("reality keypair generated (pub=%s…)", realityPub[:min(12, len(realityPub))])

	cases := []vmessMatrixCase{
		{
			name:      "tcp-plain",
			transport: "tcp",
			security:  "", // VMess supports plain tcp; no security at all
		},
		{
			name:      "tcp-tls",
			transport: "tcp",
			security:  "tls",
			certFile:  certPath,
			keyFile:   keyPath,
		},
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
			name:      "ws-tls",
			transport: "ws",
			security:  "tls",
			certFile:  certPath,
			keyFile:   keyPath,
			wsPath:    "/vmess-ws",
		},
		{
			name:            "grpc-tls",
			transport:       "grpc",
			security:        "tls",
			certFile:        certPath,
			keyFile:         keyPath,
			grpcServiceName: "VmessTunnel",
		},
		{
			name:            "grpc-reality",
			transport:       "grpc",
			security:        "reality",
			grpcServiceName: "VmessTunnel",
			realityPriv:     realityPriv,
			realityPub:      realityPub,
			realityDest:     "www.microsoft.com:443",
			realityNames:    []string{"www.microsoft.com"},
			realityShort:    []string{"1122334455667788"},
		},
		{
			// Phase 1 documented mihomo Alpha VLESS+ws+reality as an edge
			// case where client/server disagree on stack order (client
			// serialises inside-out, server expects outside-in). VMess
			// reuses the same reality plumbing in mihomo's listener, so
			// the same symptom is expected here. Skip-with-reason keeps
			// the row visible in test output; lift the skip once mihomo
			// upstream publishes a fix.
			name:         "ws-reality",
			transport:    "ws",
			security:     "reality",
			wsPath:       "/vmess-ws",
			realityPriv:  realityPriv,
			realityPub:   realityPub,
			realityDest:  "www.microsoft.com:443",
			realityNames: []string{"www.microsoft.com"},
			realityShort: []string{"aabbccddeeff0011"},
			skipReason:   "mihomo Alpha VMess+ws+reality shares the VLESS stack-order mismatch (Phase 1 documented, same root cause)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runVMessMatrixCase(t, rig, mihomoBin, tc, upstream)
		})
	}

	// Cross-cutting subtest — verifies the **real subscription chain**
	// (GetUserSubscriptions → ClashConverter.convertVMess) end-to-end
	// for a vmess+reality node, instead of the matrix's hand-written
	// buildVMessClashProxy which bypasses the converter.
	//
	// The Phase 2 review caught a P0 where convertVMess had no reality
	// branch — emitting tls=false / no reality-opts — and the matrix
	// didn't see it because every case bypassed the converter. This
	// subtest catches the regression class going forward.
	t.Run("subscription_chain_reality", func(t *testing.T) {
		runVMessSubscriptionChainCase(t, rig, mihomoBin, vmessMatrixCase{
			name:         "subscription-chain-grpc-reality",
			transport:    "grpc",
			security:     "reality",
			grpcServiceName: "VmessTunnel",
			realityPriv:  realityPriv,
			realityPub:   realityPub,
			realityDest:  "www.microsoft.com:443",
			realityNames: []string{"www.microsoft.com"},
			realityShort: []string{"aabbccddeeff0011"},
		}, upstream)
	})
}

type vmessMatrixCase struct {
	name string

	transport string // tcp | ws | grpc
	security  string // "" (none) | tls | reality

	// TLS material (self-signed — the client runs with skip-cert-verify)
	certFile string
	keyFile  string

	// Reality
	realityPriv  string
	realityPub   string
	realityDest  string
	realityNames []string
	realityShort []string

	// Transport sub-fields
	wsPath          string
	grpcServiceName string

	skipReason string
}

func runVMessMatrixCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc vmessMatrixCase, upstream string) {
	t.Helper()
	if tc.skipReason != "" {
		t.Skip(tc.skipReason)
	}

	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	tag := fmt.Sprintf("vmess-%s-%d", tc.name, inboundPort)
	uuid := fmt.Sprintf("%08x-1111-2222-3333-%012x", inboundPort, inboundPort)

	params := map[string]any{
		"protocol":  "vmess",
		"port":      inboundPort,
		"uuid":      uuid,
		"transport": tc.transport,
	}
	if tc.security != "" {
		params["security"] = tc.security
	}
	switch tc.security {
	case "tls":
		params["cert_file"] = tc.certFile
		params["key_file"] = tc.keyFile
		params["cert_source"] = "self_signed"
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

	t.Logf("FastAddInbound %s on 127.0.0.1:%d", tag, inboundPort)
	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 15*time.Second); err != nil {
		t.Fatalf("mihomo listener %d not ready: %v", inboundPort, err)
	}

	username := fmt.Sprintf("alice-vmess-%s-%d", tc.name, inboundPort)
	forwardPort := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))
	t.Logf("user %q allocated forward port %d -> 127.0.0.1:%d", username, forwardPort, inboundPort)

	clientCaseDir := filepath.Join(t.TempDir(), tc.name)
	if err := os.MkdirAll(clientCaseDir, 0o755); err != nil {
		t.Fatalf("mkdir client case dir: %v", err)
	}

	proxy := buildVMessClashProxy(tc, uuid, "127.0.0.1", int(forwardPort))
	socksPort := spawnMihomoClient(t, clientCaseDir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    filepath.Join(clientCaseDir, "data"),
		Proxy:      proxy,
	})

	t.Logf("mihomo client listening socks5 on 127.0.0.1:%d", socksPort)
	status, body := httpGetViaSocks5(t, socksPort, upstream)
	if status < 200 || status >= 400 {
		t.Fatalf("upstream via vmess chain: status=%d body=%q", status, body)
	}
	t.Logf("PASS: %s upstream=%s status=%d via vmess %s chain", tc.name, upstream, status, tc.name)
}

// runVMessSubscriptionChainCase wires the same FastAddInbound path as
// runVMessMatrixCase, but builds the mihomo client outbound by calling
// GetUserSubscriptions → ClashConverter.convertVMess instead of the
// hand-written buildVMessClashProxy. Catches converter-vs-fill drift
// (Phase 2 review P0 was exactly this class of bug).
func runVMessSubscriptionChainCase(t *testing.T, rig *mihomoTestRig, mihomoBin string, tc vmessMatrixCase, upstream string) {
	t.Helper()

	inboundPort, err := freeTCPPort()
	if err != nil {
		t.Fatalf("freeTCPPort: %v", err)
	}
	tag := fmt.Sprintf("vmess-%s-%d", tc.name, inboundPort)
	uuid := fmt.Sprintf("%08x-1111-2222-3333-%012x", inboundPort, inboundPort)

	params := map[string]any{
		"protocol":  "vmess",
		"port":      inboundPort,
		"uuid":      uuid,
		"transport": tc.transport,
		"security":  tc.security,
	}
	switch tc.security {
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

	t.Logf("FastAddInbound %s on 127.0.0.1:%d", tag, inboundPort)
	if err := rig.container.FastAddInbound(tag, params); err != nil {
		t.Fatalf("FastAddInbound: %v", err)
	}
	t.Cleanup(func() { _ = rig.container.RemoveInboundConfig(tag) })

	if err := waitPort(fmt.Sprintf("127.0.0.1:%d", inboundPort), 15*time.Second); err != nil {
		t.Fatalf("mihomo listener %d not ready: %v", inboundPort, err)
	}

	username := fmt.Sprintf("alice-vmess-chain-%d", inboundPort)
	forwardPort := addMihomoUserAndWaitForPort(t, rig, username, uint32(inboundPort))

	// Pull the spec out of the real subscription pipeline. Loopback
	// host so the client connects to the locally-bound forward port.
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
	// Override port to the actual forward-allocated port — Host is
	// already 127.0.0.1, but Port came from the user manager allocation
	// not from the request, so we trust GetUserSubscriptions's port.
	if specs[0].Port != forwardPort {
		t.Fatalf("spec.Port = %d, want forward port %d", specs[0].Port, forwardPort)
	}

	// Reality clients require a uTLS fingerprint; mihomo refuses to
	// connect with "REALITY is based on uTLS, please set a
	// client-fingerprint". xray's subscription emits fp= only when
	// caller supplied utls_fingerprint, and v2raymg propagates the
	// same way. For this end-to-end test we inject it into Extensions
	// directly to mimic a caller-supplied value, so the test asserts
	// the converter chain (the Phase 2 review P0 surface) without
	// also entangling itself in caller-supplied uTLS-fingerprint
	// plumbing — which is a separate concern outside Phase 2 scope.
	if extString(specs[0].Extensions, "security") == "reality" {
		specs[0].Extensions["utls_fingerprint"] = "chrome"
	}

	// Build the mihomo client outbound via the real ClashConverter.
	// This is what production subscription serving emits, so any
	// converter-vs-fill drift surfaces here.
	proxy := vmessProxyFromSpec(t, specs[0])

	clientCaseDir := filepath.Join(t.TempDir(), tc.name)
	if err := os.MkdirAll(clientCaseDir, 0o755); err != nil {
		t.Fatalf("mkdir client case dir: %v", err)
	}
	socksPort := spawnMihomoClient(t, clientCaseDir, mihomoClientConfig{
		BinaryPath: mihomoBin,
		DataDir:    filepath.Join(clientCaseDir, "data"),
		Proxy:      proxy,
	})

	t.Logf("subscription-chain client listening socks5 on 127.0.0.1:%d", socksPort)
	status, body := httpGetViaSocks5(t, socksPort, upstream)
	if status < 200 || status >= 400 {
		t.Fatalf("upstream via subscription-chain vmess: status=%d body=%q", status, body)
	}
	t.Logf("PASS: %s upstream=%s status=%d via subscription chain", tc.name, upstream, status)
}

// vmessProxyFromSpec converts a SubscriptionSpec to a mihomo client
// outbound proxy map by routing it through the same ClashConverter
// production uses. Reflects the on-the-wire shape the converter would
// emit into a clash YAML, then unmarshals back into a map for
// spawnMihomoClient.
//
// We construct a tiny ClashProxy directly via convertVMess (exposed as
// converter.ConvertVMessForTest). Going through ConvertWithOptions
// would require fetching an external clash template (network call),
// which is unwanted in this loopback test.
func vmessProxyFromSpec(t *testing.T, spec contracts.SubscriptionSpec) map[string]any {
	t.Helper()
	cp := converter.ConvertVMessForTest(spec)
	if cp == nil {
		t.Fatalf("convertVMess returned nil for spec: %+v", spec)
	}
	// Confirm the converter did the right thing for reality (the bug
	// we are guarding against). Fail loud if either marker is missing
	// — that's the regression class the cross-cutting test exists for.
	if extString(spec.Extensions, "security") == "reality" {
		if !cp.TLS {
			t.Fatalf("convertVMess reality spec did not set TLS=true (regression of Phase 2 review P0)")
		}
		if cp.RealityOpts == nil {
			t.Fatalf("convertVMess reality spec did not populate RealityOpts (regression of Phase 2 review P0)")
		}
	}

	p := map[string]any{
		"name":    "vmess-subscription-chain",
		"type":    "vmess",
		"server":  cp.Server,
		"port":    cp.Port,
		"uuid":    cp.UUID,
		"alterId": derefIntOr(cp.AlterId, 0),
		"cipher":  cp.Cipher,
		"udp":     cp.UDP,
	}
	if cp.TLS {
		p["tls"] = true
	}
	if cp.SkipCertVerify {
		p["skip-cert-verify"] = true
	}
	if cp.Servername != "" {
		p["servername"] = cp.Servername
	}
	if cp.SNI != "" && cp.Servername == "" {
		p["servername"] = cp.SNI
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

func derefIntOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

func extString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// buildVMessClashProxy builds the single outbound clash proxy map the
// client uses to talk to the VMess server. Field names match mihomo's
// clash-outbound vmess schema (mihomo/adapter/outbound/vmess.go).
//
// cipher is required on the client side (mihomo's outbound VMess refuses
// to start with an empty cipher); "auto" is the canonical choice and
// matches what convertVMess hardcodes in clash.go:192.
func buildVMessClashProxy(tc vmessMatrixCase, uuid, server string, port int) map[string]any {
	p := map[string]any{
		"name":    "vmess-test",
		"type":    "vmess",
		"server":  server,
		"port":    port,
		"uuid":    uuid,
		"alterId": 0,
		"cipher":  "auto",
		"udp":     true,
	}
	switch tc.security {
	case "tls":
		p["tls"] = true
		p["skip-cert-verify"] = true
		p["servername"] = "localhost"
	case "reality":
		p["tls"] = true
		p["servername"] = tc.realityNames[0]
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
