package converter

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
)

// subConverterURLs 是外部 Clash 模板服务的备用 URL 列表。
// 用一个假节点触发，拉回完整的 Clash 配置骨架（包含 proxy-groups、rules 等）。
var subConverterURLs = []string{
	"https://sub.xeton.dev/sub?target=clash&new_name=true&url=%s",
	"https://api.dler.io/sub?target=clash&new_name=true&url=%s",
	"https://sub.maoxiongnet.com/sub?target=clash&new_name=true&url=%s",
	"https://sub.id9.cc/sub?target=clash&new_name=true&url=%s",
}

const (
	clashFakeSubName = "FakeSub"
	clashFakeSub     = "ss://YWVzLTEyOC1nY206dGVzdA==@192.168.100.1:8888#" + clashFakeSubName

	clashProxiesKey     = "proxies"
	clashProxyGroupsKey = "proxy-groups"
)

// ClashConverter 实现 Clash 订阅格式转换器。
// 输出格式：完整的 Clash YAML 配置，包含 proxies + proxy-groups + rules。
// 当外部模板服务不可用时，退化为仅包含 proxies 列表的精简格式。
type ClashConverter struct{}

// Format 返回 FormatClash。
func (c *ClashConverter) Format() subscription.ClientFormat {
	return subscription.FormatClash
}

// Convert 将 SubscriptionSpec 列表转换为 Clash YAML 格式。
// 优先使用外部模板服务生成完整配置；模板不可用时退化为 proxies-only 格式。
// 支持协议：vmess, trojan, shadowsocks（vless 跳过，Clash 不支持）
func (c *ClashConverter) Convert(specs []contracts.SubscriptionSpec) (string, error) {
	return c.ConvertWithOptions(specs, nil)
}

// --- 参数解析函数 ---

// 类型别名，从 subscription 包导入
type (
	ProxyGroupConfig   = subscription.ProxyGroupConfig
	RuleProviderConfig = subscription.RuleProviderConfig
	RuleConfig         = subscription.RuleConfig
	ConvertOptions     = subscription.ConvertOptions
)

// ParseProxyGroupParam 调用 subscription.ParseProxyGroupParam
func ParseProxyGroupParam(param string) (*ProxyGroupConfig, error) {
	return subscription.ParseProxyGroupParam(param)
}

// ParseRuleProviderParam 调用 subscription.ParseRuleProviderParam
func ParseRuleProviderParam(param string) (*RuleProviderConfig, error) {
	return subscription.ParseRuleProviderParam(param)
}

// ParseRuleParam 调用 subscription.ParseRuleParam
func ParseRuleParam(param string) (*RuleConfig, error) {
	return subscription.ParseRuleParam(param)
}

// FetchProxyGroupsFromURL 调用 subscription.FetchProxyGroupsFromURL
func FetchProxyGroupsFromURL(url string) ([]ProxyGroupConfig, error) {
	return subscription.FetchProxyGroupsFromURL(url)
}

// FetchRuleProvidersFromURL 调用 subscription.FetchRuleProvidersFromURL
func FetchRuleProvidersFromURL(url string) ([]RuleProviderConfig, error) {
	return subscription.FetchRuleProvidersFromURL(url)
}

// FetchRulesFromURL 调用 subscription.FetchRulesFromURL
func FetchRulesFromURL(url string) ([]RuleConfig, error) {
	return subscription.FetchRulesFromURL(url)
}

// --- 节点匹配和 Policy 验证函数 ---

// builtinPolicies 内置策略集合
var builtinPolicies = map[string]bool{
	"DIRECT":     true,
	"REJECT":     true,
	"PASS":       true,
	"COMPATIBLE": true,
}

// MatchProxies 根据 proxies 配置匹配实际节点
// proxies 配置可能是: "all", "DIRECT", "REJECT", "group-name", "regex"
func MatchProxies(configProxies []string, nodeNames []string, definedGroups map[string]bool) []string {
	var result []string
	for _, p := range configProxies {
		switch {
		case p == "all":
			result = append(result, nodeNames...)
		case builtinPolicies[p]:
			result = append(result, p)
		case definedGroups[p]:
			result = append(result, p)
		default:
			// 正则匹配节点名
			for _, name := range nodeNames {
				if matched, _ := regexp.MatchString(p, name); matched {
					result = append(result, name)
				}
			}
		}
	}
	return result
}

