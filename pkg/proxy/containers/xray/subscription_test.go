package xray

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Unit tests: credential extraction
// ---------------------------------------------------------------------------

func TestExtractCredential(t *testing.T) {
	tests := []struct {
		name    string
		inbound *XrayInbound
		user    contracts.UserSpec
		want    string
		wantErr bool
	}{
		{
			name: "VLESS with inbound defaultClientUUID",
			inbound: &XrayInbound{
				tag:               "test-inbound",
				protocol:          contracts.ProtocolVLess,
				defaultClientUUID: "vless-uuid-123",
			},
			user: contracts.UserSpec{
				Username:   "u@test.com",
				Extensions: map[string]any{"uuid": "should-be-ignored"},
			},
			want: "vless-uuid-123",
		},
		{
			name: "VMess with inbound defaultClientUUID",
			inbound: &XrayInbound{
				tag:               "test-inbound",
				protocol:          contracts.ProtocolVMess,
				defaultClientUUID: "vmess-uuid-456",
			},
			user: contracts.UserSpec{
				Username:   "u@test.com",
				Extensions: map[string]any{"uuid": "should-be-ignored"},
			},
			want: "vmess-uuid-456",
		},
		{
			name: "Trojan with password from inbound defaultPassword",
			inbound: &XrayInbound{
				tag:             "test-inbound",
				protocol:        contracts.ProtocolTrojan,
				defaultPassword: "trojan-pass",
			},
			user: contracts.UserSpec{
				Username:   "u@test.com",
				Extensions: map[string]any{"password": "should-be-ignored"},
			},
			want: "trojan-pass",
		},
		{
			name: "Shadowsocks with password from inbound defaultPassword",
			inbound: &XrayInbound{
				tag:             "test-inbound",
				protocol:        contracts.ProtocolShadowsocks,
				defaultPassword: "ss-pass",
			},
			user: contracts.UserSpec{
				Username:   "u@test.com",
				Extensions: map[string]any{"password": "should-be-ignored"},
			},
			want: "ss-pass",
		},
		{
			name: "VLESS without inbound defaultClientUUID errors",
			inbound: &XrayInbound{
				tag:      "test-inbound",
				protocol: contracts.ProtocolVLess,
			},
			user:    contracts.UserSpec{Username: "u@test.com"},
			wantErr: true,
		},
		{
			name: "Trojan without defaultPassword errors",
			inbound: &XrayInbound{
				tag:      "test-inbound",
				protocol: contracts.ProtocolTrojan,
			},
			user:    contracts.UserSpec{Username: "u@test.com"},
			wantErr: true,
		},
		{
			name: "Unsupported protocol errors",
			inbound: &XrayInbound{
				tag:      "test-inbound",
				protocol: contracts.ProtocolHTTP,
			},
			user:    contracts.UserSpec{Username: "u@test.com"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractCredential(tt.inbound, tt.user)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Unit tests: VLESS URI generation
// ---------------------------------------------------------------------------

func TestGenerateVLESSURI(t *testing.T) {
	tests := []struct {
		name string
		spec contracts.SubscriptionSpec
		want string
	}{
		{
			name: "TCP no TLS",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVLess,
				Host:     "1.2.3.4",
				Port:     443,
				Password: "test-uuid",
				NodeName: "my-node",
				Extensions: map[string]any{
					"transport": "tcp",
					"security":  "none",
				},
			},
			want: "vless://test-uuid@1.2.3.4:443?type=tcp&security=none#my-node",
		},
		{
			name: "WS + TLS",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVLess,
				Host:     "example.com",
				Port:     443,
				Password: "uuid-ws",
				NodeName: "ws-node",
				Extensions: map[string]any{
					"transport":   "ws",
					"security":    "tls",
					"ws_path":     "/vless",
					"ws_host":     "cdn.example.com",
					"server_name": "example.com",
				},
			},
			want: "vless://uuid-ws@example.com:443?type=ws&security=tls&path=/vless&host=cdn.example.com&sni=example.com#ws-node",
		},
		{
			name: "gRPC + TLS + flow",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVLess,
				Host:     "10.0.0.1",
				Port:     8443,
				Password: "uuid-grpc",
				NodeName: "grpc-node",
				Extensions: map[string]any{
					"transport":         "grpc",
					"security":          "tls",
					"grpc_service_name": "mygrpc",
					"server_name":       "grpc.example.com",
					"flow":              "xtls-rprx-vision",
				},
			},
			want: "vless://uuid-grpc@10.0.0.1:8443?type=grpc&security=tls&serviceName=mygrpc&sni=grpc.example.com&flow=xtls-rprx-vision#grpc-node",
		},
		{
			name: "TCP + XTLS + alpn",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVLess,
				Host:     "xtls.example.com",
				Port:     443,
				Password: "uuid-xtls",
				NodeName: "xtls-node",
				Extensions: map[string]any{
					"transport":   "tcp",
					"security":    "xtls",
					"server_name": "xtls.example.com",
					"alpn":        "h2,http/1.1",
					"flow":        "xtls-rprx-direct",
				},
			},
			want: "vless://uuid-xtls@xtls.example.com:443?type=tcp&security=xtls&sni=xtls.example.com&alpn=h2,http/1.1&flow=xtls-rprx-direct#xtls-node",
		},
		// Issue #91: Reality test cases
		{
			name: "TCP + Reality",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVLess,
				Host:     "reality.example.com",
				Port:     443,
				Password: "uuid-reality",
				NodeName: "reality-node",
				Extensions: map[string]any{
					"transport":            "tcp",
					"security":             "reality",
					"server_name":          "www.microsoft.com",
					"reality_public_key":   "ABC123xyz...",
					"reality_server_names": "www.microsoft.com",
					"reality_short_ids":    "0123456789abcdef",
				},
			},
			want: "vless://uuid-reality@reality.example.com:443?type=tcp&security=reality&sni=www.microsoft.com&pbk=ABC123xyz...&serverNames=www.microsoft.com&sid=0123456789abcdef#reality-node",
		},
		{
			name: "TCP + Reality with uTLS",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVLess,
				Host:     "reality.example.com",
				Port:     443,
				Password: "uuid-reality-utls",
				NodeName: "reality-utls-node",
				Extensions: map[string]any{
					"transport":            "tcp",
					"security":             "reality",
					"server_name":          "www.microsoft.com",
					"utls_fingerprint":     "chrome",
					"reality_public_key":   "XYZ987...",
					"reality_server_names": "www.microsoft.com,www.google.com",
					"reality_short_ids":    "abcd1234",
				},
			},
			want: "vless://uuid-reality-utls@reality.example.com:443?type=tcp&security=reality&sni=www.microsoft.com&fp=chrome&pbk=XYZ987...&serverNames=www.microsoft.com,www.google.com&sid=abcd1234#reality-utls-node",
		},
		// xhttp + reality + flow: flow should NOT be in URI
		{
			name: "XHTTP + Reality (flow should be excluded)",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVLess,
				Host:     "reality.example.com",
				Port:     443,
				Password: "uuid-xhttp-reality",
				NodeName: "xhttp-reality-node",
				Extensions: map[string]any{
					"transport":            "xhttp",
					"security":             "reality",
					"server_name":          "www.microsoft.com",
					"xhttp_mode":           "auto",
					"xhttp_path":           "/xhttp",
					"reality_public_key":   "ABC123xyz...",
					"reality_server_names": "www.microsoft.com",
					"reality_short_ids":    "0123456789abcdef",
					"flow":                 "xtls-rprx-vision", // This should be ignored
				},
			},
			// Flow should NOT be in URI for xhttp transport
			want: "vless://uuid-xhttp-reality@reality.example.com:443?type=xhttp&security=reality&sni=www.microsoft.com&pbk=ABC123xyz...&serverNames=www.microsoft.com&sid=0123456789abcdef#xhttp-reality-node",
		},
		// splithttp + reality + flow: flow should NOT be in URI
		{
			name: "SplitHTTP + Reality (flow should be excluded)",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVLess,
				Host:     "reality.example.com",
				Port:     443,
				Password: "uuid-splithttp-reality",
				NodeName: "splithttp-reality-node",
				Extensions: map[string]any{
					"transport":            "splithttp",
					"security":             "reality",
					"server_name":          "www.microsoft.com",
					"xhttp_mode":           "auto",
					"xhttp_path":           "/splithttp",
					"reality_public_key":   "ABC123xyz...",
					"reality_server_names": "www.microsoft.com",
					"reality_short_ids":    "0123456789abcdef",
					"flow":                 "xtls-rprx-vision", // This should be ignored
				},
			},
			// Flow should NOT be in URI for splithttp transport
			want: "vless://uuid-splithttp-reality@reality.example.com:443?type=splithttp&security=reality&sni=www.microsoft.com&pbk=ABC123xyz...&serverNames=www.microsoft.com&sid=0123456789abcdef#splithttp-reality-node",
		},
		// h3 + reality + flow: flow should NOT be in URI
		{
			name: "H3 + Reality (flow should be excluded)",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVLess,
				Host:     "reality.example.com",
				Port:     443,
				Password: "uuid-h3-reality",
				NodeName: "h3-reality-node",
				Extensions: map[string]any{
					"transport":            "h3",
					"security":             "reality",
					"server_name":          "www.microsoft.com",
					"reality_public_key":   "ABC123xyz...",
					"reality_server_names": "www.microsoft.com",
					"reality_short_ids":    "0123456789abcdef",
					"flow":                 "xtls-rprx-vision", // This should be ignored
				},
			},
			// Flow should NOT be in URI for h3 transport
			want: "vless://uuid-h3-reality@reality.example.com:443?type=h3&security=reality&sni=www.microsoft.com&pbk=ABC123xyz...&serverNames=www.microsoft.com&sid=0123456789abcdef#h3-reality-node",
		},
		// TCP + reality + flow: flow SHOULD be in URI
		{
			name: "TCP + Reality (flow should be included)",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVLess,
				Host:     "reality.example.com",
				Port:     443,
				Password: "uuid-tcp-reality",
				NodeName: "tcp-reality-node",
				Extensions: map[string]any{
					"transport":            "tcp",
					"security":             "reality",
					"server_name":          "www.microsoft.com",
					"reality_public_key":   "ABC123xyz...",
					"reality_server_names": "www.microsoft.com",
					"reality_short_ids":    "0123456789abcdef",
					"flow":                 "xtls-rprx-vision",
				},
			},
			// Flow SHOULD be in URI for tcp transport (order: flow comes after pbk)
			want: "vless://uuid-tcp-reality@reality.example.com:443?type=tcp&security=reality&sni=www.microsoft.com&pbk=ABC123xyz...&flow=xtls-rprx-vision&serverNames=www.microsoft.com&sid=0123456789abcdef#tcp-reality-node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateVLESSURI(tt.spec)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Unit tests: VMess URI generation
