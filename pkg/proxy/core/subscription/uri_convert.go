package subscription

import (
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
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

	// 检查是否支持 ConvertWithOptions
	if cc, ok := c.(interface{ ConvertWithOptions([]contracts.SubscriptionSpec, *ConvertOptions) (string, error) }); ok && opts != nil {
		return cc.ConvertWithOptions(specs, opts)
	}

	return c.Convert(specs)
}

// BuildConvertOptions 从 HTTP 参数构建 ConvertOptions。
// 参数解析失败时记录日志但不中断流程。
func BuildConvertOptions(proxyGroups, ruleProviders, rules []string, pgURL, rpURL, rURL string) *ConvertOptions {
	opts := &ConvertOptions{}

	// 解析 proxy_group 参数
	for _, pg := range proxyGroups {
		config, err := ParseProxyGroupParam(pg)
		if err != nil {
			log.Warn("parse proxy_group param failed", "param", pg, "err", err)
			continue
		}
		opts.ProxyGroups = append(opts.ProxyGroups, *config)
	}

	// 从 URL 获取 proxy-groups
	if pgURL != "" {
		configs, err := FetchProxyGroupsFromURL(pgURL)
		if err != nil {
			log.Warn("fetch proxy_groups_url failed", "url", pgURL, "err", err)
		} else {
			opts.ProxyGroups = append(opts.ProxyGroups, configs...)
		}
	}

	// 解析 rule_provider 参数
	for _, rp := range ruleProviders {
		config, err := ParseRuleProviderParam(rp)
		if err != nil {
			log.Warn("parse rule_provider param failed", "param", rp, "err", err)
			continue
		}
		opts.RuleProviders = append(opts.RuleProviders, *config)
	}

	// 从 URL 获取 rule-providers
	if rpURL != "" {
		configs, err := FetchRuleProvidersFromURL(rpURL)
		if err != nil {
			log.Warn("fetch rule_providers_url failed", "url", rpURL, "err", err)
		} else {
			opts.RuleProviders = append(opts.RuleProviders, configs...)
		}
	}

	// 解析 rule 参数
	for _, r := range rules {
		config, err := ParseRuleParam(r)
		if err != nil {
			log.Warn("parse rule param failed", "param", r, "err", err)
			continue
		}
		opts.Rules = append(opts.Rules, *config)
	}

	// 从 URL 获取 rules
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

// detectFormat selects a ClientFormat based on User-Agent string.
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

// urisToSpecs converts plain URI strings into SubscriptionSpec objects.
// For protocols that converters handle via structured fields (Host, Port, Password, etc.),
// the URI is parsed into those fields. For unknown URIs, only the URI field is set.
func urisToSpecs(uris []string) []contracts.SubscriptionSpec {
	specs := make([]contracts.SubscriptionSpec, 0, len(uris))
	for _, uri := range uris {
		if uri == "" {
			continue
		}
		if spec, ok := parseProxyURI(uri); ok {
			specs = append(specs, spec)
		} else {
			specs = append(specs, contracts.SubscriptionSpec{URI: uri})
		}
	}
	return specs
}

// parseProxyURI detects the scheme and delegates to the appropriate parser.
func parseProxyURI(raw string) (contracts.SubscriptionSpec, bool) {
	switch {
	case strings.HasPrefix(raw, "trojan://"):
		return parseStandardURI(raw, contracts.ProtocolTrojan)
	case strings.HasPrefix(raw, "hysteria2://") || strings.HasPrefix(raw, "hy2://"):
		return parseStandardURI(raw, contracts.ProtocolHysteria2)
	case strings.HasPrefix(raw, "vless://"):
		return parseStandardURI(raw, contracts.ProtocolVLess)
	case strings.HasPrefix(raw, "ss://"):
		return parseShadowsocksURI(raw)
	case strings.HasPrefix(raw, "vmess://"):
		return parseVMessURI(raw)
	case strings.HasPrefix(raw, "snell://"):
		return parseStandardURI(raw, contracts.ProtocolSnell)
	default:
		return contracts.SubscriptionSpec{}, false
	}
}

// parseStandardURI parses URIs with format: scheme://password@host:port?params#fragment
// Works for trojan, hysteria2, vless, snell.
func parseStandardURI(raw string, protocol contracts.Protocol) (contracts.SubscriptionSpec, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return contracts.SubscriptionSpec{}, false
	}
	port, _ := strconv.ParseUint(u.Port(), 10, 32)
	password := ""
	if u.User != nil {
		password = u.User.Username()
	}
	nodeName := u.Fragment

	// Parse query params into Extensions
	ext := make(map[string]string)
	for k, v := range u.Query() {
		if len(v) > 0 {
			ext[k] = v[0]
		}
	}
	// Map standard query params to extension keys used by converters
	extensions := make(map[string]any)
	if sni, ok := ext["sni"]; ok {
		extensions["server_name"] = sni
	}
	if tp, ok := ext["type"]; ok {
		extensions["transport"] = tp
	}
	if path, ok := ext["path"]; ok {
		extensions["ws_path"] = path
	}
	if host, ok := ext["host"]; ok {
		extensions["ws_host"] = host
	}

	return contracts.SubscriptionSpec{
		URI:        raw,
		Protocol:   protocol,
		Host:       u.Hostname(),
		Port:       uint32(port),
		Password:   password,
		NodeName:   nodeName,
		Extensions: extensions,
	}, true
}

