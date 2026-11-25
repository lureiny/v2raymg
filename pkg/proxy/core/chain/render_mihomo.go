package chain

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// RenderMihomoProxy converts an OutboundSpec into a mihomo proxies[] dictionary
// entry named proxyName. Returns a descriptive error (suggesting xray when
// applicable) for unsupported protocols, transports, or VLESS encryption.
func RenderMihomoProxy(proxyName string, spec *OutboundSpec) (map[string]any, error) {
	if spec == nil {
		return nil, fmt.Errorf("chain: nil outbound spec")
	}
	if strings.TrimSpace(proxyName) == "" {
		return nil, fmt.Errorf("chain: empty mihomo proxy name")
	}
	if strings.TrimSpace(spec.Host) == "" {
		return nil, fmt.Errorf("chain: empty target host")
	}
	if spec.Port == 0 || spec.Port > 65535 {
		return nil, fmt.Errorf("chain: port %d out of range [1, 65535]", spec.Port)
	}

	switch spec.Protocol {
	case contracts.ProtocolVLess:
		return renderMihomoVLESS(proxyName, spec)
	case contracts.ProtocolVMess:
		return renderMihomoVMess(proxyName, spec)
	case contracts.ProtocolTrojan:
		return renderMihomoTrojan(proxyName, spec)
	case contracts.ProtocolShadowsocks:
		return renderMihomoSS(proxyName, spec)
	case contracts.ProtocolHysteria2:
		return renderMihomoHysteria2(proxyName, spec)
	case contracts.ProtocolTUIC:
		return renderMihomoTUIC(proxyName, spec)
	case contracts.ProtocolAnyTLS:
		return renderMihomoAnyTLS(proxyName, spec)
	case contracts.ProtocolSOCKS5:
		return renderMihomoSOCKS5(proxyName, spec)
	case contracts.ProtocolHTTP:
		return renderMihomoHTTP(proxyName, spec)
	default:
		return nil, fmt.Errorf("mihomo does not support chain outbound protocol %q", spec.Protocol)
	}
}

func renderMihomoVLESS(name string, spec *OutboundSpec) (map[string]any, error) {
	if spec.UUID == "" {
		return nil, fmt.Errorf("chain: vless requires uuid")
	}
	if spec.Encryption != "" && spec.Encryption != "none" {
		return nil, fmt.Errorf("mihomo does not support VLESS encryption %q; use xray container instead", spec.Encryption)
	}
	if spec.Security == "xtls" {
		return nil, fmt.Errorf("mihomo does not support VLESS xtls security; use xray container instead")
	}

	m := map[string]any{
		"name":   name,
		"type":   "vless",
		"server": spec.Host,
		"port":   int(spec.Port),
		"uuid":   spec.UUID,
		"udp":    true,
	}
	if spec.Flow != "" {
		m["flow"] = spec.Flow
	}
	if err := applyMihomoSecurity(m, spec, "servername"); err != nil {
		return nil, err
	}
	if err := applyMihomoTransport(m, spec); err != nil {
		return nil, err
	}
	return m, nil
}

func renderMihomoVMess(name string, spec *OutboundSpec) (map[string]any, error) {
	if spec.UUID == "" {
		return nil, fmt.Errorf("chain: vmess requires uuid")
	}
	if spec.Security == "xtls" {
		return nil, fmt.Errorf("mihomo does not support VMess xtls security; use xray container instead")
	}

	cipher := spec.Method
	if cipher == "" {
		cipher = "auto"
	}

	m := map[string]any{
		"name":    name,
		"type":    "vmess",
		"server":  spec.Host,
		"port":    int(spec.Port),
		"uuid":    spec.UUID,
		"alterId": spec.AlterID,
		"cipher":  cipher,
		"udp":     true,
	}
	if err := applyMihomoSecurity(m, spec, "servername"); err != nil {
		return nil, err
	}
	if err := applyMihomoTransport(m, spec); err != nil {
		return nil, err
	}
	return m, nil
}

func renderMihomoTrojan(name string, spec *OutboundSpec) (map[string]any, error) {
	if spec.Password == "" {
		return nil, fmt.Errorf("chain: trojan requires password")
	}

	m := map[string]any{
		"name":     name,
		"type":     "trojan",
		"server":   spec.Host,
		"port":     int(spec.Port),
		"password": spec.Password,
		"udp":      true,
	}
	if spec.Flow != "" {
		m["flow"] = spec.Flow
	}
	if err := applyMihomoSecurity(m, spec, "sni"); err != nil {
		return nil, err
	}
	if err := applyMihomoTransport(m, spec); err != nil {
		return nil, err
	}
	return m, nil
}