// ---------------------------------------------------------------------------

func TestGenerateVMessURI(t *testing.T) {
	tests := []struct {
		name        string
		spec        contracts.SubscriptionSpec
		checkFields map[string]interface{}
	}{
		{
			name: "TCP + TLS",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVMess,
				Host:     "1.2.3.4",
				Port:     443,
				Password: "vmess-uuid",
				NodeName: "vmess-node",
				Extensions: map[string]any{
					"transport":   "tcp",
					"security":    "tls",
					"server_name": "example.com",
				},
			},
			checkFields: map[string]interface{}{
				"v":          "2",
				"ps":         "vmess-node",
				"add":        "1.2.3.4",
				"port":       float64(443), // Issue #91: port as integer
				"id":         "vmess-uuid",
				"net":        "tcp",
				"tls":        "tls",
				"sni":        "example.com",
				"encryption": "auto", // Issue #91: encryption defaults to auto
			},
		},
		{
			name: "WS + TLS",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVMess,
				Host:     "example.com",
				Port:     443,
				Password: "vmess-ws-uuid",
				NodeName: "vmess-ws",
				Extensions: map[string]any{
					"transport": "ws",
					"security":  "tls",
					"ws_path":   "/vmess",
					"ws_host":   "cdn.example.com",
				},
			},
			checkFields: map[string]interface{}{
				"net":  "ws",
				"path": "/vmess",
				"host": "cdn.example.com",
				"tls":  "tls",
			},
		},
		{
			name: "gRPC + none",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVMess,
				Host:     "10.0.0.1",
				Port:     8080,
				Password: "vmess-grpc-uuid",
				NodeName: "vmess-grpc",
				Extensions: map[string]any{
					"transport":         "grpc",
					"security":          "none",
					"grpc_service_name": "vmess-svc",
				},
			},
			checkFields: map[string]interface{}{
				"net":  "grpc",
				"path": "vmess-svc",
				"tls":  "none",
			},
		},
		{
			name: "HTTP/2",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVMess,
				Host:     "h2.example.com",
				Port:     443,
				Password: "vmess-h2-uuid",
				NodeName: "vmess-h2",
				Extensions: map[string]any{
					"transport":   "http",
					"security":    "tls",
					"http_host":   "h2.example.com",
					"http_path":   "/h2path",
					"server_name": "h2.example.com",
				},
			},
			checkFields: map[string]interface{}{
				"net":  "h2",
				"host": "h2.example.com",
				"path": "/h2path",
			},
		},
		{
			name: "mKCP",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolVMess,
				Host:     "kcp.example.com",
				Port:     12345,
				Password: "vmess-kcp-uuid",
				NodeName: "vmess-kcp",
				Extensions: map[string]any{
					"transport":        "mkcp",
					"security":         "none",
					"mkcp_header_type": "srtp",
					"mkcp_seed":        "myseed",
				},
			},
			checkFields: map[string]interface{}{
				"net":  "kcp",
				"type": "srtp",
				"path": "myseed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uri, err := generateVMessURI(tt.spec)
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(uri, "vmess://"), "URI should start with vmess://")

			// Decode and verify JSON fields
			encoded := strings.TrimPrefix(uri, "vmess://")
			decoded, err := base64.RawURLEncoding.DecodeString(encoded)
			require.NoError(t, err)

			var cfg map[string]interface{}
			err = json.Unmarshal(decoded, &cfg)
			require.NoError(t, err)

			for key, expected := range tt.checkFields {
				assert.Equal(t, expected, cfg[key], "field %s mismatch", key)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Unit tests: Trojan URI generation
// ---------------------------------------------------------------------------

func TestGenerateTrojanURI(t *testing.T) {
	tests := []struct {
		name string
		spec contracts.SubscriptionSpec
		want string
	}{
		{
			name: "TCP + TLS",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolTrojan,
				Host:     "example.com",
				Port:     443,
				Password: "trojan-password",
				NodeName: "trojan-node",
				Extensions: map[string]any{
					"transport":   "tcp",
					"security":    "tls",
					"server_name": "example.com",
				},
			},
			want: "trojan://trojan-password@example.com:443?type=tcp&security=tls&sni=example.com#trojan-node",
		},
		{
			name: "WS + TLS",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolTrojan,
				Host:     "10.0.0.1",
				Port:     8443,
				Password: "trojan-ws-pass",
				NodeName: "trojan-ws",
				Extensions: map[string]any{
					"transport":   "ws",
					"security":    "tls",
					"ws_path":     "/trojan",
					"server_name": "trojan.example.com",
				},
			},
			want: "trojan://trojan-ws-pass@10.0.0.1:8443?type=ws&security=tls&path=/trojan&sni=trojan.example.com#trojan-ws",
		},
		{
			name: "gRPC + TLS",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolTrojan,
				Host:     "grpc.example.com",
				Port:     443,
				Password: "trojan-grpc-pass",
				NodeName: "trojan-grpc",
				Extensions: map[string]any{
					"transport":         "grpc",
					"security":          "tls",
					"grpc_service_name": "trojan-svc",
					"server_name":       "grpc.example.com",
				},
			},
			want: "trojan://trojan-grpc-pass@grpc.example.com:443?type=grpc&security=tls&serviceName=trojan-svc&sni=grpc.example.com#trojan-grpc",
		},
		{
			name: "with flow",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolTrojan,
				Host:     "flow.example.com",
				Port:     443,
				Password: "trojan-flow-pass",
				NodeName: "trojan-flow",
				Extensions: map[string]any{
					"transport":   "tcp",
					"security":    "xtls",
					"server_name": "flow.example.com",
					"flow":        "xtls-rprx-direct",
				},
			},
			want: "trojan://trojan-flow-pass@flow.example.com:443?type=tcp&security=xtls&sni=flow.example.com&flow=xtls-rprx-direct#trojan-flow",
		},
		// xhttp + reality + flow: flow should NOT be in URI
		{
			name: "XHTTP + Reality (flow should be excluded)",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolTrojan,
				Host:     "reality.example.com",
				Port:     443,
				Password: "trojan-xhttp-reality",
				NodeName: "trojan-xhttp-reality",
				Extensions: map[string]any{
					"transport":          "xhttp",
					"security":           "reality",
					"server_name":        "www.microsoft.com",
					"xhttp_mode":         "auto",
					"xhttp_path":         "/xhttp",
					"reality_public_key": "ABC123xyz...",
					"flow":               "xtls-rprx-vision", // This should be ignored
				},
			},
			// Flow should NOT be in URI for xhttp transport
			want: "trojan://trojan-xhttp-reality@reality.example.com:443?type=xhttp&security=reality&sni=www.microsoft.com&pbk=ABC123xyz...#trojan-xhttp-reality",
		},
		// splithttp + reality + flow: flow should NOT be in URI
		{
			name: "SplitHTTP + Reality (flow should be excluded)",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolTrojan,
				Host:     "reality.example.com",
				Port:     443,
				Password: "trojan-splithttp-reality",
				NodeName: "trojan-splithttp-reality",
				Extensions: map[string]any{
					"transport":          "splithttp",
					"security":           "reality",
					"server_name":        "www.microsoft.com",
					"xhttp_mode":         "auto",
					"xhttp_path":         "/splithttp",
					"reality_public_key": "ABC123xyz...",
					"flow":               "xtls-rprx-vision", // This should be ignored
				},
			},
			// Flow should NOT be in URI for splithttp transport
			want: "trojan://trojan-splithttp-reality@reality.example.com:443?type=splithttp&security=reality&sni=www.microsoft.com&pbk=ABC123xyz...#trojan-splithttp-reality",
		},
		// TCP + xtls + flow: flow SHOULD be in URI
		{
			name: "TCP + XTLS (flow should be included)",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolTrojan,
				Host:     "xtls.example.com",
				Port:     443,
				Password: "trojan-tcp-xtls",
				NodeName: "trojan-tcp-xtls",
				Extensions: map[string]any{
					"transport":   "tcp",
					"security":    "xtls",
					"server_name": "xtls.example.com",
					"flow":        "xtls-rprx-vision",
				},
			},
			// Flow SHOULD be in URI for TCP + xtls
			want: "trojan://trojan-tcp-xtls@xtls.example.com:443?type=tcp&security=xtls&sni=xtls.example.com&flow=xtls-rprx-vision#trojan-tcp-xtls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateTrojanURI(tt.spec)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Unit tests: Shadowsocks SIP002 URI generation