// ValidatePolicy 检查 policy 是否有效
func ValidatePolicy(policy string, definedGroups map[string]bool) error {
	if builtinPolicies[policy] {
		return nil
	}
	if definedGroups[policy] {
		return nil
	}
	return fmt.Errorf("invalid policy: %s (not a defined group or builtin policy)", policy)
}

// --- ConvertWithOptions 方法 ---

// ConvertWithOptions 将 SubscriptionSpec 列表转换为 Clash YAML 格式，支持自定义配置。
// 生成顺序：
//  1. 生成 proxies 列表
//  2. 解析 proxy_groups（参数 + URL）
//  3. 生成 proxy-groups，处理节点填充和覆盖逻辑
//  4. 构建有效 policy 集合（proxy-group 名称 + 内置策略）
//  5. 解析 rule_providers（参数 + URL）
//  6. 解析 rules（参数 + URL），验证 policy 有效性
//  7. 组装最终 YAML
func (c *ClashConverter) ConvertWithOptions(specs []contracts.SubscriptionSpec, opts *ConvertOptions) (string, error) {
	// 1. 生成 proxies 列表
	var proxies []*ClashProxy
	for _, spec := range specs {
		proxy := c.convertSpec(spec)
		if proxy != nil {
			proxies = append(proxies, proxy)
		}
	}

	nodeNames := make([]string, 0, len(proxies))
	for _, p := range proxies {
		nodeNames = append(nodeNames, p.Name)
	}

	hasCustomOptions := opts != nil && (len(opts.ProxyGroups) > 0 || len(opts.RuleProviders) > 0 || len(opts.Rules) > 0)

	// 当前 Clash 输出必须走模板，避免网络/模板异常时静默 fallback 到另一套配置语义。
	nodeMap, err := fetchClashTemplate()
	if err != nil {
		return "", fmt.Errorf("fetch clash template failed: %w", err)
	}
	if err := injectProxiesToTemplate(nodeMap, proxies); err != nil {
		return "", fmt.Errorf("inject proxies to clash template failed: %w", err)
	}

	if hasCustomOptions {
		return c.patchTemplateWithOptions(nodeMap, nodeNames, opts)
	}

	data, err := yaml.Marshal(nodeMap)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// convertSpec 转换单个 SubscriptionSpec 为 ClashProxy。
// 返回 nil 表示协议不支持或节点配置不兼容 Clash。
func (c *ClashConverter) convertSpec(spec contracts.SubscriptionSpec) *ClashProxy {
	switch spec.Protocol {
	case contracts.ProtocolVLess:
		// Clash 不支持 VLESS
		return nil
	case contracts.ProtocolSnell:
		// Clash 不支持 Snell（仅 Surge 支持）
		return nil
	case contracts.ProtocolVMess:
		return c.convertVMess(spec)
	case contracts.ProtocolTrojan:
		return c.convertTrojan(spec)
	case contracts.ProtocolShadowsocks:
		return c.convertShadowsocks(spec)
	case contracts.ProtocolHysteria2:
		return c.convertHysteria2(spec)
	default:
		return nil
	}
}

// convertVMess 转换 VMess 为 Clash 格式。
func (c *ClashConverter) convertVMess(spec contracts.SubscriptionSpec) *ClashProxy {
	proxy := &ClashProxy{
		Name:    c.nodeName(spec, "VMESS"),
		Type:    "vmess",
		Server:  spec.Host,
		Port:    int(spec.Port),
		UUID:    spec.Password,
		Cipher:  "auto",
		AlterId: ptrInt(0),
		UDP:     true,
	}

	ext := spec.Extensions

	// TLS
	security := extString(ext, "security")
	if security == "tls" {
		proxy.TLS = true
	}

	// SNI / Servername
	if sni := extString(ext, "server_name"); sni != "" {
		proxy.Servername = sni
	}

	// Transport
	transport := extString(ext, "transport")
	switch transport {
	case "ws":
		proxy.Network = "ws"
		wsOpts := &WSOpts{}
		if path := extString(ext, "ws_path"); path != "" {
			wsOpts.Path = path
		}
		if host := extString(ext, "ws_host"); host != "" {
			wsOpts.Headers = &WSHeaders{Host: host}
		}
		proxy.WSOpts = wsOpts
	case "grpc":
		proxy.Network = "grpc"
		if serviceName := extString(ext, "grpc_service_name"); serviceName != "" {
			proxy.GrpcOpts = &GrpcOpts{GrpcServiceName: serviceName}
		}
	case "h2", "http":
		proxy.Network = "h2"
		h2Opts := &H2Opts{}
		if host := extString(ext, "http_host"); host != "" {
			h2Opts.Host = []string{host}
		}
		if path := extString(ext, "http_path"); path != "" {
			h2Opts.Path = path
		}
		proxy.H2Opts = h2Opts
	}

	return proxy
}

// convertTrojan 转换 Trojan 为 Clash 格式。
// 跳过 security=xtls 和 security=none 的节点（Clash 不支持）。
func (c *ClashConverter) convertTrojan(spec contracts.SubscriptionSpec) *ClashProxy {
	ext := spec.Extensions
	security := extString(ext, "security")
	// Clash 不支持 XTLS，也不支持无加密
	if security == "xtls" || security == "none" {
		return nil
	}

	proxy := &ClashProxy{
		Name:     c.nodeName(spec, "TROJAN"),
		Type:     "trojan",
		Server:   spec.Host,
		Port:     int(spec.Port),
		Password: spec.Password,
		UDP:      true,
		TLS:      true,
	}

	// SNI
	if sni := extString(ext, "server_name"); sni != "" {
		proxy.SNI = sni
	}

	// Transport
	transport := extString(ext, "transport")
	switch transport {
	case "ws":
		proxy.Network = "ws"
		wsOpts := &WSOpts{}
		if path := extString(ext, "ws_path"); path != "" {
			wsOpts.Path = path
		}
		if host := extString(ext, "ws_host"); host != "" {
			wsOpts.Headers = &WSHeaders{Host: host}
		}
		proxy.WSOpts = wsOpts
	case "grpc":
		proxy.Network = "grpc"
		if serviceName := extString(ext, "grpc_service_name"); serviceName != "" {
			proxy.GrpcOpts = &GrpcOpts{GrpcServiceName: serviceName}
		}
	}

	return proxy
}

// convertShadowsocks 转换 Shadowsocks 为 Clash 格式。
func (c *ClashConverter) convertShadowsocks(spec contracts.SubscriptionSpec) *ClashProxy {
	method := extString(spec.Extensions, "method")
	if method == "" {
		method = "aes-256-gcm"
	}

	proxy := &ClashProxy{
		Name:     c.nodeName(spec, "SS"),
		Type:     "ss",
		Server:   spec.Host,
		Port:     int(spec.Port),
		Password: spec.Password,
		Cipher:   method,
		UDP:      true,
	}

	// obfs plugin
	if plugin := extString(spec.Extensions, "plugin"); plugin != "" {
		proxy.Plugin = plugin
		if pluginOpts := buildPluginOpts(spec.Extensions); pluginOpts != nil {
			proxy.PluginOpts = pluginOpts
		}
	}

	return proxy
}

// convertHysteria2 转换 Hysteria2 为 Clash Meta 格式。
// 格式: type=hysteria2, server, port, password, sni, skip-cert-verify
func (c *ClashConverter) convertHysteria2(spec contracts.SubscriptionSpec) *ClashProxy {
	proxy := &ClashProxy{
		Name:     c.nodeName(spec, "HYSTERIA2"),
		Type:     "hysteria2",
		Server:   spec.Host,
		Port:     int(spec.Port),
		Password: spec.Password,
		UDP:      true,
	}
	// self-signed cert (insecure=1 in URI) → skip-cert-verify
	if strings.Contains(spec.URI, "insecure=1") {
		proxy.SkipCertVerify = true
	}
	return proxy
}

// nodeName 生成节点名称。
func (c *ClashConverter) nodeName(spec contracts.SubscriptionSpec, protoPrefix string) string {
	if spec.NodeName != "" {
		return fmt.Sprintf("🌿 %s_%s", protoPrefix, spec.NodeName)
	}
	return fmt.Sprintf("🌿 %s_%s", protoPrefix, spec.InboundTag)
}

// --- Clash 模板拉取 ---

// NodeMap 对应 Clash YAML 顶层 map，值保留为 yaml.Node 以便原样回写。
type NodeMap map[string]yaml.Node

// fetchClashTemplate 从外部服务拉取 Clash 配置骨架。
// 用一个假 SS 节点触发，返回包含 proxy-groups/rules 等字段的完整模板。
func fetchClashTemplate() (NodeMap, error) {
	errMsg := ""
	for _, tmpl := range subConverterURLs {
		reqURL := fmt.Sprintf(tmpl, url.QueryEscape(clashFakeSub))
		data, err := httpGet(reqURL)
		if err != nil {
			errMsg += "|" + err.Error()
			continue
		}
		data = clearClashTemplateNoise(data)
		nodeMap := NodeMap{}
		if err := yaml.Unmarshal(data, nodeMap); err != nil {
			errMsg += "|" + err.Error()
			continue
		}
		return nodeMap, nil
	}
	return nil, fmt.Errorf("all clash template sources failed: %s", errMsg)
}

// clearClashTemplateNoise 清除模板中假节点留下的痕迹，并修复已知格式问题。
func clearClashTemplateNoise(data []byte) []byte {
	rawLines := strings.Split(string(data), "\n")
	result := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if strings.Contains(line, clashFakeSubName) {
			continue
		}
		// 修复 "MATCH,,\U0001F41F 漏网之鱼,dns-failed" 格式问题
		line = strings.ReplaceAll(line, ",,", ",")
		line = strings.ReplaceAll(line, ",dns-failed", "")
		result = append(result, line)
	}
	return []byte(strings.Join(result, "\n"))
}

