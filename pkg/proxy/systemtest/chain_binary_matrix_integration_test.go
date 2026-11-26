//go:build integration

package systemtest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/lureiny/v2raymg/pkg/proxy/core/chain"
)

func makeTestVMessURI(fields map[string]any) string {
	raw, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(raw)
}

func makeTestSSURI(method, password, hostPort, rawQuery, fragment string) string {
	userinfo := base64.RawURLEncoding.EncodeToString([]byte(method + ":" + password))
	uri := fmt.Sprintf("ss://%s@%s", userinfo, hostPort)
	if rawQuery != "" {
		uri += "?" + rawQuery
	}
	if fragment != "" {
		uri += "#" + fragment
	}
	return uri
}

// TestChainOutboundBinaryValidationMatrix renders every supported chain outbound
// protocol/transport through chain.ParseURI + RenderXrayOutbound / RenderMihomoProxy
// and validates the rendered configs directly with the real host xray (`xray run -test`)
// and mihomo (`mihomo -t`) binaries. It also verifies that Xray 26.2.6 removed features
// (allowInsecure, network=http/h2, security=xtls) are rejected both by RenderXrayOutbound
// and by the real xray binary itself, and that Mihomo accepts hy2 pinSHA256 with
// optional case-insensitive "sha256:" prefix stripped.
func TestChainOutboundBinaryValidationMatrix(t *testing.T) {
	xrayBin := resolveChainBinary(t, "XRAY_BIN", defaultXrayChainBin, "xray")
	mihomoBin := resolveChainBinary(t, "MIHOMO_BIN", defaultMihomoChainBin, "mihomo")
	tmpDir := t.TempDir()

	// Valid 32-byte X25519 Reality public key (base64url raw) and 16-byte SS2022 PSK (base64).
	realityPubKey := base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	ss2022Key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	const validSHA256Hex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	t.Run("Xray_SupportedMatrix_xray_run_test", func(t *testing.T) {
		cases := []struct {
			name string
			uri  string
		}{
			{
				name: "vless-tcp-tls-vision",
				uri:  "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=tls&type=tcp&sni=exit.example.com&fp=chrome&alpn=h2%2Chttp%2F1.1&flow=xtls-rprx-vision#VL-TLS",
			},
			{
				name: "vless-tcp-reality",
				uri:  fmt.Sprintf("vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=reality&type=tcp&sni=www.microsoft.com&fp=chrome&pbk=%s&sid=ab12cd34&spx=%%2F&flow=xtls-rprx-vision#VL-Real", realityPubKey),
			},
			{
				name: "vless-ws-tls",
				uri:  "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=tls&type=ws&path=%2Fws&host=cdn.example.com&sni=exit.example.com#VL-WS",
			},
			{
				name: "vless-grpc-tls",
				uri:  "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=tls&type=grpc&serviceName=tun&mode=multi&sni=exit.example.com#VL-gRPC",
			},
			{
				name: "vless-httpupgrade-tls",
				uri:  "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=tls&type=httpupgrade&path=%2Fup&host=up.example.com&sni=exit.example.com#VL-HU",
			},
			{
				name: "vless-xhttp-tls",
				uri:  "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=tls&type=xhttp&path=%2Fxh&host=xh.example.com&mode=auto&sni=exit.example.com#VL-XH",
			},
			{
				name: "vmess-tcp-tls-alpn",
				uri: makeTestVMessURI(map[string]any{
					"v": "2", "ps": "VM-TLS", "add": "exit.example.com", "port": "443",
					"id": "22222222-2222-4222-8222-222222222222", "aid": "0", "scy": "auto",
					"net": "tcp", "tls": "tls", "sni": "exit.example.com", "alpn": "h2,http/1.1",
				}),
			},
			{
				name: "vmess-ws-tls",
				uri: makeTestVMessURI(map[string]any{
					"v": "2", "ps": "VM-WS", "add": "exit.example.com", "port": "443",
					"id": "22222222-2222-4222-8222-222222222222", "aid": "0",
					"net": "ws", "path": "/vmws", "host": "ws.example.com", "tls": "tls", "sni": "exit.example.com",
				}),
			},
			{
				name: "vmess-grpc-tls",
				uri: makeTestVMessURI(map[string]any{
					"v": "2", "ps": "VM-gRPC", "add": "exit.example.com", "port": "443",
					"id": "22222222-2222-4222-8222-222222222222", "aid": "0",
					"net": "grpc", "path": "vmgrpc", "tls": "tls", "sni": "exit.example.com",
				}),
			},
			{
				name: "vmess-httpupgrade-tls-alpn",
				uri: makeTestVMessURI(map[string]any{
					"v": "2", "ps": "VM-HU", "add": "exit.example.com", "port": "443",
					"id": "22222222-2222-4222-8222-222222222222", "aid": "0",
					"net": "httpupgrade", "path": "/vmup", "host": "up.example.com",
					"tls": "tls", "sni": "exit.example.com", "alpn": "http/1.1",
				}),
			},
			{
				name: "vmess-xhttp-tls",
				uri: makeTestVMessURI(map[string]any{
					"v": "2", "ps": "VM-XH", "add": "exit.example.com", "port": "443",
					"id": "22222222-2222-4222-8222-222222222222", "aid": "0",
					"net": "xhttp", "path": "/vmxh", "host": "xh.example.com",
					"tls": "tls", "sni": "exit.example.com",
				}),
			},
			{
				name: "trojan-tcp-tls",
				uri:  "trojan://tr-secret@exit.example.com:443?security=tls&sni=exit.example.com&fp=chrome&alpn=h2%2Chttp%2F1.1#TR-TCP",
			},
			{
				name: "trojan-ws-tls",
				uri:  "trojan://tr-secret@exit.example.com:443?security=tls&type=ws&path=%2Ftrws&host=tr.example.com&sni=exit.example.com#TR-WS",
			},
			{
				name: "trojan-grpc-tls",
				uri:  "trojan://tr-secret@exit.example.com:443?security=tls&type=grpc&serviceName=trgrpc&sni=exit.example.com#TR-gRPC",
			},
			{
				name: "trojan-httpupgrade-tls",
				uri:  "trojan://tr-secret@exit.example.com:443?security=tls&type=httpupgrade&path=%2Ftrup&host=tr.example.com&sni=exit.example.com#TR-HU",
			},
			{
				name: "shadowsocks-aead",
				uri:  makeTestSSURI("aes-256-gcm", "ss-secret-pass", "exit.example.com:8388", "", "SS-AEAD"),
			},
			{
				name: "shadowsocks-2022",
				uri:  makeTestSSURI("2022-blake3-aes-128-gcm", ss2022Key, "exit.example.com:8389", "", "SS-2022"),
			},
			{
				name: "socks5-auth",
				uri:  "socks5://user1:pass1@exit.example.com:1080#S5-Auth",
			},
			{
				name: "socks5-noauth",
				uri:  "socks5://exit.example.com:1080#S5-NoAuth",
			},
			{
				name: "http-auth",
				uri:  "http://user1:pass1@exit.example.com:8080#HTTP",
			},
			{
				name: "https-tls",
				uri:  "https://user1:pass1@exit.example.com:8443?sni=exit.example.com#HTTPS",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				spec, err := chain.ParseURI(tc.uri)
				require.NoError(t, err, "ParseURI(%s)", tc.name)

				outJSON, err := chain.RenderXrayOutbound("__chain_out__:"+tc.name, spec)
				require.NoError(t, err, "RenderXrayOutbound(%s)", tc.name)

				var renderedMap map[string]any
				require.NoError(t, json.Unmarshal(outJSON, &renderedMap))
				require.Equal(t, "AsIs", renderedMap["targetStrategy"], "rendered outbound must use Xray 26+ targetStrategy=AsIs")
				require.NotContains(t, renderedMap, "domainStrategy", "rendered outbound must not emit ignored top-level domainStrategy")

				cfgDoc := map[string]any{
					"log":      map[string]any{"loglevel": "warning"},
					"inbounds": []any{},
					"outbounds": []any{
						json.RawMessage(outJSON),
						map[string]any{"tag": "direct", "protocol": "freedom"},
					},
					"routing": map[string]any{
						"rules": []any{
							map[string]any{
								"type":        "field",
								"inboundTag":  []string{"in-" + tc.name},
								"outboundTag": "__chain_out__:" + tc.name,
							},
						},
					},
				}
				cfgBytes, err := json.MarshalIndent(cfgDoc, "", "  ")
				require.NoError(t, err)

				cfgPath := filepath.Join(tmpDir, "xray-"+tc.name+".json")
				require.NoError(t, os.WriteFile(cfgPath, cfgBytes, 0o644))

				out, runErr := exec.Command(xrayBin, "run", "-test", "-config", cfgPath).CombinedOutput()
				require.NoError(t, runErr, "xray run -test failed for %s:\nconfig:\n%s\noutput:\n%s", tc.name, string(cfgBytes), string(out))
			})
		}
	})

	t.Run("Xray_RejectedRemovedFeatures", func(t *testing.T) {
		rejectURIs := []struct {
			name      string
			uri       string
			wantParts []string
		}{
			{
				name:      "vless-insecure",
				uri:       "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=tls&insecure=1",
				wantParts: []string{"allowInsecure", "mihomo"},
			},
			{
				name: "vmess-allowInsecure",
				uri: makeTestVMessURI(map[string]any{
					"v": "2", "add": "exit.example.com", "port": "443",
					"id": "22222222-2222-4222-8222-222222222222", "aid": "0",
					"net": "tcp", "tls": "tls", "allowInsecure": true,
				}),
				wantParts: []string{"allowInsecure", "mihomo"},
			},
			{
				name:      "vless-h2",
				uri:       "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=tls&type=h2&path=%2Fh2",
				wantParts: []string{"h2", "mihomo", "xhttp"},
			},
			{
				name:      "vless-xtls",
				uri:       "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=xtls",
				wantParts: []string{"xtls", "mihomo"},
			},
		}
		for _, tc := range rejectURIs {
			t.Run("render-rejects-"+tc.name, func(t *testing.T) {
				spec, err := chain.ParseURI(tc.uri)
				require.NoError(t, err)
				_, err = chain.RenderXrayOutbound("__chain_out__:"+tc.name, spec)
				require.Error(t, err)
				for _, part := range tc.wantParts {
					require.Contains(t, strings.ToLower(err.Error()), strings.ToLower(part))
				}
			})
		}

		// Confirm real Xray 26.2.6 binary itself rejects raw configs using those removed fields.
		rawInvalid := map[string]string{
			"raw-allowInsecure": `{"outbounds":[{"tag":"o","protocol":"vless","settings":{"vnext":[{"address":"exit.example.com","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}]}]},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"allowInsecure":true}}}]}`,
			"raw-network-http":  `{"outbounds":[{"tag":"o","protocol":"vless","settings":{"vnext":[{"address":"exit.example.com","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}]}]},"streamSettings":{"network":"http","security":"tls","httpSettings":{"path":"/h2"}}}]}`,
			"raw-security-xtls": `{"outbounds":[{"tag":"o","protocol":"vless","settings":{"vnext":[{"address":"exit.example.com","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}]}]},"streamSettings":{"network":"tcp","security":"xtls"}}]}`,
		}
		for name, body := range rawInvalid {
			t.Run("binary-rejects-"+name, func(t *testing.T) {
				cfgPath := filepath.Join(tmpDir, name+".json")
				require.NoError(t, os.WriteFile(cfgPath, []byte(body), 0o644))
				out, runErr := exec.Command(xrayBin, "run", "-test", "-config", cfgPath).CombinedOutput()
				require.Error(t, runErr, "expected xray binary to reject %s, output=%s", name, string(out))
			})
		}
	})

	t.Run("Mihomo_SupportedMatrix_mihomo_t", func(t *testing.T) {
		mihomoDataDir := filepath.Join(tmpDir, "mihomo-data")
		require.NoError(t, os.MkdirAll(mihomoDataDir, 0o755))

		cases := []struct {
			name string
			uri  string
		}{
			{
				name: "vless-tcp-tls-insecure",
				uri:  "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=tls&type=tcp&sni=exit.example.com&fp=chrome&insecure=1&flow=xtls-rprx-vision#VL-TLS",
			},
			{
				name: "vless-tcp-reality",
				uri:  fmt.Sprintf("vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=reality&type=tcp&sni=www.microsoft.com&fp=chrome&pbk=%s&sid=ab12cd34#VL-Real", realityPubKey),
			},
			{
				name: "vless-ws-tls",
				uri:  "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=tls&type=ws&path=%2Fws&host=ws.example.com&sni=exit.example.com#VL-WS",
			},
			{
				name: "vless-grpc-tls",
				uri:  "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=tls&type=grpc&serviceName=tun&sni=exit.example.com#VL-gRPC",
			},
			{
				name: "vless-h2-tls",
				uri:  "vless://11111111-1111-4111-8111-111111111111@exit.example.com:443?security=tls&type=h2&path=%2Fh2&host=h2.example.com&sni=exit.example.com#VL-H2",
			},
			{
				name: "vmess-tcp-tls-insecure-alpn",
				uri: makeTestVMessURI(map[string]any{
					"v": "2", "ps": "VM-TLS", "add": "exit.example.com", "port": "443",
					"id": "22222222-2222-4222-8222-222222222222", "aid": "0",
					"net": "tcp", "tls": "tls", "sni": "exit.example.com", "allowInsecure": true, "alpn": "h2,http/1.1",
				}),
			},
			{
				name: "vmess-ws-tls",
				uri: makeTestVMessURI(map[string]any{
					"v": "2", "ps": "VM-WS", "add": "exit.example.com", "port": "443",
					"id": "22222222-2222-4222-8222-222222222222", "aid": "0",
					"net": "ws", "path": "/vmws", "host": "ws.example.com", "tls": "tls", "sni": "exit.example.com",
				}),
			},
			{
				name: "vmess-h2-tls",
				uri: makeTestVMessURI(map[string]any{
					"v": "2", "ps": "VM-H2", "add": "exit.example.com", "port": "443",
					"id": "22222222-2222-4222-8222-222222222222", "aid": "0",
					"net": "h2", "path": "/vmh2", "host": "h2.example.com", "tls": "tls", "sni": "exit.example.com",
				}),
			},
			{
				name: "vmess-grpc-tls",
				uri: makeTestVMessURI(map[string]any{
					"v": "2", "ps": "VM-gRPC", "add": "exit.example.com", "port": "443",
					"id": "22222222-2222-4222-8222-222222222222", "aid": "0",
					"net": "grpc", "path": "vmgrpc", "tls": "tls", "sni": "exit.example.com",
				}),
			},
			{
				name: "trojan-tcp-tls-insecure",
				uri:  "trojan://tr-secret@exit.example.com:443?security=tls&sni=exit.example.com&insecure=1#TR-TCP",
			},
			{
				name: "trojan-ws-tls",
				uri:  "trojan://tr-secret@exit.example.com:443?security=tls&type=ws&path=%2Ftrws&host=tr.example.com&sni=exit.example.com#TR-WS",
			},
			{
				name: "trojan-grpc-tls",
				uri:  "trojan://tr-secret@exit.example.com:443?security=tls&type=grpc&serviceName=trgrpc&sni=exit.example.com#TR-gRPC",
			},
			{
				name: "shadowsocks-aead",
				uri:  makeTestSSURI("aes-256-gcm", "ss-secret-pass", "exit.example.com:8388", "", "SS-AEAD"),
			},
			{
				name: "shadowsocks-obfs",
				uri:  makeTestSSURI("aes-256-gcm", "ss-secret-pass", "exit.example.com:8388", "plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dbing.com", "SS-Obfs"),
			},
			{
				name: "shadowsocks-v2ray-plugin",
				uri:  makeTestSSURI("aes-256-gcm", "ss-secret-pass", "exit.example.com:8388", "plugin=v2ray-plugin%3Bmode%3Dwebsocket%3Btls%3Bhost%3Dws.example.com%3Bpath%3D%2Fv2", "SS-V2P"),
			},
			{
				name: "shadowsocks-shadow-tls",
				uri:  makeTestSSURI("aes-256-gcm", "ss-secret-pass", "exit.example.com:8388", "plugin=shadow-tls%3Bhost%3Dtls.example.com%3Bpassword%3Dstlspw%3Bversion%3D3", "SS-STLS"),
			},
			{
				name: "hy2-pin-sha256-lower-prefix",
				uri:  fmt.Sprintf("hy2://hy2pass@exit.example.com:443?sni=exit.example.com&pinSHA256=sha256%%3A%s#HY2-PinLower", validSHA256Hex),
			},
			{
				name: "hy2-pin-sha256-upper-prefix",
				uri:  fmt.Sprintf("hy2://hy2pass@exit.example.com:443?sni=exit.example.com&pinSHA256=SHA256%%3A%s#HY2-PinUpper", strings.ToUpper(validSHA256Hex)),
			},
			{
				name: "hy2-mport-obfs-insecure",
				uri:  "hy2://hy2pass@exit.example.com:443?sni=exit.example.com&mport=20000-40000&obfs=salamander&obfs-password=obfspw&insecure=1#HY2-Full",
			},
			{
				name: "tuic-full",
				uri:  "tuic://11111111-1111-4111-8111-111111111111:tuicpass@exit.example.com:443?sni=exit.example.com&alpn=h3&congestion_control=bbr&udp_relay_mode=native&allow_insecure=1#TUIC",
			},
			{
				name: "anytls-full",
				uri:  "anytls://anypass@exit.example.com:443?sni=exit.example.com&insecure=1&alpn=h2#AnyTLS",
			},
			{
				name: "socks5-auth",
				uri:  "socks5://user1:pass1@exit.example.com:1080#S5-Auth",
			},
			{
				name: "socks5-noauth",
				uri:  "socks5://exit.example.com:1080#S5-NoAuth",
			},
			{
				name: "http-auth",
				uri:  "http://user1:pass1@exit.example.com:8080#HTTP",
			},
			{
				name: "https-tls-insecure",
				uri:  "https://user1:pass1@exit.example.com:8443?sni=exit.example.com&insecure=1#HTTPS",
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				spec, err := chain.ParseURI(tc.uri)
				require.NoError(t, err, "ParseURI(%s)", tc.name)

				proxyMap, err := chain.RenderMihomoProxy("__chain_out__:"+tc.name, spec)
				require.NoError(t, err, "RenderMihomoProxy(%s)", tc.name)

				cfgDoc := map[string]any{
					"mode":    "rule",
					"ipv6":    true,
					"proxies": []any{proxyMap},
					"listeners": []any{
						map[string]any{
							"name":   "in-" + tc.name,
							"type":   "socks",
							"port":   18080,
							"listen": "127.0.0.1",
							"proxy":  "__chain_out__:" + tc.name,
						},
					},
					"rules": []any{
						fmt.Sprintf("IN-NAME,in-%s,__chain_out__:%s", tc.name, tc.name),
						"MATCH,DIRECT",
					},
				}
				cfgBytes, err := yaml.Marshal(cfgDoc)
				require.NoError(t, err)

				cfgPath := filepath.Join(tmpDir, "mihomo-"+tc.name+".yaml")
				require.NoError(t, os.WriteFile(cfgPath, cfgBytes, 0o644))

				out, runErr := exec.Command(mihomoBin, "-d", mihomoDataDir, "-t", "-f", cfgPath).CombinedOutput()
				require.NoError(t, runErr, "mihomo -t failed for %s:\nconfig:\n%s\noutput:\n%s", tc.name, string(cfgBytes), string(out))
			})
		}
	})
}