// ---------------------------------------------------------------------------

func TestGenerateShadowsocksURI(t *testing.T) {
	tests := []struct {
		name string
		spec contracts.SubscriptionSpec
		want string
	}{
		{
			// Matches SIP002 wiki example 1
			name: "aes-128-gcm basic (SIP002 example)",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolShadowsocks,
				Host:     "192.168.100.1",
				Port:     8888,
				Password: "test",
				NodeName: "Example1",
				Extensions: map[string]any{
					"method": "aes-128-gcm",
				},
			},
			want: "ss://YWVzLTEyOC1nY206dGVzdA@192.168.100.1:8888#Example1",
		},
		{
			// Matches SIP002 wiki example 2 (with SIP003 plugin)
			name: "rc4-md5 with plugin (SIP002 example)",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolShadowsocks,
				Host:     "192.168.100.1",
				Port:     8888,
				Password: "passwd",
				NodeName: "Example2",
				Extensions: map[string]any{
					"method":         "rc4-md5",
					"ss_plugin":      "obfs-local",
					"ss_plugin_opts": "obfs=http",
				},
			},
			want: "ss://cmM0LW1kNTpwYXNzd2Q@192.168.100.1:8888/?plugin=obfs-local%3Bobfs%3Dhttp#Example2",
		},
		{
			// AEAD-2022 plain-text format (SIP022)
			name: "2022-blake3-aes-256-gcm AEAD-2022",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolShadowsocks,
				Host:     "192.168.100.1",
				Port:     8888,
				Password: "YctPZ6U7xPPcU+gp3u+0tx/tRizJN9K8y+uKlW2qjlI=",
				NodeName: "Example3",
				Extensions: map[string]any{
					"method": "2022-blake3-aes-256-gcm",
				},
			},
			want: "ss://2022-blake3-aes-256-gcm:YctPZ6U7xPPcU%2Bgp3u%2B0tx%2FtRizJN9K8y%2BuKlW2qjlI%3D@192.168.100.1:8888#Example3",
		},
		{
			name: "default method (aes-256-gcm)",
			spec: contracts.SubscriptionSpec{
				Protocol:   contracts.ProtocolShadowsocks,
				Host:       "10.0.0.1",
				Port:       8388,
				Password:   "mypass",
				NodeName:   "SS-Node",
				Extensions: map[string]any{},
			},
			want: fmt.Sprintf("ss://%s@10.0.0.1:8388#SS-Node",
				base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:mypass"))),
		},
		{
			name: "no tag",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolShadowsocks,
				Host:     "1.2.3.4",
				Port:     1234,
				Password: "pass",
				Extensions: map[string]any{
					"method": "chacha20-ietf-poly1305",
				},
			},
			want: fmt.Sprintf("ss://%s@1.2.3.4:1234",
				base64.RawURLEncoding.EncodeToString([]byte("chacha20-ietf-poly1305:pass"))),
		},
		{
			name: "tag with spaces (percent-encoded)",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolShadowsocks,
				Host:     "1.2.3.4",
				Port:     8388,
				Password: "test",
				NodeName: "My SS Node",
				Extensions: map[string]any{
					"method": "aes-256-gcm",
				},
			},
			want: fmt.Sprintf("ss://%s@1.2.3.4:8388#My+SS+Node",
				base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:test"))),
		},
		{
			name: "plugin without options",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolShadowsocks,
				Host:     "10.0.0.1",
				Port:     8388,
				Password: "pass",
				NodeName: "PluginOnly",
				Extensions: map[string]any{
					"method":    "aes-256-gcm",
					"ss_plugin": "v2ray-plugin",
				},
			},
			want: fmt.Sprintf("ss://%s@10.0.0.1:8388/?plugin=v2ray-plugin#PluginOnly",
				base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:pass"))),
		},
		{
			name: "2022-blake3-aes-128-gcm AEAD-2022",
			spec: contracts.SubscriptionSpec{
				Protocol: contracts.ProtocolShadowsocks,
				Host:     "10.0.0.1",
				Port:     8388,
				Password: "abc123+/==",
				NodeName: "AEAD2022-128",
				Extensions: map[string]any{
					"method": "2022-blake3-aes-128-gcm",
				},
			},
			want: "ss://2022-blake3-aes-128-gcm:abc123%2B%2F%3D%3D@10.0.0.1:8388#AEAD2022-128",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := generateShadowsocksURI(tt.spec)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Unit tests: helper functions
// ---------------------------------------------------------------------------

func TestVmessTransportName(t *testing.T) {
	assert.Equal(t, "h2", vmessTransportName("http"))
	assert.Equal(t, "kcp", vmessTransportName("mkcp"))
	assert.Equal(t, "tcp", vmessTransportName("tcp"))
	assert.Equal(t, "ws", vmessTransportName("ws"))
	assert.Equal(t, "grpc", vmessTransportName("grpc"))
	assert.Equal(t, "quic", vmessTransportName("quic"))
}

func TestBuildShareLinkParams(t *testing.T) {
	t.Run("WS + TLS with all fields", func(t *testing.T) {
		spec := contracts.SubscriptionSpec{
			Extensions: map[string]any{
				"transport":   "ws",
				"security":    "tls",
				"ws_path":     "/path",
				"ws_host":     "cdn.example.com",
				"server_name": "example.com",
				"alpn":        "h2,http/1.1",
			},
		}
		params := buildShareLinkParams(spec)
		paramStr := strings.Join(params, "&")

		assert.Contains(t, paramStr, "type=ws")
		assert.Contains(t, paramStr, "security=tls")
		assert.Contains(t, paramStr, "path=/path")
		assert.Contains(t, paramStr, "host=cdn.example.com")
		assert.Contains(t, paramStr, "sni=example.com")
		assert.Contains(t, paramStr, "alpn=h2,http/1.1")
	})

	t.Run("gRPC with mode", func(t *testing.T) {
		spec := contracts.SubscriptionSpec{
			Extensions: map[string]any{
				"transport":         "grpc",
				"security":          "none",
				"grpc_service_name": "mysvc",
				"grpc_mode":         "gun",
			},
		}
		params := buildShareLinkParams(spec)
		paramStr := strings.Join(params, "&")

		assert.Contains(t, paramStr, "serviceName=mysvc")
		assert.Contains(t, paramStr, "mode=gun")
		assert.NotContains(t, paramStr, "sni=") // no TLS
	})

	t.Run("alpn as string slice", func(t *testing.T) {
		spec := contracts.SubscriptionSpec{
			Extensions: map[string]any{
				"transport":   "tcp",
				"security":    "tls",
				"server_name": "sni.test.com",
				"alpn":        []string{"h2", "http/1.1"},
			},
		}
		params := buildShareLinkParams(spec)
		paramStr := strings.Join(params, "&")

		assert.Contains(t, paramStr, "alpn=h2,http/1.1")
	})

	t.Run("no security skips TLS params", func(t *testing.T) {
		spec := contracts.SubscriptionSpec{
			Extensions: map[string]any{
				"transport":   "tcp",
				"security":    "none",
				"server_name": "should-be-ignored.com",
			},
		}
		params := buildShareLinkParams(spec)
		paramStr := strings.Join(params, "&")

		assert.NotContains(t, paramStr, "sni=")
	})
}

func TestBuildSubscriptionExtensions(t *testing.T) {
	in := &XrayInbound{
		transport: contracts.TransportWS,
		security:  contracts.SecurityTLS,
		extra: map[string]interface{}{
			"ws_path":     "/test",
			"server_name": "sni.example.com",
		},
	}

	user := contracts.UserSpec{
		Username: "test@example.com",
		Extensions: map[string]any{
			"flow": "xtls-rprx-vision",
		},
	}

	ext := buildSubscriptionExtensions(in, user)
	assert.Equal(t, "ws", ext["transport"])
	assert.Equal(t, "tls", ext["security"])
	assert.Equal(t, "/test", ext["ws_path"])
	assert.Equal(t, "sni.example.com", ext["server_name"])
	assert.Equal(t, "xtls-rprx-vision", ext["flow"])
}

func TestExtString(t *testing.T) {
	m := map[string]any{
		"key1": "val1",
		"key2": 123,
	}
	assert.Equal(t, "val1", extString(m, "key1"))
	assert.Equal(t, "", extString(m, "key2"))    // not a string
	assert.Equal(t, "", extString(m, "missing")) // missing key
	assert.Equal(t, "", extString(nil, "key1"))  // nil map
}

func TestExtStringSlice(t *testing.T) {
	m := map[string]any{
		"key1": []string{"a", "b"},
		"key2": "not-a-slice",
	}
	assert.Equal(t, []string{"a", "b"}, extStringSlice(m, "key1"))
	assert.Nil(t, extStringSlice(m, "key2"))
	assert.Nil(t, extStringSlice(m, "missing"))
	assert.Nil(t, extStringSlice(nil, "key1"))
}

// ---------------------------------------------------------------------------
// Integration tests: GetUserSubscriptions on Executor
// ---------------------------------------------------------------------------

func TestGetUserSubscriptions(t *testing.T) {
	// Create executor with minimal config (no binary/process needed for subscription tests)
	executor := &Executor{
		inbounds: make(map[string]*XrayInbound),
	}

	// Set up mock UserManager with forward ports
	um := usermanager.NewUserManager(nil)
	um.AddUserForTest("alice@example.com", []uint32{30001})
	um.AddUserForTest("bob@example.com", []uint32{30001})
	um.AddUserForTest("charlie@example.com", []uint32{30001})
	um.AddUserForTest("dave@example.com", []uint32{30001})
	um.AddUserForTest("eve@example.com", []uint32{30001})
	um.AddUserForTest("noone@example.com", []uint32{30001})
	executor.userMgr = um

	// Add VLESS inbound (WS + TLS) - with defaultClientUUID for subscription
	executor.inbounds["vless-in"] = &XrayInbound{
		tag:               "vless-in",
		protocol:          contracts.ProtocolVLess,
		port:              443,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityTLS,
		transport:         contracts.TransportWS,
		defaultClientUUID: "default-vless-uuid",
		extra: map[string]interface{}{
			"ws_path":     "/vless",
			"server_name": "example.com",
		},
		userMgr: um,
	}
	// Set up user port mappings for testing (new approach uses inbound internal mapping)
	executor.inbounds["vless-in"].SetUserPortForTest("alice@example.com", 30001)
	executor.inbounds["vless-in"].SetUserPortForTest("bob@example.com", 30001)
	executor.inbounds["vless-in"].SetUserPortForTest("charlie@example.com", 30001)
	executor.inbounds["vless-in"].SetUserPortForTest("noone@example.com", 30001)

	// Add VMess inbound (TCP + none) - with defaultClientUUID for subscription
	executor.inbounds["vmess-in"] = &XrayInbound{
		tag:               "vmess-in",
		protocol:          contracts.ProtocolVMess,
		port:              10086,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityNone,
		transport:         contracts.TransportTCP,
		defaultClientUUID: "default-vmess-uuid",
		extra:             map[string]interface{}{},
		userMgr:           um,
	}
	executor.inbounds["vmess-in"].SetUserPortForTest("alice@example.com", 30001)
	executor.inbounds["vmess-in"].SetUserPortForTest("bob@example.com", 30001)
	executor.inbounds["vmess-in"].SetUserPortForTest("charlie@example.com", 30001)
	executor.inbounds["vmess-in"].SetUserPortForTest("noone@example.com", 30001)

	// Add Trojan inbound (TCP + TLS) - with defaultPassword for subscription
	executor.inbounds["trojan-in"] = &XrayInbound{
		tag:             "trojan-in",
		protocol:        contracts.ProtocolTrojan,
		port:            8443,
		listenAddr:      "0.0.0.0",
		security:        contracts.SecurityTLS,
		transport:       contracts.TransportTCP,
		defaultPassword: "default-trojan-password",
		extra: map[string]interface{}{
			"server_name": "trojan.example.com",
		},
		userMgr: um,
	}
	executor.inbounds["trojan-in"].SetUserPortForTest("charlie@example.com", 30001)
	executor.inbounds["trojan-in"].SetUserPortForTest("bob@example.com", 30001)
	executor.inbounds["trojan-in"].SetUserPortForTest("dave@example.com", 30001)

	// Add Shadowsocks inbound - with defaultPassword for subscription
	executor.inbounds["ss-in"] = &XrayInbound{
		tag:             "ss-in",
		protocol:        contracts.ProtocolShadowsocks,
		port:            8388,
		listenAddr:      "0.0.0.0",
		security:        contracts.SecurityNone,
		transport:       contracts.TransportTCP,
		defaultPassword: "default-ss-password",
		extra: map[string]interface{}{
			"method": "aes-256-gcm",
		},
		userMgr: um,
	}
	executor.inbounds["ss-in"].SetUserPortForTest("bob@example.com", 30001)
	executor.inbounds["ss-in"].SetUserPortForTest("charlie@example.com", 30001)
	executor.inbounds["ss-in"].SetUserPortForTest("dave@example.com", 30001)
	executor.inbounds["ss-in"].SetUserPortForTest("eve@example.com", 30001)

	t.Run("user with UUID gets VLESS and VMess subs", func(t *testing.T) {
		user := contracts.UserSpec{
			Username: "alice@example.com",
			Protocol: contracts.ProtocolVLess,
			Extensions: map[string]any{
				// uuid in user.Extensions is now IGNORED for VMess/VLESS
				// UUID comes from inbound's defaultClientUUID
				"uuid": "alice-uuid-123-should-be-ignored",
				"flow": "xtls-rprx-vision",
			},
		}

		specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
			User:     user,
			Host:     "my.server.com",
			NodeName: "TestNode",
		})
		require.NoError(t, err)

		// Should get VLESS + VMess (both use UUID from inbound defaultClientUUID), skip Trojan (needs password)
		assert.Equal(t, 2, len(specs))

		tagMap := make(map[string]contracts.SubscriptionSpec)
		for _, s := range specs {
			tagMap[s.InboundTag] = s
		}

		// Verify VLESS subscription - UUID comes from inbound's defaultClientUUID, NOT from user.Extensions
		vlessSub, ok := tagMap["vless-in"]
		require.True(t, ok, "should have vless-in subscription")
		assert.Equal(t, contracts.ProtocolVLess, vlessSub.Protocol)
		assert.Equal(t, "default-vless-uuid", vlessSub.Password, "UUID should come from inbound defaultClientUUID")
		// With usermanager set and forward port bound, subscription uses forward port
		assert.Equal(t, uint32(30001), vlessSub.Port)
		assert.Equal(t, "alice@example.com", vlessSub.Username)
		assert.Contains(t, vlessSub.URI, "vless://default-vless-uuid@my.server.com:30001")
		assert.Contains(t, vlessSub.URI, "flow=xtls-rprx-vision")
		assert.Contains(t, vlessSub.URI, "path=/vless")

		// Verify VMess subscription - UUID comes from inbound's defaultClientUUID
		vmessSub, ok := tagMap["vmess-in"]
		require.True(t, ok, "should have vmess-in subscription")
		assert.Equal(t, contracts.ProtocolVMess, vmessSub.Protocol)
		assert.Equal(t, "default-vmess-uuid", vmessSub.Password, "UUID should come from inbound defaultClientUUID")
		assert.Contains(t, vmessSub.URI, "vmess://")
	})

	t.Run("user with password gets Trojan and SS subs", func(t *testing.T) {
		user := contracts.UserSpec{
			Username: "bob@example.com",
			Protocol: contracts.ProtocolTrojan,
			Extensions: map[string]any{
				"password": "bob-trojan-pass-should-be-ignored",
			},
		}

		specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
			User:     user,
			Host:     "my.server.com",
			NodeName: "TestNode",
		})
		require.NoError(t, err)

		// With new design: VMess/VLESS uuid comes from inbound defaultClientUUID,
		// Trojan/SS password comes from inbound defaultPassword
		// so now we get ALL 4 subscriptions (VLESS+VMess from inbound, Trojan+SS from inbound)
		require.Equal(t, 4, len(specs))

		tagMap := make(map[string]contracts.SubscriptionSpec)
		for _, s := range specs {
			tagMap[s.InboundTag] = s
		}

		// Verify VMess/VLESS subscriptions use inbound's defaultClientUUID
		vlessSub, ok := tagMap["vless-in"]
		require.True(t, ok)
		assert.Equal(t, "default-vless-uuid", vlessSub.Password)

		vmessSub, ok := tagMap["vmess-in"]
		require.True(t, ok)
		assert.Equal(t, "default-vmess-uuid", vmessSub.Password)

		trojanSub, ok := tagMap["trojan-in"]
		require.True(t, ok)
		assert.Equal(t, contracts.ProtocolTrojan, trojanSub.Protocol)
		// Password comes from inbound's defaultPassword, NOT from user.Extensions
		assert.Equal(t, "default-trojan-password", trojanSub.Password)
		// With usermanager set, uses forward port
		assert.Contains(t, trojanSub.URI, "trojan://default-trojan-password@my.server.com:30001")
		assert.Contains(t, trojanSub.URI, "sni=trojan.example.com")

		ssSub, ok := tagMap["ss-in"]
		require.True(t, ok)
		assert.Equal(t, contracts.ProtocolShadowsocks, ssSub.Protocol)
		assert.True(t, strings.HasPrefix(ssSub.URI, "ss://"))
		// Password comes from inbound's defaultPassword, NOT from user.Extensions
		assert.Equal(t, "default-ss-password", ssSub.Password)
	})

	t.Run("user with both UUID and password gets all subs", func(t *testing.T) {
		user := contracts.UserSpec{
			Username: "charlie@example.com",
			Protocol: contracts.ProtocolVLess,
			Extensions: map[string]any{
				"uuid":     "charlie-uuid",
				"password": "charlie-pass",
			},
		}

		specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
			User: user,
			Host: "my.server.com",
		})
		require.NoError(t, err)

		// Should get all 4 subs (VLESS, VMess, Trojan, SS)
		assert.Equal(t, 4, len(specs))

		tags := make(map[string]bool)
		for _, s := range specs {
			tags[s.InboundTag] = true
		}
		assert.True(t, tags["vless-in"])
		assert.True(t, tags["vmess-in"])
		assert.True(t, tags["trojan-in"])
		assert.True(t, tags["ss-in"])
	})

	t.Run("port override applies to all subs", func(t *testing.T) {
		user := contracts.UserSpec{
			Username: "dave@example.com",
			Extensions: map[string]any{
				"uuid":     "dave-uuid",
				"password": "dave-pass",
			},
		}

		specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
			User: user,
			Host: "my.server.com",
			Port: 9999,
		})
		require.NoError(t, err)

		for _, s := range specs {
			assert.Equal(t, uint32(9999), s.Port, "port should be overridden for %s", s.InboundTag)
		}
	})

	t.Run("default node name uses inbound tag", func(t *testing.T) {
		user := contracts.UserSpec{
			Username:   "eve@example.com",
			Extensions: map[string]any{"uuid": "eve-uuid"},
		}

		specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
			User: user,
			Host: "my.server.com",
		})
		require.NoError(t, err)

		for _, s := range specs {
			assert.Equal(t, s.InboundTag, s.NodeName, "node name should default to inbound tag")
		}
	})

	t.Run("empty username returns error", func(t *testing.T) {
		user := contracts.UserSpec{
			Extensions: map[string]any{"uuid": "test"},
		}

		_, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
			User: user,
			Host: "my.server.com",
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "username")
	})

	t.Run("empty host returns error", func(t *testing.T) {
		user := contracts.UserSpec{
			Username:   "test@example.com",
			Extensions: map[string]any{"uuid": "test"},
		}

		_, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
			User: user,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "host")
	})

	t.Run("no matching credentials returns VMess/VLESS from inbound defaultClientUUID", func(t *testing.T) {
		user := contracts.UserSpec{
			Username:   "noone@example.com",
			Extensions: map[string]any{},
		}

		specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
			User: user,
			Host: "my.server.com",
		})
		require.NoError(t, err)

		// With new design: VMess/VLESS uuid comes from inbound defaultClientUUID,
		// so even users without any extensions get VLESS + VMess subscriptions
		// (Trojan/SS require password in user.Extensions, so those are skipped)
		assert.Equal(t, 2, len(specs))

		tags := make(map[string]bool)
		for _, s := range specs {
			tags[s.InboundTag] = true
		}
		assert.True(t, tags["vless-in"])
		assert.True(t, tags["vmess-in"])
	})

	t.Run("empty executor returns empty", func(t *testing.T) {
		emptyExecutor := &Executor{
			inbounds: make(map[string]*XrayInbound),
		}

		user := contracts.UserSpec{
			Username:   "test@example.com",
			Extensions: map[string]any{"uuid": "test-uuid"},
		}

		specs, err := emptyExecutor.GetUserSubscriptions(contracts.SubscriptionRequest{
			User: user,
			Host: "my.server.com",
		})
		require.NoError(t, err)
		assert.Empty(t, specs)
	})
}

