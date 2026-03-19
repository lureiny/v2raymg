package converter

import (
	"fmt"
	"net/url"
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
	var proxies []*ClashProxy
	for _, spec := range specs {
		proxy := c.convertSpec(spec)
		if proxy != nil {
			proxies = append(proxies, proxy)
		}
	}

	// 尝试拉取外部模板，填入 proxies + proxy-groups
	if nodeMap, err := fetchClashTemplate(); err == nil {
		if err := injectProxiesToTemplate(nodeMap, proxies); err == nil {
			data, err := yaml.Marshal(nodeMap)
			if err == nil {
				return string(data), nil
			}
		}
	}

	// 退化：仅输出 proxies 列表
	data, err := yaml.Marshal(map[string]interface{}{clashProxiesKey: proxies})
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
// 并将所有节点名追加到空的或目标 proxy-group 中。
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

	// 把节点名注入 proxy-groups
	addProxyNamesToGroups(nodeMap, proxies)
	return nil
}

// addProxyNamesToGroups 把所有节点名添加到空 proxy-group 或名称为 "" 的 group。
func addProxyNamesToGroups(nodeMap NodeMap, proxies []*ClashProxy) {
	proxyGroups, ok := nodeMap[clashProxyGroupsKey]
	if !ok {
		return
	}

	names := make([]string, 0, len(proxies))
	for _, p := range proxies {
		names = append(names, p.Name)
	}

	for i, n := range proxyGroups.Content {
		data, err := yaml.Marshal(n)
		if err != nil {
			continue
		}
		pg := &ProxyGroup{}
		if err := yaml.Unmarshal(data, pg); err != nil {
			continue
		}
		// 注入条件：group 为空（占位 group）或 name 为空
		if len(pg.Proxies) == 0 || pg.Name == "" {
			pg.Proxies = append(pg.Proxies, names...)
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

func init() {
	Register(&ClashConverter{})
}
