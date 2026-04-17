package subscription

import (
	"errors"
	"net/url"
	"strings"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription/codec"
)

// ConvertURIs converts a list of raw proxy URIs to the subscription format that matches
// the given User-Agent string. This is the pkg equivalent of proxy/sub/converter.ConvertSubUri.
//
// User-Agent matching (case-insensitive contains):
//   - "clash"  → FormatClash
//   - "surge"  → FormatSurge
//   - "qv2ray" → FormatQv2ray
//   - default  → FormatCommon (base64)
func ConvertURIs(userAgent string, uris []string) (string, error) {
	format := detectFormat(userAgent)
	specs := urisToSpecs(uris)
	c := GetConverterOrDefault(format)
	if c == nil {
		return "", nil
	}
	return c.Convert(specs)
}

// ConvertURIsWithOptions converts URIs with custom options (proxy-groups, rules, etc).
// Only Clash format supports custom options; other formats ignore opts.
func ConvertURIsWithOptions(userAgent string, uris []string, opts *ConvertOptions) (string, error) {
	format := detectFormat(userAgent)
	specs := urisToSpecs(uris)
	c := GetConverterOrDefault(format)
	if c == nil {
		return "", nil
	}

	if cc, ok := c.(interface {
		ConvertWithOptions([]contracts.SubscriptionSpec, *ConvertOptions) (string, error)
	}); ok && opts != nil {
		return cc.ConvertWithOptions(specs, opts)
	}

	return c.Convert(specs)
}

// BuildConvertOptions 从 HTTP 参数构建 ConvertOptions。
// 参数解析失败时记录日志但不中断流程。
func BuildConvertOptions(proxyGroups, ruleProviders, rules []string, pgURL, rpURL, rURL string) *ConvertOptions {
	opts := &ConvertOptions{}

	for _, pg := range proxyGroups {
		config, err := ParseProxyGroupParam(pg)
		if err != nil {
			log.Warn("parse proxy_group param failed", "param", pg, "err", err)
			continue
		}
		opts.ProxyGroups = append(opts.ProxyGroups, *config)
	}

	if pgURL != "" {
		configs, err := FetchProxyGroupsFromURL(pgURL)
		if err != nil {
			log.Warn("fetch proxy_groups_url failed", "url", pgURL, "err", err)
		} else {
			opts.ProxyGroups = append(opts.ProxyGroups, configs...)
		}
	}

	for _, rp := range ruleProviders {
		config, err := ParseRuleProviderParam(rp)
		if err != nil {
			log.Warn("parse rule_provider param failed", "param", rp, "err", err)
			continue
		}
		opts.RuleProviders = append(opts.RuleProviders, *config)
	}

	if rpURL != "" {
		configs, err := FetchRuleProvidersFromURL(rpURL)
		if err != nil {
			log.Warn("fetch rule_providers_url failed", "url", rpURL, "err", err)
		} else {
			opts.RuleProviders = append(opts.RuleProviders, configs...)
		}
	}

	for _, r := range rules {
		config, err := ParseRuleParam(r)
		if err != nil {
			log.Warn("parse rule param failed", "param", r, "err", err)
			continue
		}
		opts.Rules = append(opts.Rules, *config)
	}

	if rURL != "" {
		configs, err := FetchRulesFromURL(rURL)
		if err != nil {
			log.Warn("fetch rules_url failed", "url", rURL, "err", err)
		} else {
			opts.Rules = append(opts.Rules, configs...)
		}
	}

	return opts
}

// DetectFormat selects a ClientFormat based on User-Agent string.
func DetectFormat(userAgent string) ClientFormat {
	return detectFormat(userAgent)
}

func detectFormat(userAgent string) ClientFormat {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, string(FormatClash)):
		return FormatClash
	case strings.Contains(ua, string(FormatSurge)):
		return FormatSurge
	case strings.Contains(ua, string(FormatQv2ray)):
		return FormatQv2ray
	default:
		return FormatCommon
	}
}

// urisToSpecs decodes each URI via the codec package and projects the typed Node
// into a SubscriptionSpec with Extensions populated for the converter layer.
// Unrecognized or malformed URIs pass through as {URI: raw} with no extensions.
//
// A warn log is emitted when a URI with a known scheme fails to decode
// (e.g. truncated base64, missing required field). Unknown schemes are
// silently treated as raw pass-through, since they may represent
// non-protocol input the caller wants to preserve verbatim.
func urisToSpecs(uris []string) []contracts.SubscriptionSpec {
	specs := make([]contracts.SubscriptionSpec, 0, len(uris))
	for _, uri := range uris {
		if uri == "" {
			continue
		}
		node, err := codec.Decode(uri)
		if err != nil {
			if !errors.Is(err, codec.ErrUnsupportedScheme) {
				log.Warn("subscription: codec.Decode failed, falling back to raw URI spec",
					"uri", redactURI(uri), "err", err)
			}
			specs = append(specs, contracts.SubscriptionSpec{URI: uri})
			continue
		}
		specs = append(specs, nodeToSpec(node, uri))
	}
	return specs
}