// ---------------------------------------------------------------------------
// Integration test: full subscription flow (add user → get subscriptions)
// ---------------------------------------------------------------------------

func TestSubscriptionFlow_AddUserThenGetSubs(t *testing.T) {
	executor := &Executor{
		inbounds: make(map[string]*XrayInbound),
	}

	// Set up mock UserManager with forward port for test user
	um := usermanager.NewUserManager(nil)
	um.AddUserForTest("fulluser@example.com", []uint32{30001})
	executor.userMgr = um

	// Step 1: Add inbounds with defaultClientUUID for VMess/VLESS
	executor.inbounds["vless-ws"] = &XrayInbound{
		tag:               "vless-ws",
		protocol:          contracts.ProtocolVLess,
		port:              443,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityTLS,
		transport:         contracts.TransportWS,
		defaultClientUUID: "default-vless-uuid-123",
		extra: map[string]interface{}{
			"ws_path":     "/vless",
			"ws_host":     "cdn.example.com",
			"server_name": "example.com",
		},
		userMgr: um,
	}
	executor.inbounds["trojan-tcp"] = &XrayInbound{
		tag:             "trojan-tcp",
		protocol:        contracts.ProtocolTrojan,
		port:            8443,
		listenAddr:      "0.0.0.0",
		security:        contracts.SecurityTLS,
		transport:       contracts.TransportTCP,
		defaultPassword: "super-secret-password",
		extra: map[string]interface{}{
			"server_name": "trojan.example.com",
		},
		userMgr: um,
	}
	executor.inbounds["vmess-grpc"] = &XrayInbound{
		tag:               "vmess-grpc",
		protocol:          contracts.ProtocolVMess,
		port:              50051,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityTLS,
		transport:         contracts.TransportGRPC,
		defaultClientUUID: "default-vmess-uuid-456",
		extra: map[string]interface{}{
			"grpc_service_name": "vmess-svc",
			"server_name":       "vmess.example.com",
		},
		userMgr: um,
	}
	executor.inbounds["ss-aead"] = &XrayInbound{
		tag:             "ss-aead",
		protocol:        contracts.ProtocolShadowsocks,
		port:            8388,
		listenAddr:      "0.0.0.0",
		security:        contracts.SecurityNone,
		transport:       contracts.TransportTCP,
		defaultPassword: "super-secret-password",
		extra: map[string]interface{}{
			"method": "aes-256-gcm",
		},
		userMgr: um,
	}

	// Set up user port mappings for the test user (new approach uses inbound internal mapping)
	executor.inbounds["vless-ws"].SetUserPortForTest("fulluser@example.com", 30001)
	executor.inbounds["trojan-tcp"].SetUserPortForTest("fulluser@example.com", 30001)
	executor.inbounds["vmess-grpc"].SetUserPortForTest("fulluser@example.com", 30001)
	executor.inbounds["ss-aead"].SetUserPortForTest("fulluser@example.com", 30001)

	// Step 2: Simulate user with all credentials
	// Note: uuid in user.Extensions is IGNORED for VMess/VLESS - UUID comes from inbound defaultClientUUID
	user := contracts.UserSpec{
		Username: "fulluser@example.com",
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"uuid":     "user-provided-uuid-should-be-ignored",
			"password": "super-secret-password",
			"flow":     "xtls-rprx-vision",
		},
	}

	// Step 3: Get subscriptions
	specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     user,
		Host:     "proxy.example.com",
		NodeName: "MyProxy",
	})
	require.NoError(t, err)
	require.Equal(t, 4, len(specs))

	tagMap := make(map[string]contracts.SubscriptionSpec)
	for _, s := range specs {
		tagMap[s.InboundTag] = s
	}

	// Step 4: Verify each subscription
	t.Run("VLESS-WS subscription", func(t *testing.T) {
		sub := tagMap["vless-ws"]
		assert.Equal(t, contracts.ProtocolVLess, sub.Protocol)
		assert.Equal(t, "proxy.example.com", sub.Host)
		// With usermanager set and forward port bound, subscription uses forward port
		assert.Equal(t, uint32(30001), sub.Port)
		// UUID comes from inbound's defaultClientUUID, NOT from user.Extensions
		assert.Equal(t, "default-vless-uuid-123", sub.Password)
		assert.Equal(t, "MyProxy", sub.NodeName)
		assert.Equal(t, "fulluser@example.com", sub.Username)

		// Verify URI structure
		assert.True(t, strings.HasPrefix(sub.URI, "vless://"))
		// With usermanager set, uses forward port
		assert.Contains(t, sub.URI, "default-vless-uuid-123@proxy.example.com:30001")
		assert.Contains(t, sub.URI, "type=ws")
		assert.Contains(t, sub.URI, "security=tls")
		assert.Contains(t, sub.URI, "path=/vless")
		assert.Contains(t, sub.URI, "host=cdn.example.com")
		assert.Contains(t, sub.URI, "sni=example.com")
		assert.Contains(t, sub.URI, "flow=xtls-rprx-vision")
	})

	t.Run("Trojan-TCP subscription", func(t *testing.T) {
		sub := tagMap["trojan-tcp"]
		assert.Equal(t, contracts.ProtocolTrojan, sub.Protocol)
		assert.Equal(t, "super-secret-password", sub.Password)
		// With usermanager set and forward port bound, subscription uses forward port
		assert.Equal(t, uint32(30001), sub.Port)

		assert.True(t, strings.HasPrefix(sub.URI, "trojan://"))
		// With usermanager set, uses forward port
		assert.Contains(t, sub.URI, "super-secret-password@proxy.example.com:30001")
		assert.Contains(t, sub.URI, "type=tcp")
		assert.Contains(t, sub.URI, "security=tls")
		assert.Contains(t, sub.URI, "sni=trojan.example.com")
	})

	t.Run("VMess-gRPC subscription", func(t *testing.T) {
		sub := tagMap["vmess-grpc"]
		assert.Equal(t, contracts.ProtocolVMess, sub.Protocol)
		// UUID comes from inbound's defaultClientUUID, NOT from user.Extensions
		assert.Equal(t, "default-vmess-uuid-456", sub.Password)

		assert.True(t, strings.HasPrefix(sub.URI, "vmess://"))

		// Decode and verify
		encoded := strings.TrimPrefix(sub.URI, "vmess://")
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		require.NoError(t, err)

		var cfg map[string]interface{}
		err = json.Unmarshal(decoded, &cfg)
		require.NoError(t, err)

		assert.Equal(t, "2", cfg["v"])
		assert.Equal(t, "MyProxy", cfg["ps"])
		assert.Equal(t, "proxy.example.com", cfg["add"])
		// With usermanager set and forward port bound, subscription uses forward port
		assert.Equal(t, float64(30001), cfg["port"]) // Issue #91: port as integer
		// UUID comes from inbound's defaultClientUUID
		assert.Equal(t, "default-vmess-uuid-456", cfg["id"])
		assert.Equal(t, "grpc", cfg["net"])
		assert.Equal(t, "vmess-svc", cfg["path"])
		assert.Equal(t, "vmess.example.com", cfg["sni"])
		assert.Equal(t, "auto", cfg["encryption"]) // Issue #91: encryption defaults to auto
	})

	t.Run("SS-AEAD subscription", func(t *testing.T) {
		sub := tagMap["ss-aead"]
		assert.Equal(t, contracts.ProtocolShadowsocks, sub.Protocol)
		assert.Equal(t, "super-secret-password", sub.Password)
		// With usermanager set and forward port bound, subscription uses forward port
		assert.Equal(t, uint32(30001), sub.Port)
		assert.Equal(t, "MyProxy", sub.NodeName)

		assert.True(t, strings.HasPrefix(sub.URI, "ss://"))

		// Decode the userinfo portion
		rest := strings.TrimPrefix(sub.URI, "ss://")
		atIdx := strings.Index(rest, "@")
		require.True(t, atIdx > 0)
		encodedUserinfo := rest[:atIdx]
		decodedUserinfo, err := base64.RawURLEncoding.DecodeString(encodedUserinfo)
		require.NoError(t, err)
		assert.Equal(t, "aes-256-gcm:super-secret-password", string(decodedUserinfo))

		// With usermanager set, uses forward port
		assert.Contains(t, sub.URI, "@proxy.example.com:30001")
		assert.Contains(t, sub.URI, "#MyProxy")
	})
}