// injectProxiesToTemplate 把 proxies 写入模板的 proxies 字段，
// 并根据模板语义补齐/填充 Auto、Manual 等 proxy-group。
func injectProxiesToTemplate(nodeMap NodeMap, proxies []*ClashProxy) error {
	if _, ok := nodeMap[clashProxiesKey]; !ok {
		return fmt.Errorf("template missing %q key", clashProxiesKey)
	}

	// 序列化 proxies 并写回
	data, err := yaml.Marshal(proxies)
	if err != nil {
		return err
	}
	proxyNode := &yaml.Node{}
	if err := yaml.Unmarshal(data, proxyNode); err != nil {
		return err
	}
	if len(proxyNode.Content) > 0 {
		nodeMap[clashProxiesKey] = *proxyNode.Content[0]
	}

	// 按模板语义处理 proxy-groups
	ensureTemplateGroups(nodeMap, proxies)
	return nil
}

// ensureTemplateGroups 按模板语义处理 proxy-groups：
// 1. Auto/Manual 若存在且为空，则填入节点；
// 2. 若不存在 Manual，则补一个 Manual(select=all,DIRECT)；
// 3. 若是补出的 Manual，则把它加入所有其他 proxy-group。
func ensureTemplateGroups(nodeMap NodeMap, proxies []*ClashProxy) {
	proxyGroups, ok := nodeMap[clashProxyGroupsKey]
	if !ok {
		return
	}

	names := make([]string, 0, len(proxies))
	for _, p := range proxies {
		names = append(names, p.Name)
	}

	foundManual := false
	for i, n := range proxyGroups.Content {
		data, err := yaml.Marshal(n)
		if err != nil {
			continue
		}
		pg := &ProxyGroup{}
		if err := yaml.Unmarshal(data, pg); err != nil {
			continue
		}

		if pg.Name == "Manual" {
			foundManual = true
		}

		// 保持现有逻辑：空占位组或 Auto/Manual 空组时填充节点
		if len(pg.Proxies) == 0 || pg.Name == "" || ((pg.Name == "Auto" || pg.Name == "Manual") && len(pg.Proxies) == 0) {
			if pg.Name == "Manual" {
				pg.Proxies = append(append([]string{}, names...), "DIRECT")
			} else {
				pg.Proxies = append(pg.Proxies, names...)
			}
			updated, err := yaml.Marshal(pg)
			if err != nil {
				continue
			}
			node := &yaml.Node{}
			if err := yaml.Unmarshal(updated, node); err != nil {
				continue
			}
			if len(node.Content) > 0 {
				proxyGroups.Content[i] = node.Content[0]
			}
		}
	}

	if !foundManual {
		manual := ProxyGroup{
			Name:    "Manual",
			Type:    "select",
			Proxies: append(append([]string{}, names...), "DIRECT"),
		}
		manualData, err := yaml.Marshal(manual)
		if err == nil {
			node := &yaml.Node{}
			if err := yaml.Unmarshal(manualData, node); err == nil && len(node.Content) > 0 {
				proxyGroups.Content = append(proxyGroups.Content, node.Content[0])
			}
		}
		proxyGroups = appendGroupNameToAllOtherGroups(proxyGroups, "Manual")
	}

	nodeMap[clashProxyGroupsKey] = proxyGroups
}

