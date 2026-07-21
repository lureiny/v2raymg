package converter

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
)

// parseTuicHeartbeatMs accepts both Go duration strings ("10s", "500ms") and
// raw integer milliseconds ("10000"). The latter matches mihomo's outbound
// schema directly, so an operator copying upstream values literally won't
// silently drop to 0. Returns 0 for unparseable input — mihomo client
// applies its own default (10000 ms) when the field is absent or zero, so
// the user-visible behaviour stays sane.
func parseTuicHeartbeatMs(s string) int {
	if s == "" {
		return 0
	}
	// Try integer ms first — `time.ParseDuration("10000")` errors out, and
	// emitting raw ms is the literal mihomo wire format.
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		if n < 0 {
			return 0
		}
		return n
	}
	if d, err := time.ParseDuration(s); err == nil {
		ms := int(d / time.Millisecond)
		if ms < 0 {
			return 0
		}
		return ms
	}
	return 0
}

// clashTemplateURLs 是外部 Clash 模板服务的 URL 列表，用一个假节点触发，拉回完整的
// Clash 配置骨架（proxy-groups、rules 等）。这里**故意不硬编码任何默认值** —— 推荐的
// 公共服务地址放在 config.example.yaml 里，运行时由 SetClashTemplateURLs 从
// subscription.clash_template_urls 注入。列表为空（或全部失败）时，fetchClashTemplate
// 返回 error，ConvertWithOptions 回退到内置极简模板，/sub 不会因此整体失败。
var (
	clashTemplateMu   sync.RWMutex
	clashTemplateURLs []string
)

// SetClashTemplateURLs 配置外部 Clash 模板服务列表。由启动流程用 app config 的
// subscription.clash_template_urls 调用一次。空白项会被丢弃；传入空列表（或全为空白）
// 是合法的，表示 /sub 始终使用内置极简回退模板。每个 URL 必须包含一个 "%s" 占位符，
// 在抓取时替换为 URL 编码后的触发订阅；不含占位符的项会被跳过（并打 warning），而不是
// 生成一个畸形 URL。
func SetClashTemplateURLs(urls []string) {
	cleaned := make([]string, 0, len(urls))
	for _, u := range urls {
		if s := strings.TrimSpace(u); s != "" {
			cleaned = append(cleaned, s)
		}
	}
	clashTemplateMu.Lock()
	clashTemplateURLs = cleaned
	clashTemplateMu.Unlock()
}

// clashTemplateURLsSnapshot 返回配置列表的副本，使调用方在做网络 IO 时不持锁。
func clashTemplateURLsSnapshot() []string {
	clashTemplateMu.RLock()
	defer clashTemplateMu.RUnlock()
	out := make([]string, len(clashTemplateURLs))
	copy(out, clashTemplateURLs)
	return out
}

// fetchClashTemplateFn is a seam so tests can force the external fetch to fail
// and exercise the built-in fallback deterministically (without network).
var fetchClashTemplateFn = fetchClashTemplate

// localClashTemplate is a minimal, self-contained Clash config skeleton used
// only when every external sub-converter source is unreachable. It is
// deliberately bare — a single "Manual" selector plus a catch-all rule — so it
// is obviously a degraded fallback rather than a silent stand-in for the rich
// third-party template. Because it is built in, it also carries none of the
// external template's injection surface (no dns / proxy-providers / third-party
// rules). injectProxiesToTemplate overwrites `proxies` and ensureTemplateGroups
// fills the empty Manual group with the node names (+ DIRECT).
const localClashTemplate = `port: 7890
socks-port: 7891
allow-lan: false
mode: rule
log-level: info
proxies: []
proxy-groups:
  - name: Manual
    type: select
    proxies: []
rules:
  - MATCH,Manual
`

// localClashTemplateNodeMap parses the built-in fallback template into the same
// NodeMap shape fetchClashTemplate returns, so both feed the identical
// injectProxiesToTemplate / patch pipeline.
func localClashTemplateNodeMap() (NodeMap, error) {
	nodeMap := NodeMap{}
	if err := yaml.Unmarshal([]byte(localClashTemplate), nodeMap); err != nil {
		return nil, fmt.Errorf("parse built-in clash fallback template: %w", err)
	}
	return nodeMap, nil
}