func applyMihomoSecurity(m map[string]any, spec *OutboundSpec, sniKey string) error {
	switch spec.Security {
	case "", "none":
		if spec.Protocol == contracts.ProtocolTrojan {
			m["tls"] = true
		}
	case "tls", "xtls":
		m["tls"] = true
		if spec.SNI != "" {
			m[sniKey] = spec.SNI
		}
		if spec.SkipCertVerify {
			m["skip-cert-verify"] = true
		}
		if spec.Fingerprint != "" {
			m["client-fingerprint"] = spec.Fingerprint
		}
		if len(spec.ALPN) > 0 {
			m["alpn"] = spec.ALPN
		}
	case "reality":
		if spec.RealityPublicKey == "" {
			return fmt.Errorf("chain: reality requires public key")
		}
		m["tls"] = true
		if spec.SNI != "" {
			m[sniKey] = spec.SNI
		}
		if spec.SkipCertVerify {
			m["skip-cert-verify"] = true
		}
		fp := spec.Fingerprint
		if fp == "" {
			fp = "chrome"
		}
		m["client-fingerprint"] = fp
		ro := map[string]any{
			"public-key": spec.RealityPublicKey,
		}
		if spec.RealityShortID != "" {
			ro["short-id"] = spec.RealityShortID
		}
		m["reality-opts"] = ro
		if len(spec.ALPN) > 0 {
			m["alpn"] = spec.ALPN
		}
	default:
		return fmt.Errorf("mihomo does not support security %q for %s", spec.Security, spec.Protocol)
	}
	return nil
}

func applyMihomoTransport(m map[string]any, spec *OutboundSpec) error {
	t := strings.ToLower(spec.Transport)
	switch t {
	case "", "tcp":
		return nil
	case "ws":
		m["network"] = "ws"
		wsOpts := map[string]any{}
		if spec.WSPath != "" {
			wsOpts["path"] = spec.WSPath
		}
		if spec.WSHost != "" {
			wsOpts["headers"] = map[string]any{"Host": spec.WSHost}
		}
		if len(wsOpts) > 0 {
			m["ws-opts"] = wsOpts
		}
		return nil
	case "grpc":
		m["network"] = "grpc"
		if spec.GRPCServiceName != "" {
			m["grpc-opts"] = map[string]any{
				"grpc-service-name": spec.GRPCServiceName,
			}
		}
		return nil
	case "h2", "http":
		m["network"] = "h2"
		h2Opts := map[string]any{}
		if spec.H2Path != "" {
			h2Opts["path"] = spec.H2Path
		}
		if spec.H2Host != "" {
			h2Opts["host"] = splitCommaList(spec.H2Host)
		}
		if len(h2Opts) > 0 {
			m["h2-opts"] = h2Opts
		}
		return nil
	case "httpupgrade", "xhttp", "splithttp":
		return fmt.Errorf("mihomo does not support %s transport for %s; use xray container instead", t, spec.Protocol)
	default:
		return fmt.Errorf("mihomo does not support %s transport for %s; use xray container instead", t, spec.Protocol)
	}
}

func splitCommaList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func renderMihomoSS(name string, spec *OutboundSpec) (map[string]any, error) {
	if spec.Method == "" {
		return nil, fmt.Errorf("chain: shadowsocks requires cipher method")
	}
	if spec.Password == "" {
		return nil, fmt.Errorf("chain: shadowsocks requires password")
	}

	m := map[string]any{
		"name":     name,
		"type":     "ss",
		"server":   spec.Host,
		"port":     int(spec.Port),
		"cipher":   spec.Method,
		"password": spec.Password,
		"udp":      true,
	}

	if spec.Plugin != "" {
		plugin, opts, err := normalizeMihomoSSPlugin(spec.Plugin, spec.PluginOpts)
		if err != nil {
			return nil, err
		}
		m["plugin"] = plugin
		if len(opts) > 0 {
			m["plugin-opts"] = opts
		}
	}
	return m, nil
}

func normalizeMihomoSSPlugin(plugin string, rawOpts map[string]string) (string, map[string]any, error) {
	switch plugin {
	case "obfs-local", "simple-obfs", "obfs":
		opts := map[string]any{}
		if mode := firstMapVal(rawOpts, "mode", "obfs"); mode != "" {
			opts["mode"] = mode
		}
		if host := firstMapVal(rawOpts, "host", "obfs-host"); host != "" {
			opts["host"] = host
		}
		return "obfs", opts, nil

	case "v2ray-plugin":
		opts := map[string]any{}
		if mode := rawOpts["mode"]; mode != "" {
			opts["mode"] = mode
		}
		if host := rawOpts["host"]; host != "" {
			opts["host"] = host
		}
		if path := rawOpts["path"]; path != "" {
			opts["path"] = path
		}
		if v, ok := rawOpts["tls"]; ok && (v == "" || strings.EqualFold(v, "true") || v == "1") {
			opts["tls"] = true
		}
		if v := firstMapVal(rawOpts, "skip-cert-verify", "insecure"); strings.EqualFold(v, "true") || v == "1" {
			opts["skip-cert-verify"] = true
		}
		if v, ok := rawOpts["mux"]; ok && (v == "" || strings.EqualFold(v, "true") || v == "1") {
			opts["mux"] = true
		}
		return "v2ray-plugin", opts, nil

	case "shadow-tls":
		opts := map[string]any{}
		if host := rawOpts["host"]; host != "" {
			opts["host"] = host
		}
		if pwd := firstMapVal(rawOpts, "password", "passwd"); pwd != "" {
			opts["password"] = pwd
		}
		if verStr := rawOpts["version"]; verStr != "" {
			ver, err := strconv.Atoi(verStr)
			if err != nil {
				return "", nil, fmt.Errorf("chain: invalid shadow-tls version %q: %w", verStr, err)
			}
			opts["version"] = ver
		}
		return "shadow-tls", opts, nil

	default:
		opts := make(map[string]any, len(rawOpts))
		for k, v := range rawOpts {
			opts[k] = v
		}
		return plugin, opts, nil
	}
}