// --- types ---

// ClashProxy 表示 Clash 配置中的一个代理节点。
type ClashProxy struct {
	Name           string      `yaml:"name"`
	Type           string      `yaml:"type"`
	Server         string      `yaml:"server"`
	Port           int         `yaml:"port"`
	Password       string      `yaml:"password,omitempty"`
	UUID           string      `yaml:"uuid,omitempty"`
	AlterId        *int        `yaml:"alterId,omitempty"`
	Cipher         string      `yaml:"cipher,omitempty"`
	Plugin         string      `yaml:"plugin,omitempty"`
	PluginOpts     *PluginOpts `yaml:"plugin-opts,omitempty"`
	UDP            bool        `yaml:"udp,omitempty"`
	TLS            bool        `yaml:"tls,omitempty"`
	SkipCertVerify bool        `yaml:"skip-cert-verify,omitempty"`
	Servername     string      `yaml:"servername,omitempty"`
	SNI            string      `yaml:"sni,omitempty"`
	Network        string      `yaml:"network,omitempty"`
	WSOpts         *WSOpts     `yaml:"ws-opts,omitempty"`
	H2Opts         *H2Opts     `yaml:"h2-opts,omitempty"`
	HttpOpts       *HttpOpts   `yaml:"http-opts,omitempty"`
	GrpcOpts       *GrpcOpts   `yaml:"grpc-opts,omitempty"`
}

