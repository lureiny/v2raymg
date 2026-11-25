package chain

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// RenderXrayOutbound renders a single Xray outbound JSON object (ready for
// wrapping in `{"outbounds":[...]}` when fed to `xray api ado`) with
// `domainStrategy: "AsIs"` so DDNS hostnames are not pinned or leaked locally.
//
// Returns a descriptive error suggesting `mihomo` for protocols Xray does not
// support natively (hysteria2, tuic, anytls) or Shadowsocks SIP003 plugins.
func RenderXrayOutbound(outboundTag string, spec *OutboundSpec) ([]byte, error) {
	if spec == nil {
		return nil, fmt.Errorf("chain: nil outbound spec")
	}
	if strings.TrimSpace(outboundTag) == "" {
		return nil, fmt.Errorf("chain: empty xray outbound tag")
	}
	if strings.TrimSpace(spec.Host) == "" {
		return nil, fmt.Errorf("chain: empty target host")
	}
	if spec.Port == 0 || spec.Port > 65535 {
		return nil, fmt.Errorf("chain: port %d out of range [1, 65535]", spec.Port)
	}

	var protocol string
	var settings map[string]any
	var includeStream bool

	switch spec.Protocol {
	case contracts.ProtocolVLess:
		if spec.UUID == "" {
			return nil, fmt.Errorf("chain: vless requires uuid")
		}
		protocol = "vless"
		enc := spec.Encryption
		if enc == "" {
			enc = "none"
		}
		user := map[string]any{
			"id":         spec.UUID,
			"encryption": enc,
		}
		if spec.Flow != "" {
			user["flow"] = spec.Flow
		}
		settings = map[string]any{
			"vnext": []map[string]any{
				{
					"address": spec.Host,
					"port":    int(spec.Port),
					"users":   []map[string]any{user},
				},
			},
		}
		includeStream = true

	case contracts.ProtocolVMess:
		if spec.UUID == "" {
			return nil, fmt.Errorf("chain: vmess requires uuid")
		}
		protocol = "vmess"
		cipher := spec.Method
		if cipher == "" {
			cipher = "auto"
		}
		settings = map[string]any{
			"vnext": []map[string]any{
				{
					"address": spec.Host,
					"port":    int(spec.Port),
					"users": []map[string]any{
						{
							"id":       spec.UUID,
							"security": cipher,
							"alterId":  spec.AlterID,
						},
					},
				},
			},
		}
		includeStream = true

	case contracts.ProtocolTrojan:
		if spec.Password == "" {
			return nil, fmt.Errorf("chain: trojan requires password")
		}
		protocol = "trojan"
		settings = map[string]any{
			"servers": []map[string]any{
				{
					"address":  spec.Host,
					"port":     int(spec.Port),
					"password": spec.Password,
				},
			},
		}
		includeStream = true

	case contracts.ProtocolShadowsocks:
		if spec.Plugin != "" {
			return nil, fmt.Errorf("xray shadowsocks outbound does not support external SIP003 plugin %q; use mihomo container instead", spec.Plugin)
		}
		if spec.Method == "" {
			return nil, fmt.Errorf("chain: shadowsocks requires cipher method")
		}
		if spec.Password == "" {
			return nil, fmt.Errorf("chain: shadowsocks requires password")
		}
		protocol = "shadowsocks"
		settings = map[string]any{
			"servers": []map[string]any{
				{
					"address":  spec.Host,
					"port":     int(spec.Port),
					"method":   spec.Method,
					"password": spec.Password,
				},
			},
		}

	case contracts.ProtocolSOCKS5:
		protocol = "socks"
		srv := map[string]any{
			"address": spec.Host,
			"port":    int(spec.Port),
		}
		if spec.Username != "" || spec.Password != "" {
			srv["users"] = []map[string]any{
				{
					"user": spec.Username,
					"pass": spec.Password,
				},
			}
		}
		settings = map[string]any{
			"servers": []map[string]any{srv},
		}

	case contracts.ProtocolHTTP:
		protocol = "http"
		srv := map[string]any{
			"address": spec.Host,
			"port":    int(spec.Port),
		}
		if spec.Username != "" || spec.Password != "" {
			srv["users"] = []map[string]any{
				{
					"user": spec.Username,
					"pass": spec.Password,
				},
			}
		}
		settings = map[string]any{
			"servers": []map[string]any{srv},
		}
		if spec.TLS || spec.Security == "tls" {
			includeStream = true
		}

	case contracts.ProtocolHysteria2, contracts.ProtocolTUIC, contracts.ProtocolAnyTLS:
		return nil, fmt.Errorf("xray does not support %s outbound; use mihomo container instead", spec.Protocol)

	default:
		return nil, fmt.Errorf("xray does not support chain outbound protocol %q", spec.Protocol)
	}

	outbound := map[string]any{
		"tag":            outboundTag,
		"protocol":       protocol,
		"settings":       settings,
		"domainStrategy": "AsIs",
	}

	if includeStream {
		stream, err := buildXrayStreamSettings(spec)
		if err != nil {
			return nil, err
		}
		if len(stream) > 0 {
			outbound["streamSettings"] = stream
		}
	}

	data, err := json.Marshal(outbound)
	if err != nil {
		return nil, fmt.Errorf("chain: marshal xray outbound: %w", err)
	}
	return data, nil
}