// ---------------------------------------------------------------------------
// Unit test: generateURI dispatching
// ---------------------------------------------------------------------------

func TestGenerateURI_Dispatch(t *testing.T) {
	t.Run("VLESS dispatches correctly", func(t *testing.T) {
		spec := contracts.SubscriptionSpec{
			Protocol: contracts.ProtocolVLess,
			Host:     "h", Port: 1, Password: "p", NodeName: "n",
			Extensions: map[string]any{"transport": "tcp", "security": "none"},
		}
		uri, err := generateURI(spec)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(uri, "vless://"))
	})

	t.Run("VMess dispatches correctly", func(t *testing.T) {
		spec := contracts.SubscriptionSpec{
			Protocol: contracts.ProtocolVMess,
			Host:     "h", Port: 1, Password: "p", NodeName: "n",
			Extensions: map[string]any{"transport": "tcp", "security": "none"},
		}
		uri, err := generateURI(spec)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(uri, "vmess://"))
	})

	t.Run("Trojan dispatches correctly", func(t *testing.T) {
		spec := contracts.SubscriptionSpec{
			Protocol: contracts.ProtocolTrojan,
			Host:     "h", Port: 1, Password: "p", NodeName: "n",
			Extensions: map[string]any{"transport": "tcp", "security": "none"},
		}
		uri, err := generateURI(spec)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(uri, "trojan://"))
	})

	t.Run("SS dispatches correctly", func(t *testing.T) {
		spec := contracts.SubscriptionSpec{
			Protocol: contracts.ProtocolShadowsocks,
			Host:     "h", Port: 1, Password: "p", NodeName: "n",
			Extensions: map[string]any{"method": "aes-256-gcm"},
		}
		uri, err := generateURI(spec)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(uri, "ss://"))
	})

	t.Run("unknown protocol errors", func(t *testing.T) {
		spec := contracts.SubscriptionSpec{
			Protocol: "unknown",
		}
		_, err := generateURI(spec)
		assert.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// Unit tests: forward port integration
// ---------------------------------------------------------------------------

// newMockUserManagerForSubscription creates a UserManager for subscription tests.
func newMockUserManagerForSubscription() *usermanager.UserManager {
	um := usermanager.NewUserManager(nil)
	// Pre-add users with bound ports using test helper
	um.AddUserForTest("alice@example.com", []uint32{30001})
	um.AddUserForTest("bob@example.com", []uint32{})
	um.AddUserForTest("dave@example.com", []uint32{30001})
	return um
}

// TestGetUserSubscriptions_WithForwardPort tests that subscription uses forward port
// when UserManager has bound ports.
func TestGetUserSubscriptions_WithForwardPort(t *testing.T) {
	executor := &Executor{
		inbounds: make(map[string]*XrayInbound),
	}

	// Set up mock UserManager with forward port (users already pre-configured in newMockUserManagerForSubscription)
	mockUM := newMockUserManagerForSubscription()

	// Add VLESS inbound with defaultClientUUID
	executor.inbounds["vless-in"] = &XrayInbound{
		tag:               "vless-in",
		protocol:          contracts.ProtocolVLess,
		port:              443, // xray internal port
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityTLS,
		transport:         contracts.TransportWS,
		defaultClientUUID: "default-vless-uuid-for-test",
		extra: map[string]interface{}{
			"ws_path":     "/vless",
			"server_name": "example.com",
		},
		userMgr: mockUM,
	}
	// Set up user port mapping using new approach (inbound internal mapping)
	executor.inbounds["vless-in"].SetUserPortForTest("alice@example.com", 30001)
	executor.userMgr = mockUM

	// Request subscription - uuid in user.Extensions is IGNORED for VMess/VLESS
	user := contracts.UserSpec{
		Username: "alice@example.com",
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"uuid": "alice-uuid-123-should-be-ignored",
		},
	}

	specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     user,
		Host:     "my.server.com",
		NodeName: "TestNode",
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(specs))

	// Verify subscription uses forward port (30001), not internal port (443)
	assert.Equal(t, uint32(30001), specs[0].Port, "subscription should use forward port")
	// UUID should come from inbound's defaultClientUUID
	assert.Contains(t, specs[0].URI, "default-vless-uuid-for-test@my.server.com:30001")
}