// redactURI returns a log-safe form of uri with any credentials stripped.
// VMess URIs carry credentials inside a base64-encoded JSON body, so we
// preserve only the scheme; other schemes keep scheme/host/port/fragment
// but drop the userinfo portion.
func redactURI(uri string) string {
	if strings.HasPrefix(uri, "vmess://") {
		return "vmess://[redacted]"
	}
	u, err := url.Parse(uri)
	if err != nil {
		if idx := strings.Index(uri, "://"); idx > 0 {
			return uri[:idx+3] + "[unparseable]"
		}
		return "[unparseable]"
	}
	u.User = nil
	u.RawQuery = ""
	return u.String()
}

// nodeToSpec converts a typed codec.Node into a SubscriptionSpec.
// Extensions use the keys the converter layer expects.
func nodeToSpec(node codec.Node, raw string) contracts.SubscriptionSpec {
	switch n := node.(type) {
	case *codec.VLessNode:
		return vlessNodeToSpec(n, raw)
	case *codec.VMessNode:
		return vmessNodeToSpec(n, raw)
	case *codec.TrojanNode:
		return trojanNodeToSpec(n, raw)
	case *codec.ShadowsocksNode:
		return ssNodeToSpec(n, raw)
	case *codec.Hysteria2Node:
		return hysteria2NodeToSpec(n, raw)
	case *codec.SnellNode:
		return snellNodeToSpec(n, raw)
	default:
		return contracts.SubscriptionSpec{URI: raw}
	}
}

func vlessNodeToSpec(n *codec.VLessNode, raw string) contracts.SubscriptionSpec {
	ext := map[string]any{}
	if n.SNI != "" {
		ext["server_name"] = n.SNI
	}
	if n.Security != "" {
		ext["security"] = n.Security
	}
	if n.Transport != "" {
		ext["transport"] = n.Transport
	}
	if n.Flow != "" {
		ext["flow"] = n.Flow
	}
	if n.Fingerprint != "" {
		ext["utls_fingerprint"] = n.Fingerprint
	}
	if n.RealityPublicKey != "" {
		ext["reality_public_key"] = n.RealityPublicKey
	}
	if n.RealityShortID != "" {
		ext["reality_short_ids"] = n.RealityShortID
	}
	if n.SkipCertVerify {
		ext["skip_cert_verify"] = true
	}
	if n.WSPath != "" {
		ext["ws_path"] = n.WSPath
	}
	if n.WSHost != "" {
		ext["ws_host"] = n.WSHost
	}
	if n.GRPCServiceName != "" {
		ext["grpc_service_name"] = n.GRPCServiceName
	}
	if n.XHTTPPath != "" {
		ext["xhttp_path"] = n.XHTTPPath
	}
	if n.XHTTPHost != "" {
		ext["xhttp_host"] = n.XHTTPHost
	}
	if n.XHTTPMode != "" {
		ext["xhttp_mode"] = n.XHTTPMode
	}
	return contracts.SubscriptionSpec{
		URI:        raw,
		Protocol:   contracts.ProtocolVLess,
		Host:       n.Host,
		Port:       n.Port,
		Password:   n.UUID,
		NodeName:   n.NodeName,
		Extensions: ext,
	}
}

func vmessNodeToSpec(n *codec.VMessNode, raw string) contracts.SubscriptionSpec {
	ext := map[string]any{}
	if n.Security != "" {
		ext["security"] = n.Security
	}
	if n.SNI != "" {
		ext["server_name"] = n.SNI
	}
	if n.Fingerprint != "" {
		ext["utls_fingerprint"] = n.Fingerprint
	}
	if n.AlterId != 0 {
		ext["alter_id"] = n.AlterId
	}
	if n.Transport != "" {
		ext["transport"] = n.Transport
	}
	if n.WSPath != "" {
		ext["ws_path"] = n.WSPath
	}
	if n.WSHost != "" {
		ext["ws_host"] = n.WSHost
	}
	if n.H2Path != "" {
		ext["http_path"] = n.H2Path
	}
	if n.H2Host != "" {
		ext["http_host"] = n.H2Host
	}
	if n.GRPCServiceName != "" {
		ext["grpc_service_name"] = n.GRPCServiceName
	}
	if n.XHTTPPath != "" {
		ext["xhttp_path"] = n.XHTTPPath
	}
	if n.XHTTPHost != "" {
		ext["xhttp_host"] = n.XHTTPHost
	}
	return contracts.SubscriptionSpec{
		URI:        raw,
		Protocol:   contracts.ProtocolVMess,
		Host:       n.Host,
		Port:       n.Port,
		Password:   n.UUID,
		NodeName:   n.NodeName,
		Extensions: ext,
	}
}