// PluginOpts obfs/v2ray-plugin 选项。
type PluginOpts struct {
	Mode           string `yaml:"mode,omitempty"`
	Host           string `yaml:"host,omitempty"`
	TLS            bool   `yaml:"tls,omitempty"`
	SkipCertVerify bool   `yaml:"skip-cert-verify,omitempty"`
	Path           string `yaml:"path,omitempty"`
	MUX            bool   `yaml:"mux,omitempty"`
}

// WSOpts WebSocket 选项。
type WSOpts struct {
	Path    string     `yaml:"path,omitempty"`
	Headers *WSHeaders `yaml:"headers,omitempty"`
}

// WSHeaders WebSocket headers。
type WSHeaders struct {
	Host string `yaml:"Host"`
}

// H2Opts HTTP/2 选项。
type H2Opts struct {
	Host []string `yaml:"host,omitempty"`
	Path string   `yaml:"path,omitempty"`
}

// HttpOpts HTTP 选项。
type HttpOpts struct {
	Path   []string `yaml:"path,omitempty"`
	Method string   `yaml:"method,omitempty"`
}

// GrpcOpts gRPC 选项。
type GrpcOpts struct {
	GrpcServiceName string `yaml:"grpc-service-name"`
}

// ProxyGroup 表示 Clash proxy-group 节点。
type ProxyGroup struct {
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"`
	Proxies  []string `yaml:"proxies"`
	URL      string   `yaml:"url,omitempty"`
	Interval int      `yaml:"interval,omitempty"`
}