const (
	clashFakeSubName = "FakeSub"
	clashFakeSub     = "ss://YWVzLTEyOC1nY206dGVzdA==@192.168.100.1:8888#" + clashFakeSubName

	clashProxiesKey     = "proxies"
	clashProxyGroupsKey = "proxy-groups"

	// defaultRealityFingerprint is the fallback client-fingerprint for reality
	// nodes when the caller does not supply utls_fingerprint. mihomo requires
	// a non-empty value; callers that supply an explicit value take precedence.
	defaultRealityFingerprint = "chrome"
)

// ClashConverter emits a full Clash (mihomo) YAML including proxies,
// proxy-groups and rules. When the external template service is unreachable,
// it falls back to a proxies-only output.
type ClashConverter struct{}

func (c *ClashConverter) Format() subscription.ClientFormat {
	return subscription.FormatClash
}

func (c *ClashConverter) Convert(specs []contracts.SubscriptionSpec) (string, error) {
	return c.ConvertWithOptions(specs, nil)
}

type (
	ProxyGroupConfig   = subscription.ProxyGroupConfig
	RuleProviderConfig = subscription.RuleProviderConfig
	RuleConfig         = subscription.RuleConfig
	ConvertOptions     = subscription.ConvertOptions
)

func ParseProxyGroupParam(param string) (*ProxyGroupConfig, error) {
	return subscription.ParseProxyGroupParam(param)
}

func ParseRuleProviderParam(param string) (*RuleProviderConfig, error) {
	return subscription.ParseRuleProviderParam(param)
}

func ParseRuleParam(param string) (*RuleConfig, error) {
	return subscription.ParseRuleParam(param)
}

func FetchProxyGroupsFromURL(url string) ([]ProxyGroupConfig, error) {
	return subscription.FetchProxyGroupsFromURL(url)
}

func FetchRuleProvidersFromURL(url string) ([]RuleProviderConfig, error) {
	return subscription.FetchRuleProvidersFromURL(url)
}

func FetchRulesFromURL(url string) ([]RuleConfig, error) {
	return subscription.FetchRulesFromURL(url)
}

var builtinPolicies = map[string]bool{
	"DIRECT":     true,
	"REJECT":     true,
	"PASS":       true,
	"COMPATIBLE": true,
}

// MatchProxies resolves a Clash proxies spec entry into actual node names.
// An entry may be: "all" (all nodes), a builtin policy (DIRECT/REJECT/...),
// a defined group name, or a regex matched against node names.
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
			for _, name := range nodeNames {
				if matched, _ := regexp.MatchString(p, name); matched {
					result = append(result, name)
				}
			}
		}
	}
	return result
}

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