func firstMapVal(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

func renderMihomoHysteria2(name string, spec *OutboundSpec) (map[string]any, error) {
	if spec.Password == "" {
		return nil, fmt.Errorf("chain: hysteria2 requires password")
	}

	m := map[string]any{
		"name":     name,
		"type":     "hysteria2",
		"server":   spec.Host,
		"port":     int(spec.Port),
		"password": spec.Password,
		"udp":      true,
	}
	if spec.Ports != "" {
		m["ports"] = spec.Ports
	}
	if spec.SNI != "" {
		m["sni"] = spec.SNI
	}
	if spec.SkipCertVerify {
		m["skip-cert-verify"] = true
	}
	if len(spec.ALPN) > 0 {
		m["alpn"] = spec.ALPN
	}
	if spec.Obfs != "" {
		m["obfs"] = spec.Obfs
	}
	if spec.ObfsPassword != "" {
		m["obfs-password"] = spec.ObfsPassword
	}
	if spec.PinSHA256 != "" {
		m["fingerprint"] = spec.PinSHA256
	}
	return m, nil
}

func renderMihomoTUIC(name string, spec *OutboundSpec) (map[string]any, error) {
	if spec.UUID == "" || spec.Password == "" {
		return nil, fmt.Errorf("chain: tuic requires uuid and password")
	}

	m := map[string]any{
		"name":     name,
		"type":     "tuic",
		"server":   spec.Host,
		"port":     int(spec.Port),
		"uuid":     spec.UUID,
		"password": spec.Password,
		"udp":      true,
	}
	if spec.SNI != "" {
		m["sni"] = spec.SNI
	}
	if spec.SkipCertVerify {
		m["skip-cert-verify"] = true
	}
	if len(spec.ALPN) > 0 {
		m["alpn"] = spec.ALPN
	}
	if spec.CongestionControl != "" {
		m["congestion-controller"] = spec.CongestionControl
	}
	if spec.UDPRelayMode != "" {
		m["udp-relay-mode"] = spec.UDPRelayMode
	}
	if spec.DisableSNI {
		m["disable-sni"] = true
	}
	return m, nil
}

func renderMihomoAnyTLS(name string, spec *OutboundSpec) (map[string]any, error) {
	if spec.Password == "" {
		return nil, fmt.Errorf("chain: anytls requires password")
	}

	m := map[string]any{
		"name":     name,
		"type":     "anytls",
		"server":   spec.Host,
		"port":     int(spec.Port),
		"password": spec.Password,
		"udp":      true,
	}
	if spec.SNI != "" {
		m["sni"] = spec.SNI
	}
	if spec.SkipCertVerify {
		m["skip-cert-verify"] = true
	}
	if spec.Fingerprint != "" {
		m["fingerprint"] = spec.Fingerprint
	}
	if len(spec.ALPN) > 0 {
		m["alpn"] = spec.ALPN
	}
	return m, nil
}

func renderMihomoSOCKS5(name string, spec *OutboundSpec) (map[string]any, error) {
	m := map[string]any{
		"name":   name,
		"type":   "socks5",
		"server": spec.Host,
		"port":   int(spec.Port),
		"udp":    true,
	}
	if spec.Username != "" {
		m["username"] = spec.Username
	}
	if spec.Password != "" {
		m["password"] = spec.Password
	}
	return m, nil
}

func renderMihomoHTTP(name string, spec *OutboundSpec) (map[string]any, error) {
	m := map[string]any{
		"name":   name,
		"type":   "http",
		"server": spec.Host,
		"port":   int(spec.Port),
	}
	if spec.Username != "" {
		m["username"] = spec.Username
	}
	if spec.Password != "" {
		m["password"] = spec.Password
	}
	if spec.TLS || spec.Security == "tls" {
		m["tls"] = true
		if spec.SNI != "" {
			m["sni"] = spec.SNI
		}
		if spec.SkipCertVerify {
			m["skip-cert-verify"] = true
		}
	}
	return m, nil
}