func (c *ClashConverter) buildLocalConfig(nodeNames []string, proxies []*ClashProxy, opts *ConvertOptions) (string, error) {
	definedGroups := make(map[string]bool)
	var proxyGroups []ProxyGroup

	if opts != nil {
		for _, pgConfig := range opts.ProxyGroups {
			definedGroups[pgConfig.Name] = true
			matchedProxies := MatchProxies(pgConfig.Proxies, nodeNames, definedGroups)
			proxyGroups = append(proxyGroups, ProxyGroup{
				Name:     pgConfig.Name,
				Type:     pgConfig.Type,
				Proxies:  matchedProxies,
				URL:      pgConfig.URL,
				Interval: pgConfig.Interval,
			})
		}
	}

	if len(proxyGroups) == 0 {
		proxyGroups = []ProxyGroup{
			{
				Name:    "Proxy",
				Type:    "select",
				Proxies: append(nodeNames, "Auto", "DIRECT"),
			},
			{
				Name:     "Auto",
				Type:     "url-test",
				Proxies:  nodeNames,
				URL:      "https://cp.cloudflare.com/generate_204",
				Interval: 300,
			},
		}
		for _, pg := range proxyGroups {
			definedGroups[pg.Name] = true
		}
	}
	proxyGroups = applyCustomGroupMembershipLocal(proxyGroups, opts.ProxyGroups)

	ruleProviders := make(map[string]interface{})
	if opts != nil {
		for _, rpConfig := range opts.RuleProviders {
			ruleProviders[rpConfig.Name] = map[string]interface{}{
				"type":     rpConfig.Type,
				"behavior": rpConfig.Behavior,
				"url":      rpConfig.URL,
				"interval": rpConfig.Interval,
				"path":     rpConfig.Path,
			}
		}
	}

	var rules []string
	if opts != nil {
		for _, ruleConfig := range opts.Rules {
			if err := ValidatePolicy(ruleConfig.Policy, definedGroups); err != nil {
				return "", fmt.Errorf("invalid rule %q: %w", ruleConfig.Type+","+ruleConfig.Value+","+ruleConfig.Policy, err)
			}
			rules = append(rules, fmt.Sprintf("%s,%s,%s", ruleConfig.Type, ruleConfig.Value, ruleConfig.Policy))
		}
	}

	result := map[string]interface{}{
		clashProxiesKey:     proxies,
		clashProxyGroupsKey: proxyGroups,
	}
	if len(ruleProviders) > 0 {
		result["rule-providers"] = ruleProviders
	}
	if len(rules) > 0 {
		result["rules"] = rules
	}

	data, err := yaml.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (c *ClashConverter) patchTemplateWithOptions(nodeMap NodeMap, nodeNames []string, opts *ConvertOptions) (string, error) {
	data, err := yaml.Marshal(nodeMap)
	if err != nil {
		return "", err
	}

	var result map[string]interface{}
	if err := yaml.Unmarshal(data, &result); err != nil {
		return "", err
	}

	definedGroups := collectDefinedGroups(result)

	for _, pgConfig := range opts.ProxyGroups {
		definedGroups[pgConfig.Name] = true
	}

	if len(opts.ProxyGroups) > 0 {
		customGroups := make([]interface{}, 0, len(opts.ProxyGroups))
		for _, pgConfig := range opts.ProxyGroups {
			customGroups = append(customGroups, map[string]interface{}{
				"name":     pgConfig.Name,
				"type":     pgConfig.Type,
				"proxies":  MatchProxies(pgConfig.Proxies, nodeNames, definedGroups),
				"url":      pgConfig.URL,
				"interval": pgConfig.Interval,
			})
		}
		result[clashProxyGroupsKey] = mergeNamedList(result[clashProxyGroupsKey], customGroups, "name")
		result[clashProxyGroupsKey] = applyCustomGroupMembership(result[clashProxyGroupsKey], opts.ProxyGroups)
	} else if len(opts.Rules) > 0 && !definedGroups["Proxy"] {
		defaultGroups := []interface{}{
			map[string]interface{}{
				"name":    "Proxy",
				"type":    "select",
				"proxies": []interface{}{"Manual", "Auto", "DIRECT"},
			},
			map[string]interface{}{
				"name":    "Manual",
				"type":    "select",
				"proxies": append(stringSliceToInterfaceSlice(nodeNames), "DIRECT"),
			},
			map[string]interface{}{
				"name":     "Auto",
				"type":     "url-test",
				"proxies":  stringSliceToInterfaceSlice(nodeNames),
				"url":      "https://cp.cloudflare.com/generate_204",
				"interval": 300,
			},
		}
		result[clashProxyGroupsKey] = mergeNamedList(result[clashProxyGroupsKey], defaultGroups, "name")
		definedGroups["Proxy"] = true
		definedGroups["Manual"] = true
		definedGroups["Auto"] = true
	}

	if len(opts.RuleProviders) > 0 {
		existing := toStringMap(result["rule-providers"])
		for _, rpConfig := range opts.RuleProviders {
			existing[rpConfig.Name] = map[string]interface{}{
				"type":     rpConfig.Type,
				"behavior": rpConfig.Behavior,
				"url":      rpConfig.URL,
				"interval": rpConfig.Interval,
				"path":     rpConfig.Path,
			}
		}
		result["rule-providers"] = existing
	}

	if len(opts.Rules) > 0 {
		customRules := make([]interface{}, 0, len(opts.Rules))
		for _, ruleConfig := range opts.Rules {
			if err := ValidatePolicy(ruleConfig.Policy, definedGroups); err != nil {
				return "", fmt.Errorf("invalid rule %q: %w", ruleConfig.Type+","+ruleConfig.Value+","+ruleConfig.Policy, err)
			}
			customRules = append(customRules, fmt.Sprintf("%s,%s,%s", ruleConfig.Type, ruleConfig.Value, ruleConfig.Policy))
		}
		result["rules"] = append(customRules, toInterfaceSlice(result["rules"])...)
	}

	patched, err := yaml.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(patched), nil
}

// --- helpers ---

func ptrInt(v int) *int {
	return &v
}

func buildPluginOpts(ext map[string]any) *PluginOpts {
	if ext == nil {
		return nil
	}
	opts := &PluginOpts{}
	if mode := extString(ext, "plugin_mode"); mode != "" {
		opts.Mode = mode
	}
	if host := extString(ext, "plugin_host"); host != "" {
		opts.Host = host
	}
	if path := extString(ext, "plugin_path"); path != "" {
		opts.Path = path
	}
	if tls, ok := ext["plugin_tls"].(bool); ok {
		opts.TLS = tls
	}
	return opts
}

func collectDefinedGroups(result map[string]interface{}) map[string]bool {
	groups := make(map[string]bool)
	for _, item := range toInterfaceSlice(result[clashProxyGroupsKey]) {
		if m, ok := item.(map[string]interface{}); ok {
			if name, ok := m["name"].(string); ok && name != "" {
				groups[name] = true
			}
		}
	}
	return groups
}

func mergeNamedList(existing interface{}, custom []interface{}, key string) []interface{} {
	items := toInterfaceSlice(existing)
	indexByName := make(map[string]int)
	for i, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			if name, ok := m[key].(string); ok && name != "" {
				indexByName[name] = i
			}
		}
	}

	for _, item := range custom {
		m, ok := item.(map[string]interface{})
		if !ok {
			items = append(items, item)
			continue
		}
		name, _ := m[key].(string)
		if idx, exists := indexByName[name]; exists {
			items[idx] = item
		} else {
			indexByName[name] = len(items)
			items = append(items, item)
		}
	}
	return items
}

