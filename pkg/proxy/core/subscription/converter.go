package subscription

import (
	"fmt"
	"strings"
	"sync"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// Converter 定义订阅格式转换器接口。
// 不同客户端（Clash、Surge、Qv2ray 等）实现此接口。
type Converter interface {
	// Format 返回转换器支持的客户端格式。
	Format() ClientFormat

	// Convert 将 SubscriptionSpec 列表转换为客户端特定的订阅格式字符串。
	Convert(specs []contracts.SubscriptionSpec) (string, error)
}

// registry 存储已注册的 Converter 实例
var (
	registryMu sync.RWMutex
	registry   = make(map[ClientFormat]Converter)
)

// RegisterConverter 注册一个 Converter。
// 如果同名格式已存在，将覆盖。
func RegisterConverter(c Converter) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[c.Format()] = c
}

// GetConverter 根据格式获取对应的 Converter。
// 如果未找到，返回 nil 和 false。
func GetConverter(format ClientFormat) (Converter, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	c, ok := registry[format]
	return c, ok
}

// GetConverterOrDefault 根据格式获取 Converter，未找到时返回 common 格式。
func GetConverterOrDefault(format ClientFormat) Converter {
	if c, ok := GetConverter(format); ok {
		return c
	}
	// 回退到 common 格式
	if c, ok := GetConverter(FormatCommon); ok {
		return c
	}
	return nil
}

// --- 自定义配置选项类型 ---

// ProxyGroupConfig 用户配置的 proxy-group
type ProxyGroupConfig struct {
	Name        string
	Type        string // select, url-test, fallback, load-balance
	Proxies     []string
	InjectInto  string   // 原始可选值，兼容单目标写法
	InjectIntos []string // 可选：将该 group 名追加到哪些已有 proxy-group 中
	URL         string   // 仅 url-test/fallback/load-balance 需要
	Interval    int      // 仅 url-test/fallback/load-balance 需要
}

// RuleProviderConfig 用户配置的 rule-provider
type RuleProviderConfig struct {
	Name     string
	Type     string // http, file
	Behavior string // domain, ipcidr, classical
	URL      string
	Interval int
	Path     string
}

// RuleConfig 用户配置的 rule
type RuleConfig struct {
	Type   string
	Value  string
	Policy string
}

// ConvertOptions 自定义配置选项
type ConvertOptions struct {
	ProxyGroups   []ProxyGroupConfig
	RuleProviders []RuleProviderConfig
	Rules         []RuleConfig
	// UseExternalTemplate 保留外部模板 fallback 能力
	UseExternalTemplate bool
}

// --- 参数解析函数 ---

// ParseProxyGroupParam 解析 proxy_group 参数
// 格式: name:type:proxies[:inject-into-group]
func ParseProxyGroupParam(param string) (*ProxyGroupConfig, error) {
	parts := strings.SplitN(param, ":", 4)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid proxy_group param: %s (expected name:type:proxies[:inject-into-group])", param)
	}

	config := &ProxyGroupConfig{
		Name: parts[0],
		Type: parts[1],
	}

	if len(parts) >= 3 {
		// proxies 逗号分隔
		config.Proxies = strings.Split(parts[2], ",")
	}
	if len(parts) == 4 {
		config.InjectInto = parts[3]
		for _, target := range strings.Split(parts[3], ",") {
			target = strings.TrimSpace(target)
			if target != "" {
				config.InjectIntos = append(config.InjectIntos, target)
			}
		}
	}

	// url-test/fallback/load-balance 需要默认值
	switch config.Type {
	case "url-test", "fallback", "load-balance":
		config.URL = "https://cp.cloudflare.com/generate_204"
		config.Interval = 300
	}

	return config, nil
}

// ParseRuleProviderParam 解析 rule_provider 参数
// 格式: name:type:behavior:url
func ParseRuleProviderParam(param string) (*RuleProviderConfig, error) {
	parts := strings.SplitN(param, ":", 4)
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid rule_provider param: %s (expected name:type:behavior:url)", param)
	}

	return &RuleProviderConfig{
		Name:     parts[0],
		Type:     parts[1],
		Behavior: parts[2],
		URL:      parts[3],
		Interval: 86400,
		Path:     fmt.Sprintf("./rule-provider/%s.yaml", parts[0]),
	}, nil
}

// ParseRuleParam 解析 rule 参数
// 格式: type,value,policy
func ParseRuleParam(param string) (*RuleConfig, error) {
	parts := strings.Split(param, ",")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid rule param: %s (expected type,value,policy)", param)
	}

	return &RuleConfig{
		Type:   parts[0],
		Value:  parts[1],
		Policy: parts[2],
	}, nil
}