// TestGetUserSubscriptions_WithoutForwardPort tests that when no forward port is bound
// in usermanager, subscription returns empty when no mapping exists (NO fallback to xray internal port).
func TestGetUserSubscriptions_WithoutForwardPort(t *testing.T) {
	executor := &Executor{
		inbounds: make(map[string]*XrayInbound),
	}

	// Add VLESS inbound
	executor.inbounds["vless-in"] = &XrayInbound{
		tag:        "vless-in",
		protocol:   contracts.ProtocolVLess,
		port:       443, // xray internal port
		listenAddr: "0.0.0.0",
		security:   contracts.SecurityTLS,
		transport:  contracts.TransportWS,
		extra: map[string]interface{}{
			"ws_path":     "/vless",
			"server_name": "example.com",
		},
	}
	// Note: no SetUserPortForTest - user is not in this inbound's mapping

	// Set up mock UserManager without any bound ports (bob has empty BindPorts pre-configured)
	mockUM := newMockUserManagerForSubscription()
	executor.userMgr = mockUM

	// Request subscription
	user := contracts.UserSpec{
		Username: "bob@example.com",
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"uuid": "bob-uuid-456",
		},
	}

	// Should return empty specs when user is not in inbound's mapping (not an error)
	specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     user,
		Host:     "my.server.com",
		NodeName: "TestNode",
	})
	require.NoError(t, err, "should not error when user not in inbound mapping - just returns empty")
	assert.Equal(t, 0, len(specs), "should return empty specs when no mapping exists")
}