func toStringMap(v interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

func buildGroupIndex(items []interface{}) map[string]int {
	indexByName := make(map[string]int)
	for i, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			if name, ok := m["name"].(string); ok && name != "" {
				indexByName[name] = i
			}
		}
	}
	return indexByName
}

func removeGroupNameFromAllGroups(items []interface{}, groupName string) []interface{} {
	for i, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == groupName {
			continue
		}
		m["proxies"] = removeProxyName(toInterfaceSlice(m["proxies"]), groupName)
		items[i] = m
	}
	return items
}

func removeGroupNameFromAllGroupsLocal(groups []ProxyGroup, groupName string) []ProxyGroup {
	for i := range groups {
		if groups[i].Name == groupName {
			continue
		}
		groups[i].Proxies = removeString(groups[i].Proxies, groupName)
	}
	return groups
}

func toInterfaceSlice(v interface{}) []interface{} {
	if v == nil {
		return []interface{}{}
	}
	if s, ok := v.([]interface{}); ok {
		return s
	}
	return []interface{}{}
}

func applyCustomGroupMembership(groups interface{}, configs []ProxyGroupConfig) []interface{} {
	items := toInterfaceSlice(groups)
	customNames := make(map[string]bool)
	for _, cfg := range configs {
		customNames[cfg.Name] = true
	}

	for _, cfg := range configs {
		indexByName := buildGroupIndex(items)
		items = removeGroupNameFromAllGroups(items, cfg.Name)
		indexByName = buildGroupIndex(items)
		targets := resolveInjectTargets(cfg, indexByName, customNames)
		for _, targetName := range targets {
			idx, ok := indexByName[targetName]
			if !ok {
				continue
			}
			m, ok := items[idx].(map[string]interface{})
			if !ok {
				continue
			}
			m["proxies"] = appendUniqueProxyName(toInterfaceSlice(m["proxies"]), cfg.Name)
			items[idx] = m
		}
	}
	return items
}