// ConvertWithOptions emits a full Clash YAML. Flow:
//  1. build proxies
//  2. fetch external template (fail fast; no silent fallback)
//  3. inject proxies + reconcile template-defined Auto/Manual groups
//  4. if opts has custom groups/rules, patch the template; else return as-is
func (c *ClashConverter) ConvertWithOptions(specs []contracts.SubscriptionSpec, opts *ConvertOptions) (string, error) {
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

	// Prefer the external template; its rich proxy-groups/rules are the intended
	// output. But the node has no control over those third-party services, so a
	// total outage must not hard-fail every subscription (availability). When all
	// sources fail, degrade to the built-in minimal template. This is logged
	// loudly and the fallback is deliberately bare — not a silent swap for
	// different rich routing semantics — and it is injection-free.
	nodeMap, err := fetchClashTemplateFn()
	if err != nil {
		log.Warnf("clash: all external template sources failed (%v); using built-in minimal fallback template", err)
		nodeMap, err = localClashTemplateNodeMap()
		if err != nil {
			return "", fmt.Errorf("clash fallback template failed after external sources failed: %w", err)
		}
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

// ConvertVMessForTest exposes convertVMess to integration tests in the
// systemtest package so they can exercise the real subscription→converter
// chain without going through the full ConvertWithOptions pipeline (which
// requires network access to fetch the external Clash template).
//
// Not part of the public API; the "ForTest" suffix marks it as a
// test-only seam. Production code paths must continue to go through
// ConvertWithOptions.
func ConvertVMessForTest(spec contracts.SubscriptionSpec) *ClashProxy {
	return (&ClashConverter{}).convertVMess(spec)
}

// ConvertTrojanForTest exposes convertTrojan to integration tests for the
// same reason as ConvertVMessForTest: system tests can exercise the real
// subscription→converter chain without fetching the external Clash template.
func ConvertTrojanForTest(spec contracts.SubscriptionSpec) *ClashProxy {
	return (&ClashConverter{}).convertTrojan(spec)
}

// ConvertVLessForTest exposes convertVLess to integration tests for the same
// reason as ConvertVMessForTest. VLESS is the converter's most complex path
// (reality/vision/xhttp) and previously had no ForTest seam, so its system
// matrix built the client proxy by hand and never exercised the real
// GetUserSubscriptions→convertVLess chain.
func ConvertVLessForTest(spec contracts.SubscriptionSpec) *ClashProxy {
	return (&ClashConverter{}).convertVLess(spec)
}

// ConvertSSForTest exposes convertShadowsocks to integration tests for the
// same reason as ConvertVMessForTest: system tests can exercise the real
// subscription→converter chain without fetching the external Clash template.
func ConvertSSForTest(spec contracts.SubscriptionSpec) *ClashProxy {
	return (&ClashConverter{}).convertShadowsocks(spec)
}

// ConvertHysteria2ForTest exposes convertHysteria2 to integration tests for
// the same reason as ConvertVMessForTest. Phase 5 systemtest uses this to
// drive the real GetUserSubscriptions → convertHysteria2 chain.
func ConvertHysteria2ForTest(spec contracts.SubscriptionSpec) *ClashProxy {
	return (&ClashConverter{}).convertHysteria2(spec)
}

// ConvertTuicForTest exposes convertTuic to integration tests for the
// cross-cutting subscription chain (TUIC matrix test).
func ConvertTuicForTest(spec contracts.SubscriptionSpec) *ClashProxy {
	return (&ClashConverter{}).convertTuic(spec)
}

// ConvertAnyTLSForTest exposes convertAnyTLS to integration tests for
// the AnyTLS subscription chain. Used by mihomo_anytls_matrix_test to
// drive the real GetUserSubscriptions → convertAnyTLS chain.
func ConvertAnyTLSForTest(spec contracts.SubscriptionSpec) *ClashProxy {
	return (&ClashConverter{}).convertAnyTLS(spec)
}

// convertSpec returns nil if the protocol is unsupported by Clash/mihomo or
// the node's transport/security combination is not expressible.
func (c *ClashConverter) convertSpec(spec contracts.SubscriptionSpec) *ClashProxy {
	switch spec.Protocol {
	case contracts.ProtocolVLess:
		return c.convertVLess(spec)
	case contracts.ProtocolSnell:
		// Snell is Surge-only.
		return nil
	case contracts.ProtocolVMess:
		return c.convertVMess(spec)
	case contracts.ProtocolTrojan:
		return c.convertTrojan(spec)
	case contracts.ProtocolShadowsocks:
		return c.convertShadowsocks(spec)
	case contracts.ProtocolHysteria2:
		return c.convertHysteria2(spec)
	case contracts.ProtocolTUIC:
		return c.convertTuic(spec)
	case contracts.ProtocolAnyTLS:
		return c.convertAnyTLS(spec)
	default:
		return nil
	}
}

func (c *ClashConverter) convertVMess(spec contracts.SubscriptionSpec) *ClashProxy {
	ext := spec.Extensions
	alterId := extInt(ext, "alter_id")
	proxy := &ClashProxy{
		Name:    c.nodeName(spec, "VMESS"),
		Type:    "vmess",
		Server:  spec.Host,
		Port:    int(spec.Port),
		UUID:    spec.Password,
		Cipher:  "auto",
		AlterId: ptrInt(alterId),
		UDP:     true,
	}

	// Security: tls / reality. Reality requires the same tls=true marker
	// mihomo VLESS uses (see convertVLess line ~360); without RealityOpts
	// the client falls back to plain TLS and the handshake fails.
	security := extString(ext, "security")
	switch security {
	case "tls":
		proxy.TLS = true
	case "reality":
		proxy.TLS = true
		proxy.RealityOpts = buildRealityOpts(ext)
	}
	if skipVerify, _ := ext["skip_cert_verify"].(bool); skipVerify {
		proxy.SkipCertVerify = true
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
		proxy.WSOpts = buildWSOpts(ext)
	case "grpc":
		proxy.Network = "grpc"
		if serviceName := extString(ext, "grpc_service_name"); serviceName != "" {
			proxy.GrpcOpts = &GrpcOpts{GrpcServiceName: serviceName}
		}
	case "h2", "http":
		proxy.Network = "h2"
		path := extString(ext, "http_path")
		host := extString(ext, "http_host")
		if path != "" || host != "" {
			opts := &H2Opts{Path: path}
			if host != "" {
				opts.Host = []string{host}
			}
			proxy.H2Opts = opts
		}
	case "httpupgrade", "xhttp":
		// Mihomo VMess 不支持 httpupgrade / xhttp transport
		return nil
	case "mkcp", "kcp", "quic":
		// Mihomo VMess 不支持 kcp / quic transport
		return nil
	}

	// client-fingerprint; reality requires a non-empty value
	if fp := extString(ext, "utls_fingerprint"); fp != "" {
		proxy.ClientFingerprint = fp
	} else if security == "reality" {
		proxy.ClientFingerprint = defaultRealityFingerprint
	}

	return proxy
}

// buildWSOpts returns nil when neither path nor host is set, so the YAML output
// drops the `ws-opts` key entirely instead of emitting an empty map.
func buildWSOpts(ext map[string]any) *WSOpts {
	path := extString(ext, "ws_path")
	host := extString(ext, "ws_host")
	if path == "" && host == "" {
		return nil
	}
	opts := &WSOpts{Path: path}
	if host != "" {
		opts.Headers = &WSHeaders{Host: host}
	}
	return opts
}

// convertTrojan: security=xtls maps to regular TLS + flow in mihomo (xray legacy compat).
// security=none is codec-normalized to "" before reaching us (trojan wire level requires TLS).
func (c *ClashConverter) convertTrojan(spec contracts.SubscriptionSpec) *ClashProxy {
	ext := spec.Extensions
	security := extString(ext, "security")

	proxy := &ClashProxy{
		Name:     c.nodeName(spec, "TROJAN"),
		Type:     "trojan",
		Server:   spec.Host,
		Port:     int(spec.Port),
		Password: spec.Password,
		UDP:      true,
		TLS:      true,
	}

	if security == "reality" {
		proxy.RealityOpts = buildRealityOpts(ext)
	}

	// SNI（tls / xtls / reality 均适用）
	if sni := extString(ext, "server_name"); sni != "" {
		proxy.SNI = sni
	}

	// flow（xtls-rprx-vision 等，mihomo trojan 直接支持）
	if flow := extString(ext, "flow"); flow != "" {
		proxy.Flow = flow
	}

	// client-fingerprint; reality requires a non-empty value
	if fp := extString(ext, "utls_fingerprint"); fp != "" {
		proxy.ClientFingerprint = fp
	} else if security == "reality" {
		proxy.ClientFingerprint = defaultRealityFingerprint
	}
	if skipVerify, _ := ext["skip_cert_verify"].(bool); skipVerify {
		proxy.SkipCertVerify = true
	}

	// Transport
	transport := extString(ext, "transport")
	switch transport {
	case "ws":
		proxy.Network = "ws"
		proxy.WSOpts = buildWSOpts(ext)
	case "grpc":
		proxy.Network = "grpc"
		if serviceName := extString(ext, "grpc_service_name"); serviceName != "" {
			proxy.GrpcOpts = &GrpcOpts{GrpcServiceName: serviceName}
		}
	case "httpupgrade", "xhttp":
		// Mihomo Trojan 不支持 httpupgrade / xhttp transport
		return nil
	}

	return proxy
}

// buildRealityOpts returns nil when public-key is missing; reality-opts without
// a public key would be rejected by mihomo.
func buildRealityOpts(ext map[string]any) *RealityOpts {
	pubKey := extString(ext, "reality_public_key")
	if pubKey == "" {
		return nil
	}
	shortID := ""
	if s := extStringOrJoinSlice(ext, "reality_short_ids"); s != "" {
		shortID = strings.SplitN(s, ",", 2)[0]
	}
	return &RealityOpts{PublicKey: pubKey, ShortID: shortID}
}

// convertVLess skips xtls security and httpupgrade transport (not supported by mihomo VLESS).
func (c *ClashConverter) convertVLess(spec contracts.SubscriptionSpec) *ClashProxy {
	ext := spec.Extensions
	security := extString(ext, "security")

	// Mihomo VLESS 不支持 XTLS
	if security == "xtls" {
		return nil
	}

	proxy := &ClashProxy{
		Name:   c.nodeName(spec, "VLESS"),
		Type:   "vless",
		Server: spec.Host,
		Port:   int(spec.Port),
		UUID:   spec.Password,
		UDP:    true,
	}

	// Security
	switch security {
	case "tls":
		proxy.TLS = true
		if sni := extString(ext, "server_name"); sni != "" {
			proxy.Servername = sni
		}
	case "reality":
		proxy.TLS = true // Mihomo 要求 reality 节点设置 tls=true
		if sni := extString(ext, "server_name"); sni != "" {
			proxy.Servername = sni
		}
		proxy.RealityOpts = buildRealityOpts(ext)
	}

	// Flow（如 xtls-rprx-vision）
	if flow := extString(ext, "flow"); flow != "" {
		proxy.Flow = flow
	}

	// client-fingerprint; reality requires a non-empty value
	if fp := extString(ext, "utls_fingerprint"); fp != "" {
		proxy.ClientFingerprint = fp
	} else if security == "reality" {
		proxy.ClientFingerprint = defaultRealityFingerprint
	}
	if skipVerify, _ := ext["skip_cert_verify"].(bool); skipVerify {
		proxy.SkipCertVerify = true
	}

	// Transport
	transport := extString(ext, "transport")
	switch transport {
	case "ws":
		proxy.Network = "ws"
		proxy.WSOpts = buildWSOpts(ext)
	case "grpc":
		proxy.Network = "grpc"
		if serviceName := extString(ext, "grpc_service_name"); serviceName != "" {
			proxy.GrpcOpts = &GrpcOpts{GrpcServiceName: serviceName}
		}
	case "xhttp", "splithttp":
		proxy.Network = "xhttp"
		path := extString(ext, "xhttp_path")
		host := extStringOrJoinSlice(ext, "xhttp_host")
		mode := extString(ext, "xhttp_mode")
		if path != "" || host != "" || mode != "" {
			proxy.XHTTPOpts = &XHTTPOpts{Path: path, Host: host, Mode: mode}
		}
	case "httpupgrade":
		// Mihomo VLESS 不支持 httpupgrade transport
		return nil
	}

	return proxy
}

func (c *ClashConverter) convertShadowsocks(spec contracts.SubscriptionSpec) *ClashProxy {
	method := extString(spec.Extensions, "method")
	if method == "" {
		method = "2022-blake3-aes-256-gcm"
	}

	// UDP defaults to true (SS is almost always used with UDP). When the
	// ProtocolParams path emits udp=false in Extensions we honour it.
	udp := true
	if v, ok := spec.Extensions["udp"]; ok {
		if b, ok := v.(bool); ok {
			udp = b
		}
	}

	proxy := &ClashProxy{
		Name:     c.nodeName(spec, "SS"),
		Type:     "ss",
		Server:   spec.Host,
		Port:     int(spec.Port),
		Password: spec.Password,
		Cipher:   method,
		UDP:      udp,
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

func (c *ClashConverter) convertHysteria2(spec contracts.SubscriptionSpec) *ClashProxy {
	proxy := &ClashProxy{
		Name:     c.nodeName(spec, "HYSTERIA2"),
		Type:     "hysteria2",
		Server:   spec.Host,
		Port:     int(spec.Port),
		Password: spec.Password,
		UDP:      true,
	}
	ext := spec.Extensions
	if sni := extString(ext, "server_name"); sni != "" {
		proxy.SNI = sni
	}
	// skip-cert-verify: parsed extension bool, with legacy URI insecure=1 fallback
	// for specs constructed without going through codec.Decode.
	if skipVerify, _ := ext["skip_cert_verify"].(bool); skipVerify || strings.Contains(spec.URI, "insecure=1") {
		proxy.SkipCertVerify = true
	}
	if obfs := extString(ext, "obfs"); obfs != "" {
		proxy.Obfs = obfs
	}
	if pwd := extString(ext, "obfs_password"); pwd != "" {
		proxy.ObfsPassword = pwd
	}
	// Up/Down advertise the client's bandwidth to the server. They are
	// optional on the client side; mihomo accepts strings like "50 Mbps".
	// Masquerade is server-only and intentionally NOT propagated here —
	// the upstream client schema has no field for it. (No read of
	// "masquerade" anywhere in this function — TestConvertHysteria2_DropsMasquerade
	// locks this contract.)
	if up := extString(ext, "up"); up != "" {
		proxy.Up = up
	}
	if down := extString(ext, "down"); down != "" {
		proxy.Down = down
	}
	return proxy
}

// convertTuic emits a mihomo TUIC v5 outbound proxy entry.
//
// Field mapping notes (see reference_tuic_protocol_facts.md for the full
// table). The mihomo client schema deliberately uses different keys from
// the listener — beware:
//
//   - ZeroRTTHandshake (ext "zero_rtt_handshake") → `reduce-rtt: true`
//     (NOT "zero-rtt-handshake" — that's the wrong key name and would be
//     ignored silently).
//   - HeartbeatInterval (ext "heartbeat_interval", e.g. "10s") → integer
//     milliseconds via time.ParseDuration. Unparseable values fall through
//     as 0 so mihomo's own client default (10000ms) applies.
//   - CongestionController (ext "congestion_controller") → mihomo client
//     has no default for this; we emit it verbatim. Server-side parse-time
//     default of "bbr" is enforced in profilegen, not here.
//   - UDPRelayMode (ext "udp_relay_mode") → mihomo client default is
//     "native"; emit only when explicitly "quic" (the case the operator
//     bothered to set).
//   - ALPN: mihomo client defaults to ["h3"], same as the listener forces.
//     We don't propagate ALPN here — both sides agree on h3 by default,
//     and an explicit override would only land via Extensions which is
//     out of scope for Phase 6.
func (c *ClashConverter) convertTuic(spec contracts.SubscriptionSpec) *ClashProxy {
	ext := spec.Extensions
	proxy := &ClashProxy{
		Name:     c.nodeName(spec, "TUIC"),
		Type:     "tuic",
		Server:   spec.Host,
		Port:     int(spec.Port),
		UUID:     extString(ext, "uuid"),
		Password: spec.Password,
		UDP:      true,
	}
	if sni := extString(ext, "server_name"); sni != "" {
		proxy.SNI = sni
	}
	if skipVerify, _ := ext["skip_cert_verify"].(bool); skipVerify || strings.Contains(spec.URI, "allow_insecure=1") {
		proxy.SkipCertVerify = true
	}
	if cc := extString(ext, "congestion_controller"); cc != "" {
		proxy.CongestionController = cc
	}
	if mode := extString(ext, "udp_relay_mode"); mode != "" {
		proxy.UDPRelayMode = mode
	}
	if zr, _ := ext["zero_rtt_handshake"].(bool); zr {
		proxy.ReduceRTT = true
	}
	if disable, _ := ext["disable_sni"].(bool); disable {
		proxy.DisableSNI = true
	}
	if hb := extString(ext, "heartbeat_interval"); hb != "" {
		proxy.HeartbeatInterval = parseTuicHeartbeatMs(hb)
	}
	return proxy
}

// convertAnyTLS emits a mihomo AnyTLS outbound proxy entry.
//
// Schema parity (see project_protocol_expansion_status.md Phase 7
// section + reference_anytls_protocol_facts):
//
//   - Required fields: server / port / password (plus name/type).
//   - sni / skip-cert-verify follow the standard TLS-required pattern,
//     read from Extensions ("server_name" / "skip_cert_verify") just
//     like hy2 and tuic.
//   - idle-session-check-interval / idle-session-timeout / min-idle-session
//     are int seconds in mihomo's outbound schema; we read them from
//     the matching `_seconds` Extensions keys (or `min_idle_session`
//     for the int-typed knob). Zero defers to mihomo runtime defaults
//     (≤5s gets bumped to 30s by `session/client.go:46-51`), so omit
//     here when zero rather than send 0 explicitly.
//   - PaddingScheme is server-only (mihomo client takes no
//     padding-scheme yaml) and ALPN is unspecified by the URI, so
//     neither is propagated client-side.
//   - udp:true is hardcoded — mihomo wraps UDP in UoT-over-TCP
//     transparently, matching how other TCP-tunnel converters set udp.
func (c *ClashConverter) convertAnyTLS(spec contracts.SubscriptionSpec) *ClashProxy {
	ext := spec.Extensions
	proxy := &ClashProxy{
		Name:     c.nodeName(spec, "AnyTLS"),
		Type:     "anytls",
		Server:   spec.Host,
		Port:     int(spec.Port),
		Password: spec.Password,
		UDP:      true,
	}
	if sni := extString(ext, "server_name"); sni != "" {
		proxy.SNI = sni
	}
	if skipVerify, _ := ext["skip_cert_verify"].(bool); skipVerify || strings.Contains(spec.URI, "insecure=1") {
		proxy.SkipCertVerify = true
	}
	if v := extInt(ext, "idle_session_check_interval_seconds"); v > 0 {
		proxy.IdleSessionCheckInterval = v
	}
	if v := extInt(ext, "idle_session_timeout_seconds"); v > 0 {
		proxy.IdleSessionTimeout = v
	}
	if v := extInt(ext, "min_idle_session"); v > 0 {
		proxy.MinIdleSession = v
	}
	return proxy
}

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
	urls := clashTemplateURLsSnapshot()
	if len(urls) == 0 {
		// 没有配置任何模板源（subscription.clash_template_urls 为空）。这不是硬错误：
		// 调用方 ConvertWithOptions 会捕获这个 error 并回退到内置极简模板。
		return nil, fmt.Errorf("no clash template sources configured (subscription.clash_template_urls empty)")
	}
	errMsg := ""
	for _, tmpl := range urls {
		if !strings.Contains(tmpl, "%s") {
			log.Warnf("clash: template URL %q missing %%s placeholder; skipping", tmpl)
			errMsg += "|missing %s placeholder: " + tmpl
			continue
		}
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
		pruneUnsafeTemplateKeys(nodeMap)
		return nodeMap, nil
	}
	return nil, fmt.Errorf("all clash template sources failed: %s", errMsg)
}

// pruneUnsafeTemplateKeys drops top-level keys that this project never emits
// itself but that the (untrusted, third-party) Clash template could inject with
// active/hijack semantics: "dns" (DNS hijack) and "proxy-providers" (client-side
// remote fetch = exfil + second-stage injection). Stripping them is lossless for
// our output, which only ever produces proxies/proxy-groups/rules/rule-providers.
//
// This NARROWS the injection surface; it does not eliminate template trust:
// "rules"/"rule-providers"/"proxy-groups" are still passed through from the
// third-party sources (and rule-providers is itself a client-side remote-fetch
// primitive). Fully removing that trust requires self-hosting the template or a
// full schema allowlist, which is out of scope here.
func pruneUnsafeTemplateKeys(nodeMap NodeMap) {
	for _, k := range []string{"dns", "proxy-providers"} {
		delete(nodeMap, k)
	}
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

		// 空 proxy-group 填充节点：Manual 附加 DIRECT 兜底，其余(含 Auto)只填节点名。
		if len(pg.Proxies) == 0 {
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
	Name              string       `yaml:"name"`
	Type              string       `yaml:"type"`
	Server            string       `yaml:"server"`
	Port              int          `yaml:"port"`
	Password          string       `yaml:"password,omitempty"`
	UUID              string       `yaml:"uuid,omitempty"`
	AlterId           *int         `yaml:"alterId,omitempty"`
	Cipher            string       `yaml:"cipher,omitempty"`
	Plugin            string       `yaml:"plugin,omitempty"`
	PluginOpts        *PluginOpts  `yaml:"plugin-opts,omitempty"`
	UDP               bool         `yaml:"udp,omitempty"`
	TLS               bool         `yaml:"tls,omitempty"`
	SkipCertVerify    bool         `yaml:"skip-cert-verify,omitempty"`
	Servername        string       `yaml:"servername,omitempty"`
	SNI               string       `yaml:"sni,omitempty"`
	Flow              string       `yaml:"flow,omitempty"`
	ClientFingerprint string       `yaml:"client-fingerprint,omitempty"`
	Network           string       `yaml:"network,omitempty"`
	RealityOpts       *RealityOpts `yaml:"reality-opts,omitempty"`
	WSOpts            *WSOpts      `yaml:"ws-opts,omitempty"`
	H2Opts            *H2Opts      `yaml:"h2-opts,omitempty"`
	HttpOpts          *HttpOpts    `yaml:"http-opts,omitempty"`
	GrpcOpts          *GrpcOpts    `yaml:"grpc-opts,omitempty"`
	XHTTPOpts         *XHTTPOpts   `yaml:"xhttp-opts,omitempty"`
	Obfs              string       `yaml:"obfs,omitempty"`          // Hysteria2: salamander
	ObfsPassword      string       `yaml:"obfs-password,omitempty"` // Hysteria2: obfs password
	Up                string       `yaml:"up,omitempty"`            // Hysteria2: client bandwidth advertise
	Down              string       `yaml:"down,omitempty"`          // Hysteria2: client bandwidth advertise
	// TUIC client-only fields (see convertTuic / reference_tuic_protocol_facts.md).
	CongestionController string `yaml:"congestion-controller,omitempty"`
	UDPRelayMode         string `yaml:"udp-relay-mode,omitempty"`
	ReduceRTT            bool   `yaml:"reduce-rtt,omitempty"`
	HeartbeatInterval    int    `yaml:"heartbeat-interval,omitempty"` // milliseconds; 0 lets mihomo apply its own default (10000)
	DisableSNI           bool   `yaml:"disable-sni,omitempty"`
	// AnyTLS client-only fields (see convertAnyTLS). Idle/min are integer
	// seconds in mihomo's outbound schema; 0 defers to mihomo runtime
	// defaults (~30s), so we leave them omitempty rather than emit zeros.
	// Cert-pin fingerprint is intentionally not surfaced here — see the
	// codec/anytls.go AnyTLSNode.Fingerprint comment for the URI
	// hpkp= path; piping it through to the client outbound config would
	// require a TLSSpec field for cert-pin which we do not have today.
	IdleSessionCheckInterval int `yaml:"idle-session-check-interval,omitempty"`
	IdleSessionTimeout       int `yaml:"idle-session-timeout,omitempty"`
	MinIdleSession           int `yaml:"min-idle-session,omitempty"`
}

// PluginOpts obfs/v2ray-plugin/shadow-tls 选项。
type PluginOpts struct {
	Mode           string `yaml:"mode,omitempty"`
	Host           string `yaml:"host,omitempty"`
	TLS            bool   `yaml:"tls,omitempty"`
	SkipCertVerify bool   `yaml:"skip-cert-verify,omitempty"`
	Path           string `yaml:"path,omitempty"`
	MUX            bool   `yaml:"mux,omitempty"`
	// shadow-tls specific
	Password string `yaml:"password,omitempty"`
	Version  int    `yaml:"version,omitempty"`
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

// RealityOpts Reality 选项（Mihomo VLESS/Trojan）。
type RealityOpts struct {
	PublicKey string `yaml:"public-key"`
	ShortID   string `yaml:"short-id"`
}

// XHTTPOpts xhttp（SplitHTTP）选项（Mihomo VLESS）。
type XHTTPOpts struct {
	Path string `yaml:"path,omitempty"`
	Host string `yaml:"host,omitempty"`
	Mode string `yaml:"mode,omitempty"`
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
	if pwd := extString(ext, "plugin_password"); pwd != "" {
		opts.Password = pwd
	}
	if ver := extString(ext, "plugin_version"); ver != "" {
		if n, err := strconv.Atoi(ver); err == nil {
			opts.Version = n
		}
	}
	if *opts == (PluginOpts{}) {
		return nil
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
