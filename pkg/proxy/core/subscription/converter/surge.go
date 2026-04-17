package converter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
)

// SurgeConverter emits one proxy per line in Surge's native format.
// Supported: vmess, trojan, shadowsocks, snell, hysteria2. VLESS is skipped.
type SurgeConverter struct{}

func (c *SurgeConverter) Format() subscription.ClientFormat {
	return subscription.FormatSurge
}

func (c *SurgeConverter) Convert(specs []contracts.SubscriptionSpec) (string, error) {
	var lines []string
	for _, spec := range specs {
		line, ok := c.convertSpec(spec)
		if ok {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// convertSpec returns ("", false) for protocols/transports Surge doesn't accept.
func (c *SurgeConverter) convertSpec(spec contracts.SubscriptionSpec) (string, bool) {
	switch spec.Protocol {
	case contracts.ProtocolVLess:
		return "", false
	case contracts.ProtocolVMess:
		// Surge VMess accepts only TCP and WebSocket.
		if t := extString(spec.Extensions, "transport"); t != "" && t != "tcp" && t != "ws" {
			return "", false
		}
		return c.convertVMess(spec), true
	case contracts.ProtocolTrojan:
		// Surge Trojan accepts only TCP and WebSocket.
		if t := extString(spec.Extensions, "transport"); t != "" && t != "tcp" && t != "ws" {
			return "", false
		}
		return c.convertTrojan(spec), true
	case contracts.ProtocolShadowsocks:
		return c.convertShadowsocks(spec), true
	case contracts.ProtocolSnell:
		return c.convertSnell(spec), true
	case contracts.ProtocolHysteria2:
		return c.convertHysteria2(spec), true
	default:
		// Pre-refactor data may lack spec.Protocol but still have a hysteria2:// URI.
		if strings.HasPrefix(spec.URI, "hysteria2://") {
			return c.convertHysteria2(spec), true
		}
		return "", false
	}
}

func (c *SurgeConverter) convertVMess(spec contracts.SubscriptionSpec) string {
	name := c.nodeName(spec, "VMESS")
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

	return strings.Join(parts, ", ")
}

func (c *SurgeConverter) convertTrojan(spec contracts.SubscriptionSpec) string {
	name := c.nodeName(spec, "TROJAN")
	parts := []string{
		fmt.Sprintf("%s=trojan", name),
		spec.Host,
		strconv.FormatUint(uint64(spec.Port), 10),
		fmt.Sprintf("password=%s", spec.Password),
		"tfo=true",
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

	return strings.Join(parts, ", ")
}

func (c *SurgeConverter) convertShadowsocks(spec contracts.SubscriptionSpec) string {
	name := c.nodeName(spec, "SS")
	method := extString(spec.Extensions, "method")
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

	return strings.Join(parts, ", ")
}

func (c *SurgeConverter) convertHysteria2(spec contracts.SubscriptionSpec) string {
	name := c.nodeName(spec, "HYSTERIA2")
	parts := []string{
		fmt.Sprintf("%s=hysteria2", name),
		spec.Host,
		strconv.FormatUint(uint64(spec.Port), 10),
		fmt.Sprintf("password=%s", spec.Password),
		"download-bandwidth=1000",
		"ecn=true",
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

func (c *SurgeConverter) nodeName(spec contracts.SubscriptionSpec, protoPrefix string) string {
	if spec.NodeName != "" {
		return fmt.Sprintf("🌿 %s_%s", protoPrefix, spec.NodeName)
	}
	return fmt.Sprintf("🌿 %s_%s", protoPrefix, spec.InboundTag)
}

func init() {
	Register(&SurgeConverter{})
}
