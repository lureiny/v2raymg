package converter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
)

// SurgeConverter 实现 Surge 订阅格式转换器。
// 输出格式：Surge Proxy 格式，每行一个节点。
type SurgeConverter struct{}

// Format 返回 FormatSurge。
func (c *SurgeConverter) Format() subscription.ClientFormat {
	return subscription.FormatSurge
}

// Convert 将 SubscriptionSpec 列表转换为 Surge 格式。
// 支持协议：vmess, trojan, shadowsocks, hysteria2
// 不支持 vless（跳过）
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

// convertSpec 转换单个 SubscriptionSpec 为 Surge 格式。
// 返回 (line, supported)，supported=false 表示协议不支持。
func (c *SurgeConverter) convertSpec(spec contracts.SubscriptionSpec) (string, bool) {
	switch spec.Protocol {
	case contracts.ProtocolVLess:
		// Surge 不支持 VLESS
		return "", false
	case contracts.ProtocolVMess:
		return c.convertVMess(spec), true
	case contracts.ProtocolTrojan:
		return c.convertTrojan(spec), true
	case contracts.ProtocolShadowsocks:
		return c.convertShadowsocks(spec), true
	case contracts.ProtocolSnell:
		return c.convertSnell(spec), true
	case contracts.ProtocolHysteria2:
		return c.convertHysteria2(spec), true
	default:
		// 检查是否为 hysteria2（通过旧式 URI 前缀判断，兼容历史数据）
		if strings.HasPrefix(spec.URI, "hysteria2://") {
			return c.convertHysteria2(spec), true
		}
		return "", false
	}
}

// convertVMess 转换 VMess 为 Surge 格式。
// 格式: name=vmess, host, port, username=UUID, [sni=...,] [ws=true, ws-path=..., ws-headers=Host:"...",] [tls=true,] [vmess-aead=true]
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

// convertTrojan 转换 Trojan 为 Surge 格式。
// 格式: name=trojan, host, port, password=..., tfo=true, tls=true, [ws=true, ws-path=..., ws-headers=...,] [sni=...]
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

// convertShadowsocks 转换 Shadowsocks 为 Surge 格式。
// 格式: name=ss, host, port, encrypt-method=..., password=...
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

// convertHysteria2 转换 Hysteria2 为 Surge 格式。
// 格式: name=hysteria2, host, port, password=..., download-bandwidth=1000, ecn=true
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

	return strings.Join(parts, ", ")
}

// nodeName 生成节点名称。
func (c *SurgeConverter) nodeName(spec contracts.SubscriptionSpec, protoPrefix string) string {
	if spec.NodeName != "" {
		return fmt.Sprintf("🌿 %s_%s", protoPrefix, spec.NodeName)
	}
	return fmt.Sprintf("🌿 %s_%s", protoPrefix, spec.InboundTag)
}

// --- helpers ---

func extString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func extInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case uint32:
			return int(val)
		case float64:
			return int(val)
		}
	}
	return 0
}

func init() {
	Register(&SurgeConverter{})
}