// TestGetUserSubscriptions_NoUserManager tests that subscription works without usermanager
// when using inbound internal mapping (new approach).
func TestGetUserSubscriptions_NoUserManager(t *testing.T) {
	um := usermanager.NewUserManager(nil)
	executor := &Executor{
		inbounds: make(map[string]*XrayInbound),
		userMgr:  nil, // no UserManager - but we have internal mapping
	}

	// Add VLESS inbound
	executor.inbounds["vless-in"] = &XrayInbound{
		tag:               "vless-in",
		protocol:          contracts.ProtocolVLess,
		port:              443,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityTLS,
		transport:         contracts.TransportWS,
		defaultClientUUID: "test-uuid",
		extra: map[string]interface{}{
			"ws_path":     "/vless",
			"server_name": "example.com",
		},
		userMgr: um,
	}
	// Set up user port mapping using internal mapping (new approach doesn't require usermanager)
	executor.inbounds["vless-in"].SetUserPortForTest("charlie@example.com", 30001)

	// Request subscription - should work with internal mapping even without usermanager
	user := contracts.UserSpec{
		Username: "charlie@example.com",
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"uuid": "charlie-uuid-789",
		},
	}

	specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     user,
		Host:     "my.server.com",
		NodeName: "TestNode",
	})
	// Should succeed with internal mapping
	require.NoError(t, err)
	require.Equal(t, 1, len(specs))
	require.Equal(t, uint32(30001), specs[0].Port)
}

// TestGetUserSubscriptions_PortOverride tests that explicit port override
// takes precedence over forward port.
func TestGetUserSubscriptions_PortOverride(t *testing.T) {
	um := usermanager.NewUserManager(nil)
	executor := &Executor{
		inbounds: make(map[string]*XrayInbound),
	}

	// Add VLESS inbound with defaultClientUUID
	in := &XrayInbound{
		tag:               "vless-in",
		protocol:          contracts.ProtocolVLess,
		port:              443,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityTLS,
		transport:         contracts.TransportWS,
		defaultClientUUID: "default-vless-uuid",
		extra: map[string]interface{}{
			"ws_path":     "/vless",
			"server_name": "example.com",
		},
		userMgr: um,
	}
	// Set up user port mapping (this is the authoritative source now)
	in.SetUserPortForTest("dave@example.com", 30001)
	executor.inbounds["vless-in"] = in

	// Request subscription with explicit port override - uuid in user.Extensions is IGNORED
	user := contracts.UserSpec{
		Username: "dave@example.com",
		Protocol: contracts.ProtocolVLess,
		Extensions: map[string]any{
			"uuid": "dave-uuid-should-be-ignored",
		},
	}

	specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     user,
		Host:     "my.server.com",
		Port:     9999, // explicit port override
		NodeName: "TestNode",
	})
	require.NoError(t, err)
	require.Equal(t, 1, len(specs))

	// Verify explicit port takes precedence over forward port
	assert.Equal(t, uint32(9999), specs[0].Port, "explicit port should override forward port")
	// UUID should come from inbound's defaultClientUUID
	assert.Equal(t, "default-vless-uuid", specs[0].Password)
}