func buildXrayStreamSettings(spec *OutboundSpec) (map[string]any, error) {
	transport := strings.ToLower(spec.Transport)
	if transport == "" {
		transport = "tcp"
	}
	security := strings.ToLower(spec.Security)
	if security == "none" {
		security = ""
	}
	if spec.Protocol == contracts.ProtocolTrojan && security == "" {
		security = "tls"
	}
	if spec.TLS && security == "" {
		security = "tls"
	}

	hasHeader := spec.HeaderType != "" && transport == "tcp"
	if transport == "tcp" && security == "" && !hasHeader {
		return nil, nil
	}

	stream := map[string]any{}

	switch transport {
	case "tcp":
		stream["network"] = "tcp"
		if hasHeader {
			stream["tcpSettings"] = map[string]any{
				"header": map[string]any{
					"type": spec.HeaderType,
				},
			}
		}
	case "ws":
		stream["network"] = "ws"
		ws := map[string]any{}
		if spec.WSPath != "" {
			ws["path"] = spec.WSPath
		}
		if spec.WSHost != "" {
			ws["headers"] = map[string]any{"Host": spec.WSHost}
		}
		if len(ws) > 0 {
			stream["wsSettings"] = ws
		}
	case "grpc":
		stream["network"] = "grpc"
		grpc := map[string]any{}
		if spec.GRPCServiceName != "" {
			grpc["serviceName"] = spec.GRPCServiceName
		}
		if len(grpc) > 0 {
			stream["grpcSettings"] = grpc
		}
	case "h2", "http":
		stream["network"] = "http"
		httpSettings := map[string]any{}
		if spec.H2Path != "" {
			httpSettings["path"] = spec.H2Path
		}
		if spec.H2Host != "" {
			httpSettings["host"] = splitCommaList(spec.H2Host)
		}
		if len(httpSettings) > 0 {
			stream["httpSettings"] = httpSettings
		}
	case "httpupgrade":
		stream["network"] = "httpupgrade"
		hu := map[string]any{}
		if spec.HTTPUpgradePath != "" {
			hu["path"] = spec.HTTPUpgradePath
		}
		if spec.HTTPUpgradeHost != "" {
			hu["host"] = spec.HTTPUpgradeHost
		}
		if len(hu) > 0 {
			stream["httpupgradeSettings"] = hu
		}
	case "xhttp", "splithttp":
		stream["network"] = "xhttp"
		sh := map[string]any{}
		if spec.XHTTPMode != "" {
			sh["mode"] = spec.XHTTPMode
		}
		if spec.XHTTPPath != "" {
			sh["path"] = spec.XHTTPPath
		}
		if spec.XHTTPHost != "" {
			sh["host"] = spec.XHTTPHost
		}
		if len(sh) > 0 {
			stream["splithttpSettings"] = sh
		}
	default:
		return nil, fmt.Errorf("xray does not support transport %q", spec.Transport)
	}

	switch security {
	case "":
	case "tls":
		stream["security"] = "tls"
		tlsSettings := map[string]any{}
		if spec.SNI != "" {
			tlsSettings["serverName"] = spec.SNI
		}
		if spec.SkipCertVerify {
			tlsSettings["allowInsecure"] = true
		}
		if len(spec.ALPN) > 0 {
			tlsSettings["alpn"] = spec.ALPN
		}
		if spec.Fingerprint != "" {
			tlsSettings["fingerprint"] = spec.Fingerprint
		}
		if len(tlsSettings) > 0 {
			stream["tlsSettings"] = tlsSettings
		}
	case "reality":
		if spec.RealityPublicKey == "" {
			return nil, fmt.Errorf("chain: reality requires public key")
		}
		stream["security"] = "reality"
		fp := spec.Fingerprint
		if fp == "" {
			fp = "chrome"
		}
		rs := map[string]any{
			"publicKey":   spec.RealityPublicKey,
			"fingerprint": fp,
		}
		if spec.SNI != "" {
			rs["serverName"] = spec.SNI
		}
		if spec.RealityShortID != "" {
			rs["shortId"] = spec.RealityShortID
		}
		if spec.RealitySpiderX != "" {
			rs["spiderX"] = spec.RealitySpiderX
		}
		stream["realitySettings"] = rs
	case "xtls":
		stream["security"] = "xtls"
		xtlsSettings := map[string]any{}
		if spec.SNI != "" {
			xtlsSettings["serverName"] = spec.SNI
		}
		if spec.SkipCertVerify {
			xtlsSettings["allowInsecure"] = true
		}
		if len(xtlsSettings) > 0 {
			stream["xtlsSettings"] = xtlsSettings
		}
	default:
		return nil, fmt.Errorf("xray does not support security %q", spec.Security)
	}

	return stream, nil
}