func applyCustomGroupMembershipLocal(groups []ProxyGroup, configs []ProxyGroupConfig) []ProxyGroup {
	customNames := make(map[string]bool)
	for _, cfg := range configs {
		customNames[cfg.Name] = true
	}

	for _, cfg := range configs {
		indexByName := make(map[string]int)
		for i, group := range groups {
			indexByName[group.Name] = i
		}
		groups = removeGroupNameFromAllGroupsLocal(groups, cfg.Name)
		indexByName = make(map[string]int)
		for i, group := range groups {
			indexByName[group.Name] = i
		}
		targets := resolveInjectTargets(cfg, indexByName, customNames)
		for _, targetName := range targets {
			idx, ok := indexByName[targetName]
			if !ok {
				continue
			}
			groups[idx].Proxies = appendUniqueString(groups[idx].Proxies, cfg.Name)
		}
	}
	return groups
}

func resolveInjectTargets(cfg ProxyGroupConfig, indexByName map[string]int, customNames map[string]bool) []string {
	if len(cfg.InjectIntos) > 0 {
		var targets []string
		seen := make(map[string]bool)
		for _, target := range cfg.InjectIntos {
			if _, ok := indexByName[target]; !ok {
				continue
			}
			if customNames[target] || target == cfg.Name || seen[target] {
				continue
			}
			seen[target] = true
			targets = append(targets, target)
		}
		if len(targets) > 0 {
			return targets
		}
	}
	if cfg.InjectInto != "" {
		if _, ok := indexByName[cfg.InjectInto]; ok && !customNames[cfg.InjectInto] && cfg.InjectInto != cfg.Name {
			return []string{cfg.InjectInto}
		}
	}

	var targets []string
	for name := range indexByName {
		if name == cfg.Name || customNames[name] {
			continue
		}
		targets = append(targets, name)
	}
	return targets
}

func appendUniqueProxyName(existing []interface{}, name string) []interface{} {
	for _, item := range existing {
		if s, ok := item.(string); ok && s == name {
			return existing
		}
	}
	return append(existing, name)
}

func removeProxyName(existing []interface{}, name string) []interface{} {
	result := make([]interface{}, 0, len(existing))
	for _, item := range existing {
		if s, ok := item.(string); ok && s == name {
			continue
		}
		result = append(result, item)
	}
	return result
}

func appendUniqueString(existing []string, name string) []string {
	for _, item := range existing {
		if item == name {
			return existing
		}
	}
	return append(existing, name)
}

func removeString(existing []string, name string) []string {
	result := make([]string, 0, len(existing))
	for _, item := range existing {
		if item == name {
			continue
		}
		result = append(result, item)
	}
	return result
}

func stringSliceToInterfaceSlice(items []string) []interface{} {
	result := make([]interface{}, 0, len(items))
	for _, item := range items {
		result = append(result, item)
	}
	return result
}

func appendGroupNameToAllOtherGroups(groups yaml.Node, groupName string) yaml.Node {
	for i, n := range groups.Content {
		data, err := yaml.Marshal(n)
		if err != nil {
			continue
		}
		pg := &ProxyGroup{}
		if err := yaml.Unmarshal(data, pg); err != nil {
			continue
		}
		if pg.Name == "" || pg.Name == groupName {
			continue
		}
		pg.Proxies = appendUniqueString(pg.Proxies, groupName)
		updated, err := yaml.Marshal(pg)
		if err != nil {
			continue
		}
		node := &yaml.Node{}
		if err := yaml.Unmarshal(updated, node); err != nil {
			continue
		}
		if len(node.Content) > 0 {
			groups.Content[i] = node.Content[0]
		}
	}
	return groups
}

func init() {
	Register(&ClashConverter{})
}