// TestGetUserSubscriptions_UserNotFound_SkipsWithoutError tests that user not found errors
// are skipped without interrupting the overall subscription generation.
func TestGetUserSubscriptions_UserNotFound_SkipsWithoutError(t *testing.T) {
	executor := &Executor{
		inbounds: make(map[string]*XrayInbound),
	}

	// Set up mock UserManager with forward ports
	um := usermanager.NewUserManager(nil)
	um.AddUserForTest("alice@example.com", []uint32{30001})
	executor.userMgr = um

	// Add VLESS inbound where alice exists
	executor.inbounds["vless-in"] = &XrayInbound{
		tag:               "vless-in",
		protocol:          contracts.ProtocolVLess,
		port:              443,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityTLS,
		transport:         contracts.TransportWS,
		defaultClientUUID: "vless-uuid",
		extra: map[string]interface{}{
			"ws_path":     "/vless",
			"server_name": "example.com",
		},
		userMgr: um,
	}
	executor.inbounds["vless-in"].SetUserPortForTest("alice@example.com", 30001)

	// Add VMess inbound where alice does NOT exist (user not found)
	executor.inbounds["vmess-in"] = &XrayInbound{
		tag:               "vmess-in",
		protocol:          contracts.ProtocolVMess,
		port:              10086,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityNone,
		transport:         contracts.TransportTCP,
		defaultClientUUID: "vmess-uuid",
		userMgr:           um,
	}
	// Note: vmess-in does NOT have alice in its userPorts mapping
	// This will cause GetSub to return "user not found" error

	// Request subscription for alice - should succeed despite user not found in vmess-in
	user := contracts.UserSpec{
		Username: "alice@example.com",
	}

	specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     user,
		Host:     "my.server.com",
		NodeName: "TestNode",
	})

	// Should succeed and return only the VLESS subscription (vmess-in skipped)
	require.NoError(t, err, "user not found in one inbound should not cause error")
	require.Equal(t, 1, len(specs), "should return only specs from inbounds where user exists")
	assert.Equal(t, "vless-in", specs[0].InboundTag, "should return VLESS subscription")
}

// TestGetUserSubscriptions_NonUserNotFoundError_FailFast tests that non-user-not-found errors
// are immediately returned without continuing to the next inbound.
func TestGetUserSubscriptions_NonUserNotFoundError_FailFast(t *testing.T) {
	executor := &Executor{
		inbounds: make(map[string]*XrayInbound),
	}

	// Set up mock UserManager with forward ports
	um := usermanager.NewUserManager(nil)
	um.AddUserForTest("alice@example.com", []uint32{30001})
	executor.userMgr = um

	// Add VLESS inbound with NO defaultClientUUID (this will cause credential error)
	executor.inbounds["vless-in"] = &XrayInbound{
		tag:               "vless-in",
		protocol:          contracts.ProtocolVLess,
		port:              443,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityTLS,
		transport:         contracts.TransportWS,
		defaultClientUUID: "", // Missing UUID will cause getCredentialForUser to fail
		extra: map[string]interface{}{
			"ws_path":     "/vless",
			"server_name": "example.com",
		},
		userMgr: um,
	}
	executor.inbounds["vless-in"].SetUserPortForTest("alice@example.com", 30001)

	// Add another inbound (should never be reached due to fail-fast)
	executor.inbounds["vmess-in"] = &XrayInbound{
		tag:               "vmess-in",
		protocol:          contracts.ProtocolVMess,
		port:              10086,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityNone,
		transport:         contracts.TransportTCP,
		defaultClientUUID: "vmess-uuid",
		userMgr:           um,
	}
	executor.inbounds["vmess-in"].SetUserPortForTest("alice@example.com", 30001)

	// Request subscription - should fail immediately due to missing UUID
	user := contracts.UserSpec{
		Username: "alice@example.com",
	}

	specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     user,
		Host:     "my.server.com",
		NodeName: "TestNode",
	})

	// Should fail with error about missing UUID
	require.Error(t, err, "non-user-not-found error should be returned immediately")
	assert.Nil(t, specs, "specs should be nil on error")
	assert.Contains(t, err.Error(), "vless-in", "error should include inbound tag")
	assert.Contains(t, err.Error(), "default client uuid", "error should mention missing uuid")
}

// TestGetUserSubscriptions_MixedScenario_FailFastOnRealError tests that when a user is
// skipped in one inbound but a real error occurs in a subsequent inbound, the error is returned.
func TestGetUserSubscriptions_MixedScenario_FailFastOnRealError(t *testing.T) {
	executor := &Executor{
		inbounds: make(map[string]*XrayInbound),
	}

	// Set up mock UserManager with forward ports
	um := usermanager.NewUserManager(nil)
	um.AddUserForTest("alice@example.com", []uint32{30001})
	um.AddUserForTest("bob@example.com", []uint32{30001})
	executor.userMgr = um

	// Add VMess inbound where alice exists (this will work)
	executor.inbounds["vmess-ok"] = &XrayInbound{
		tag:               "vmess-ok",
		protocol:          contracts.ProtocolVMess,
		port:              10086,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityNone,
		transport:         contracts.TransportTCP,
		defaultClientUUID: "vmess-uuid",
		userMgr:           um,
	}
	executor.inbounds["vmess-ok"].SetUserPortForTest("alice@example.com", 30001)
	// bob is NOT in vmess-ok (user not found - should be skipped)

	// Add VLESS inbound where bob exists but has no UUID (will cause error)
	executor.inbounds["vless-error"] = &XrayInbound{
		tag:               "vless-error",
		protocol:          contracts.ProtocolVLess,
		port:              443,
		listenAddr:        "0.0.0.0",
		security:          contracts.SecurityTLS,
		transport:         contracts.TransportWS,
		defaultClientUUID: "", // Missing - will cause error
		extra: map[string]interface{}{
			"ws_path":     "/vless",
			"server_name": "example.com",
		},
		userMgr: um,
	}
	executor.inbounds["vless-error"].SetUserPortForTest("bob@example.com", 30001)
	// alice is NOT in vless-error (user not found - should be skipped)

	// Request subscription for alice
	// - vmess-ok: alice exists -> should succeed
	// - vless-error: alice not found -> should be skipped
	user := contracts.UserSpec{
		Username: "alice@example.com",
	}

	specs, err := executor.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     user,
		Host:     "my.server.com",
		NodeName: "TestNode",
	})

	// Should succeed - alice is in vmess-ok, and vless-error just skips alice
	require.NoError(t, err, "alice should get vmess-ok subscription, vless-error should skip alice")
	require.Equal(t, 1, len(specs))
	assert.Equal(t, "vmess-ok", specs[0].InboundTag)

	// Now request subscription for bob
	// - vmess-ok: bob not found -> should be skipped
	// - vless-error: bob exists but UUID missing -> should error (fail-fast)
	userBob := contracts.UserSpec{
		Username: "bob@example.com",
	}

	specs, err = executor.GetUserSubscriptions(contracts.SubscriptionRequest{
		User:     userBob,
		Host:     "my.server.com",
		NodeName: "TestNode",
	})

	// Should fail - vmess-ok skips bob, vless-error returns error for bob
	require.Error(t, err, "bob should cause error in vless-error due to missing UUID")
	assert.Nil(t, specs, "specs should be nil on error")
	assert.Contains(t, err.Error(), "vless-error", "error should include inbound tag")
}

// TestXrayInbound_ForwardPort tests the XrayInbound forward port getters/setters.
func TestXrayInbound_ForwardPort(t *testing.T) {
	in := &XrayInbound{
		tag:         "test-in",
		protocol:    contracts.ProtocolVLess,
		port:        443,
		listenAddr:  "0.0.0.0",
		forwardPort: 30001,
		userEmail:   "user@example.com",
	}

	// Test getters
	assert.Equal(t, uint32(30001), in.ForwardPort())
	assert.Equal(t, "user@example.com", in.Username())

	// Test setters
	in.SetForwardPort(40001)
	in.SetUsername("newuser@example.com")

	assert.Equal(t, uint32(40001), in.ForwardPort())
	assert.Equal(t, "newuser@example.com", in.Username())
}