func trojanNodeToSpec(n *codec.TrojanNode, raw string) contracts.SubscriptionSpec {
	ext := map[string]any{}
	if n.SNI != "" {
		ext["server_name"] = n.SNI
	}
	if n.Security != "" {
		ext["security"] = n.Security
	}
	if n.Transport != "" {
		ext["transport"] = n.Transport
	}
	if n.Flow != "" {
		ext["flow"] = n.Flow
	}
	if n.Fingerprint != "" {
		ext["utls_fingerprint"] = n.Fingerprint
	}
	if n.RealityPublicKey != "" {
		ext["reality_public_key"] = n.RealityPublicKey
	}
	if n.RealityShortID != "" {
		ext["reality_short_ids"] = n.RealityShortID
	}
	if n.SkipCertVerify {
		ext["skip_cert_verify"] = true
	}
	if n.WSPath != "" {
		ext["ws_path"] = n.WSPath
	}
	if n.WSHost != "" {
		ext["ws_host"] = n.WSHost
	}
	if n.GRPCServiceName != "" {
		ext["grpc_service_name"] = n.GRPCServiceName
	}
	return contracts.SubscriptionSpec{
		URI:        raw,
		Protocol:   contracts.ProtocolTrojan,
		Host:       n.Host,
		Port:       n.Port,
		Password:   n.Password,
		NodeName:   n.NodeName,
		Extensions: ext,
	}
}

func ssNodeToSpec(n *codec.ShadowsocksNode, raw string) contracts.SubscriptionSpec {
	ext := map[string]any{}
	if n.Method != "" {
		ext["method"] = n.Method
	}
	if n.Plugin != "" {
		ext["plugin"] = n.Plugin
		// Surface plugin opts under stable keys the converters read. SIP003 plugins
		// use either obfs/obfs-host (obfs-local, simple-obfs) or mode/host
		// (v2ray-plugin, gost-plugin) — try both so converters see a single schema.
		if v, ok := n.PluginOpts["mode"]; ok && v != "" {
			ext["plugin_mode"] = v
		} else if v, ok := n.PluginOpts["obfs"]; ok && v != "" {
			ext["plugin_mode"] = v
		}
		if v, ok := n.PluginOpts["host"]; ok && v != "" {
			ext["plugin_host"] = v
		} else if v, ok := n.PluginOpts["obfs-host"]; ok && v != "" {
			ext["plugin_host"] = v
		}
		if v, ok := n.PluginOpts["path"]; ok && v != "" {
			ext["plugin_path"] = v
		}
		if _, ok := n.PluginOpts["tls"]; ok {
			ext["plugin_tls"] = true
		}
	}
	return contracts.SubscriptionSpec{
		URI:        raw,
		Protocol:   contracts.ProtocolShadowsocks,
		Host:       n.Host,
		Port:       n.Port,
		Password:   n.Password,
		NodeName:   n.NodeName,
		Extensions: ext,
	}
}

func hysteria2NodeToSpec(n *codec.Hysteria2Node, raw string) contracts.SubscriptionSpec {
	ext := map[string]any{}
	if n.SNI != "" {
		ext["server_name"] = n.SNI
	}
	if n.SkipCertVerify {
		ext["skip_cert_verify"] = true
	}
	if n.Obfs != "" {
		ext["obfs"] = n.Obfs
	}
	if n.ObfsPassword != "" {
		ext["obfs_password"] = n.ObfsPassword
	}
	if len(n.ALPN) > 0 {
		ext["alpn"] = n.ALPN
	}
	return contracts.SubscriptionSpec{
		URI:        raw,
		Protocol:   contracts.ProtocolHysteria2,
		Host:       n.Host,
		Port:       n.Port,
		Password:   n.Password,
		NodeName:   n.NodeName,
		Extensions: ext,
	}
}

func snellNodeToSpec(n *codec.SnellNode, raw string) contracts.SubscriptionSpec {
	ext := map[string]any{}
	if n.Version != 0 {
		ext["version"] = n.Version
	}
	// Snell obfs is "tls"/"http" (transport obfuscation), semantically distinct
	// from Hysteria2's salamander-style obfs. Only ClashConverter.convertSnell
	// returns nil today (Snell is Surge-only), and Surge's snell line format
	// does not yet accept obfs; both are projected so a future Surge/Clash-Meta
	// Snell converter can consume them without touching this layer.
	if n.Obfs != "" {
		ext["obfs"] = n.Obfs
	}
	if n.ObfsHost != "" {
		ext["obfs_host"] = n.ObfsHost
	}
	return contracts.SubscriptionSpec{
		URI:        raw,
		Protocol:   contracts.ProtocolSnell,
		Host:       n.Host,
		Port:       n.Port,
		Password:   n.PSK,
		NodeName:   n.NodeName,
		Extensions: ext,
	}
}