// parseVMessURI parses vmess:// URIs (base64-encoded JSON body).
func parseVMessURI(raw string) (contracts.SubscriptionSpec, bool) {
	// vmess://base64(json) — just keep the URI, CommonConverter handles it
	return contracts.SubscriptionSpec{
		URI:      raw,
		Protocol: contracts.ProtocolVMess,
	}, true
}

// parseShadowsocksURI parses ss:// URIs per SIP002/SIP022 spec.
// Formats:
//   - Stream/AEAD: ss://base64url(method:password)@host:port[#fragment]
//   - AEAD-2022:   ss://method:password@host:port[#fragment] (percent-encoded)
func parseShadowsocksURI(raw string) (contracts.SubscriptionSpec, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return contracts.SubscriptionSpec{}, false
	}

	port, _ := strconv.ParseUint(u.Port(), 10, 32)

	// Parse userinfo to extract method and password
	method := ""
	password := ""
	if u.User != nil {
		username := u.User.Username()
		if strings.HasPrefix(username, "2022-") {
			// AEAD-2022 (SIP022): userinfo is "method:password" (percent-encoded)
			method = username
			password, _ = u.User.Password() // already decoded by url.Parse
		} else {
			// Stream / AEAD: entire userinfo is base64url(method:password)
			// url.Parse treats the base64 string as Username (no real colon)
			userinfo := u.User.String()
			decoded, decErr := base64.RawURLEncoding.DecodeString(userinfo)
			if decErr != nil {
				decoded, decErr = base64.StdEncoding.DecodeString(userinfo)
			}
			if decErr == nil {
				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) == 2 {
					method = parts[0]
					password = parts[1]
				}
			}
		}
	}

	// Node name from URL fragment
	nodeName := ""
	if u.Fragment != "" {
		nodeName = u.Fragment // url.Parse already decodes percent-encoding
	}

	// Build extensions for converters
	extensions := make(map[string]any)
	if method != "" {
		extensions["method"] = method
	}

	return contracts.SubscriptionSpec{
		URI:        raw,
		Protocol:   contracts.ProtocolShadowsocks,
		Host:       u.Hostname(),
		Port:       uint32(port),
		Password:   password,
		NodeName:   nodeName,
		Extensions: extensions,
	}, true
}

// parseSnellURI parses a snell:// URI into a SubscriptionSpec.
// Format: snell://psk@host:port?version=5
// Returns (spec, true) on success, or (empty spec, false) on parse failure.
func parseSnellURI(raw string) (contracts.SubscriptionSpec, bool) {
	return parseStandardURI(raw, contracts.ProtocolSnell)
}
