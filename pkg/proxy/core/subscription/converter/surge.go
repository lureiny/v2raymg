package converter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
)

// SurgeConverter emits one proxy per line in Surge's native format.
// Supported: vmess, trojan, shadowsocks, snell, hysteria2, tuic, anytls. VLESS is skipped.
type SurgeConverter struct{}

func (c *SurgeConverter) Format() subscription.ClientFormat {
	return subscription.FormatSurge
}

func (c *SurgeConverter) Convert(specs []contracts.SubscriptionSpec) (string, error) {
	supported := make([]contracts.SubscriptionSpec, 0, len(specs))
	baseNames := make([]string, 0, len(specs))
	for _, spec := range specs {
		if !c.supportsSpec(spec) {
			continue
		}
		prefix := surgeProtoPrefix(spec)
		supported = append(supported, spec)
		baseNames = append(baseNames, c.nodeName(spec, prefix))
	}

	finalNames := deduplicateDisplayNames(baseNames)
	lines := make([]string, 0, len(supported))
	for i, spec := range supported {
		if line, ok := c.convertSpecWithName(spec, finalNames[i]); ok {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), nil
}

func surgeProtoPrefix(spec contracts.SubscriptionSpec) string {
	switch spec.Protocol {
	case contracts.ProtocolVMess:
		return "VMESS"
	case contracts.ProtocolTrojan:
		return "TROJAN"
	case contracts.ProtocolShadowsocks:
		return "SS"
	case contracts.ProtocolSnell:
		return "SNELL"
	case contracts.ProtocolHysteria2:
		return "HYSTERIA2"
	case contracts.ProtocolTUIC:
		return "TUIC"
	case contracts.ProtocolAnyTLS:
		return "ANYTLS"
	default:
		lower := strings.ToLower(strings.TrimSpace(spec.URI))
		if strings.HasPrefix(lower, "hysteria2://") || strings.HasPrefix(lower, "hy2://") {
			return "HYSTERIA2"
		}
		return ""
	}
}

func (c *SurgeConverter) supportsSpec(spec contracts.SubscriptionSpec) bool {
	switch spec.Protocol {
	case contracts.ProtocolVLess:
		return false
	case contracts.ProtocolVMess, contracts.ProtocolTrojan:
		t := extString(spec.Extensions, "transport")
		return t == "" || t == "tcp" || t == "ws"
	case contracts.ProtocolShadowsocks, contracts.ProtocolSnell, contracts.ProtocolHysteria2, contracts.ProtocolTUIC, contracts.ProtocolAnyTLS:
		return true
	default:
		lower := strings.ToLower(strings.TrimSpace(spec.URI))
		return strings.HasPrefix(lower, "hysteria2://") || strings.HasPrefix(lower, "hy2://")
	}
}

// convertSpec returns ("", false) for protocols/transports Surge doesn't accept.
func (c *SurgeConverter) convertSpec(spec contracts.SubscriptionSpec) (string, bool) {
	return c.convertSpecWithName(spec, c.nodeName(spec, surgeProtoPrefix(spec)))
}

func (c *SurgeConverter) convertSpecWithName(spec contracts.SubscriptionSpec, name string) (string, bool) {
	if !c.supportsSpec(spec) {
		return "", false
	}
	switch spec.Protocol {
	case contracts.ProtocolVMess:
		return c.convertVMessWithName(spec, name), true
	case contracts.ProtocolTrojan:
		return c.convertTrojanWithName(spec, name), true
	case contracts.ProtocolShadowsocks:
		return c.convertShadowsocksWithName(spec, name), true
	case contracts.ProtocolSnell:
		return c.convertSnellWithName(spec, name), true
	case contracts.ProtocolHysteria2:
		return c.convertHysteria2WithName(spec, name), true
	case contracts.ProtocolTUIC:
		return c.convertTuicWithName(spec, name), true
	case contracts.ProtocolAnyTLS:
		return c.convertAnyTLSWithName(spec, name), true
	default:
		lower := strings.ToLower(strings.TrimSpace(spec.URI))
		if strings.HasPrefix(lower, "hysteria2://") || strings.HasPrefix(lower, "hy2://") {
			return c.convertHysteria2WithName(spec, name), true
		}
		return "", false
	}
}

func (c *SurgeConverter) convertVMess(spec contracts.SubscriptionSpec) string {
	return c.convertVMessWithName(spec, c.nodeName(spec, "VMESS"))
}

func (c *SurgeConverter) convertVMessWithName(spec contracts.SubscriptionSpec, name string) string {
	parts := []string{
		fmt.Sprintf("%s=vmess", name),
		spec.Host,
		strconv.FormatUint(uint64(spec.Port), 10),
		fmt.Sprintf("username=%s", spec.Password),
	}

	ext := spec.Extensions

	// SNI
	if sni := extString(ext, "server_name"); sni != "" {
		parts = append(parts, fmt.Sprintf("sni=%s", sni))
	}

	// WebSocket transport
	transport := extString(ext, "transport")
	if transport == "ws" {
		parts = append(parts, "ws=true")
		if path := extString(ext, "ws_path"); path != "" {
			parts = append(parts, fmt.Sprintf("ws-path=%s", path))
		}
		if host := extString(ext, "ws_host"); host != "" {
			parts = append(parts, fmt.Sprintf("ws-headers=Host:\"%s\"", host))
		}
	}

	// TLS
	security := extString(ext, "security")
	if security == "tls" {
		parts = append(parts, "tls=true")
	}

	// VMess AEAD (alter_id = 0)
	alterID := extInt(ext, "alter_id")
	if alterID == 0 {
		parts = append(parts, "vmess-aead=true")
	}

	if skip, _ := ext["skip_cert_verify"].(bool); skip {
		parts = append(parts, "skip-cert-verify=true")
	}

	return strings.Join(parts, ", ")
}

func (c *SurgeConverter) convertTrojan(spec contracts.SubscriptionSpec) string {
	return c.convertTrojanWithName(spec, c.nodeName(spec, "TROJAN"))
}

func (c *SurgeConverter) convertTrojanWithName(spec contracts.SubscriptionSpec, name string) string {
	parts := []string{
		fmt.Sprintf("%s=trojan", name),
		spec.Host,
		strconv.FormatUint(uint64(spec.Port), 10),
		fmt.Sprintf("password=%s", spec.Password),
		"tls=true",
	}

	ext := spec.Extensions

	// WebSocket transport
	transport := extString(ext, "transport")
	if transport == "ws" {
		parts = append(parts, "ws=true")
		if path := extString(ext, "ws_path"); path != "" {
			parts = append(parts, fmt.Sprintf("ws-path=%s", path))
		}
		if host := extString(ext, "ws_host"); host != "" {
			parts = append(parts, fmt.Sprintf("ws-headers=Host:\"%s\"", host))
		}
	}

	// SNI
	if sni := extString(ext, "server_name"); sni != "" {
		parts = append(parts, fmt.Sprintf("sni=%s", sni))
	}

	if skip, _ := ext["skip_cert_verify"].(bool); skip {
		parts = append(parts, "skip-cert-verify=true")
	}

	return strings.Join(parts, ", ")
}

func (c *SurgeConverter) convertShadowsocks(spec contracts.SubscriptionSpec) string {
	return c.convertShadowsocksWithName(spec, c.nodeName(spec, "SS"))
}

func (c *SurgeConverter) convertShadowsocksWithName(spec contracts.SubscriptionSpec, name string) string {
	ext := spec.Extensions
	method := extString(ext, "method")
	if method == "" {
		method = "aes-256-gcm"
	}

	parts := []string{
		fmt.Sprintf("%s=ss", name),
		spec.Host,
		strconv.FormatUint(uint64(spec.Port), 10),
		fmt.Sprintf("encrypt-method=%s", method),
		fmt.Sprintf("password=%s", spec.Password),
	}

	// obfs-local plugin → Surge obfs parameters
	if extString(ext, "plugin") == "obfs-local" {
		if mode := extString(ext, "plugin_mode"); mode != "" {
			parts = append(parts, fmt.Sprintf("obfs=%s", mode))
		}
		if host := extString(ext, "plugin_host"); host != "" {
			parts = append(parts, fmt.Sprintf("obfs-host=%s", host))
		}
	}

	return strings.Join(parts, ", ")
}

func (c *SurgeConverter) convertHysteria2(spec contracts.SubscriptionSpec) string {
	return c.convertHysteria2WithName(spec, c.nodeName(spec, "HYSTERIA2"))
}

func (c *SurgeConverter) convertHysteria2WithName(spec contracts.SubscriptionSpec, name string) string {
	parts := []string{
		fmt.Sprintf("%s=hysteria2", name),
		spec.Host,
		strconv.FormatUint(uint64(spec.Port), 10),
		fmt.Sprintf("password=%s", spec.Password),
		"download-bandwidth=1000",
	}

	ext := spec.Extensions
	if sni := extString(ext, "server_name"); sni != "" {
		parts = append(parts, fmt.Sprintf("sni=%s", sni))
	}
	// skip-cert-verify: parsed extension bool, with legacy URI insecure=1 fallback
	// for specs constructed without going through codec.Decode.
	if skip, _ := ext["skip_cert_verify"].(bool); skip || strings.Contains(spec.URI, "insecure=1") {
		parts = append(parts, "skip-cert-verify=true")
	}
	if obfs := extString(ext, "obfs"); obfs != "" {
		parts = append(parts, fmt.Sprintf("obfs=%s", obfs))
	}
	if pwd := extString(ext, "obfs_password"); pwd != "" {
		parts = append(parts, fmt.Sprintf("obfs-password=%s", pwd))
	}
	return strings.Join(parts, ", ")
}

// convertTuic emits a Surge TUIC v5 proxy line.
// Format: name=tuic-v5, host, port[, skip-cert-verify=true][, sni=...], password=..., uuid=..., alpn=h3, version=5
func (c *SurgeConverter) convertTuic(spec contracts.SubscriptionSpec) string {
	return c.convertTuicWithName(spec, c.nodeName(spec, "TUIC"))
}

func (c *SurgeConverter) convertTuicWithName(spec contracts.SubscriptionSpec, name string) string {
	ext := spec.Extensions
	parts := []string{
		fmt.Sprintf("%s=tuic-v5", name),
		spec.Host,
		strconv.FormatUint(uint64(spec.Port), 10),
	}
	if skip, _ := ext["skip_cert_verify"].(bool); skip {
		parts = append(parts, "skip-cert-verify=true")
	}
	if sni := extString(ext, "server_name"); sni != "" {
		parts = append(parts, fmt.Sprintf("sni=%s", sni))
	}
	parts = append(parts,
		fmt.Sprintf("password=%s", spec.Password),
		fmt.Sprintf("uuid=%s", extString(ext, "uuid")),
		"alpn=h3",
	)
	return strings.Join(parts, ", ")
}

func (c *SurgeConverter) convertAnyTLS(spec contracts.SubscriptionSpec) string {
	return c.convertAnyTLSWithName(spec, c.nodeName(spec, "ANYTLS"))
}

func (c *SurgeConverter) convertAnyTLSWithName(spec contracts.SubscriptionSpec, name string) string {
	parts := []string{
		fmt.Sprintf("%s=anytls", name),
		spec.Host,
		strconv.FormatUint(uint64(spec.Port), 10),
		fmt.Sprintf("password=%s", spec.Password),
	}
	ext := spec.Extensions
	if sni := extString(ext, "server_name"); sni != "" {
		parts = append(parts, fmt.Sprintf("sni=%s", sni))
	}
	if skip, _ := ext["skip_cert_verify"].(bool); skip {
		parts = append(parts, "skip-cert-verify=true")
	}
	return strings.Join(parts, ", ")
}

func (c *SurgeConverter) nodeName(spec contracts.SubscriptionSpec, protoPrefix string) string {
	return sanitizeSurgeName(formatDisplayName(spec, protoPrefix))
}

func init() {
	Register(&SurgeConverter{})
}
