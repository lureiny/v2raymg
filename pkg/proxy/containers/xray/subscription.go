package xray

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/errors"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
)

// isUserNotFoundError checks if the error indicates user doesn't exist in this inbound.
// Returns true only for "user not found" semantic errors that should be skipped.
// Match pattern: "user %s not found in inbound %s" from XrayInbound.GetSub
func isUserNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	// Must start with "user " to distinguish from credential errors like
	// "default client uuid not found for inbound %s"
	return strings.HasPrefix(errMsg, "user ") && strings.Contains(errMsg, "not found") && strings.Contains(errMsg, "inbound")
}

// Verify Executor implements SubscriptionProvider.
var _ container.SubscriptionProvider = (*Executor)(nil)

// UserPortMapperGetter is the interface to get UserPortMapper for subscription port lookup.
type UserPortMapperGetter interface {
	GetUserPortMapper() usermanager.UserPortMapper
}

// GetUserSubscriptions generates subscription specs for a user across all inbounds.
// Each inbound that has the user in its internal mapping produces one SubscriptionSpec with a populated URI.
// This uses UserManager's port mapping as the authoritative source for forward ports.
func (e *Executor) GetUserSubscriptions(req contracts.SubscriptionRequest) ([]contracts.SubscriptionSpec, error) {
	if req.User.Username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if req.Host == "" {
		return nil, fmt.Errorf("host is required")
	}

	e.inboundsMu.RLock()
	defer e.inboundsMu.RUnlock()

	// Collect inbounds into a slice
	inboundList := make([]*XrayInbound, 0, len(e.inbounds))
	for _, in := range e.inbounds {
		inboundList = append(inboundList, in)
	}

	var specs []contracts.SubscriptionSpec
	for _, in := range inboundList {
		// Use the inbound's GetSub method which uses the internal mapping
		spec, err := in.GetSub(req)
		if err != nil {
			// Error classification:
			// - "user not found" in this inbound: skip (user may not be on all inbounds)
			// - Other errors (credential missing, port mapping failed, URI generation failed, internal error): fail-fast
			if isUserNotFoundError(err) {
				continue
			}
			// Wrap error with inbound tag context and return immediately
			return nil, fmt.Errorf("GetSub failed for inbound %s: %w", in.tag, err)
		}
		specs = append(specs, spec)
	}

	return specs, nil
}

// generateSubscriptionSpec builds a SubscriptionSpec for one inbound + one user.
// It uses UserPortMapper to get the forward port - MUST use forward port, no fallback to backend port.
// Returns error if user has no forward port mapping in usermanager.
// The index parameter is used to select the correct bind port when user has multiple forward ports.
func generateSubscriptionSpec(in *XrayInbound, req contracts.SubscriptionRequest, userMgr *usermanager.UserManager, index int, bindPorts []uint32) (contracts.SubscriptionSpec, error) {
	port := req.Port

	// UserManager is REQUIRED for subscription generation
	// This is the "upper layer multi-user" approach via forward port mapping
	if userMgr == nil {
		return contracts.SubscriptionSpec{}, errors.New(errors.ErrUserManagerRequired,
			"usermanager is required to generate subscription; container must have usermanager set")
	}

	// Get the forward port from UserManager
	// Support multiple bind ports: use index to select the correct port
	var forwardPort uint32
	var found bool
	if len(bindPorts) > 0 {
		// Use index to select port, fallback to first port if index out of range
		if index < len(bindPorts) {
			forwardPort = bindPorts[index]
			found = forwardPort != 0
		} else if len(bindPorts) > 0 {
			// Fallback to first port if index out of range
			forwardPort = bindPorts[0]
			found = forwardPort != 0
		}
	} else {
		// Fallback to GetUserPort for backward compatibility
		forwardPort, found = userMgr.GetUserPort(req.User.Username)
	}

	if found && forwardPort != 0 {
		// Use forward port if available
		// Only override if req.Port was not explicitly specified
		if req.Port == 0 {
			port = forwardPort
		}
	} else {
		// No forward port mapping found - this is an error
		// We do NOT fallback to inbound backend port
		return contracts.SubscriptionSpec{}, errors.New(errors.ErrUserForwardPortEmpty,
			fmt.Sprintf("user %s on inbound %s has no forward port mapping", req.User.Username, in.tag))
	}

	password, err := extractCredential(in, req.User)
	if err != nil {
		return contracts.SubscriptionSpec{}, err
	}

	extensions := buildSubscriptionExtensions(in, req.User)

	nodeName := req.NodeName
	if nodeName == "" {
		nodeName = in.tag
	}

	spec := contracts.SubscriptionSpec{
		Protocol:   in.protocol,
		Host:       req.Host,
		Port:       port,
		Password:   password,
		NodeName:   nodeName,
		InboundTag: in.tag,
		Username:  req.User.Username,
		Extensions: extensions,
	}

	uri, err := generateURI(spec)
	if err != nil {
		return contracts.SubscriptionSpec{}, err
	}
	spec.URI = uri

	return spec, nil
}

// extractCredential extracts the user credential for the given protocol.
// For VMess/VLESS, uuid is read from inbound's default client (not from user extensions).
// For Trojan/SS/SOCKS5, password is read from inbound's internal default password (not from user extensions).
// This ensures all users on the same inbound share the same credential source.
func extractCredential(in *XrayInbound, user contracts.UserSpec) (string, error) {
	switch in.protocol {
	case contracts.ProtocolVMess, contracts.ProtocolVLess:
		// UUID MUST come from inbound's default client - user.extensions.uuid is ignored
		uuid := in.defaultClientUUID
		if uuid == "" {
			return "", fmt.Errorf("default client uuid not found for inbound %s", in.tag)
		}
		return uuid, nil
	case contracts.ProtocolTrojan, contracts.ProtocolShadowsocks:
		// Password MUST come from inbound's internal default password
		// This unifies credential source: inbound stores the password from native settings
		password := in.defaultPassword
		if password == "" {
			return "", fmt.Errorf("default password not found for inbound %s", in.tag)
		}
		return password, nil
	case contracts.ProtocolSOCKS5:
		// SOCKS5 supports per-user auth; auth token is used as the SOCKS5 password.
		if user.AuthToken != "" {
			return user.AuthToken, nil
		}
		return "", fmt.Errorf("password not found for user %s", user.Username)
	default:
		return "", fmt.Errorf("unsupported protocol for subscription: %s", in.protocol)
	}
}

// buildSubscriptionExtensions extracts inbound config fields relevant for subscription URI.
func buildSubscriptionExtensions(in *XrayInbound, user contracts.UserSpec) map[string]any {
	ext := make(map[string]any)
	ext["transport"] = string(in.transport)
	ext["security"] = string(in.security)

	// Copy transport/TLS fields from inbound extra
	if in.extra != nil {
		copyKeys := []string{
			"ws_path", "ws_host",
			"grpc_service_name", "grpc_mode",
			"server_name", "alpn", "utls_fingerprint",
			"mkcp_header_type", "mkcp_seed",
			"quic_security", "quic_key", "quic_header_type",
			"http_path", "http_host",
			"method", "ss_plugin", "ss_plugin_opts",
			"flow",
		}
		for _, key := range copyKeys {
			if v, ok := in.extra[key]; ok {
				ext[key] = v
			}
		}
	}

	return ext
}

// generateURI dispatches to protocol-specific URI generators.
func generateURI(spec contracts.SubscriptionSpec) (string, error) {
	switch spec.Protocol {
	case contracts.ProtocolVLess:
		return generateVLESSURI(spec)
	case contracts.ProtocolVMess:
		return generateVMessURI(spec)
	case contracts.ProtocolTrojan:
		return generateTrojanURI(spec)
	case contracts.ProtocolShadowsocks:
		return generateShadowsocksURI(spec)
	case contracts.ProtocolSOCKS5:
		return generateSOCKS5URI(spec)
	default:
		return "", fmt.Errorf("unsupported protocol for subscription URI: %s", spec.Protocol)
	}
}

// generateVLESSURI builds a VLESS share link.
// Format: vless://UUID@host:port?type=...&security=...&...#nodename
// Aligned with Xray-core issue #91: https://github.com/XTLS/Xray-core/issues/91
func generateVLESSURI(spec contracts.SubscriptionSpec) (string, error) {
	params := buildShareLinkParams(spec)

	// Add flow if specified, BUT NOT for grpc/xhttp/splithttp/h3 transports
	// grpc/xhttp/splithttp/h3 + reality must have empty flow in URI
	// Per Xray-examples: VLESS + gRPC + REALITY should NOT have flow
	transport := extString(spec.Extensions, "transport")
	security := extString(spec.Extensions, "security")
	if flow := extString(spec.Extensions, "flow"); flow != "" {
		isGRPCOrXHTTP := transport == "grpc" || transport == "xhttp" || transport == "splithttp" || transport == "h3"
		// For grpc/xhttp/splithttp/h3 + reality, don't output flow
		if !(security == "reality" && isGRPCOrXHTTP) {
			params = append(params, "flow="+url.QueryEscape(flow))
		}
	}

	// Add Reality-specific params (issue #91)
	if security == "reality" {
		if serverNames := extString(spec.Extensions, "reality_server_names"); serverNames != "" {
			params = append(params, "serverNames="+serverNames)
		}
		if shortID := extString(spec.Extensions, "reality_short_ids"); shortID != "" {
			params = append(params, "sid="+shortID)
		}
	}

	// Build query string (filter out empty params)
	var queryParts []string
	for _, p := range params {
		if p != "" {
			queryParts = append(queryParts, p)
		}
	}

	// Issue #91: UUID should NOT be URL-encoded for compatibility
	// Note: VLESS encryption defaults to "none", no need to specify explicitly
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s",
		spec.Password,
		spec.Host,
		spec.Port,
		strings.Join(queryParts, "&"),
		url.QueryEscape(spec.NodeName),
	), nil
}

// generateVMessURI builds a VMess share link.
// Format: vmess://base64(JSON)
// Aligned with Xray-core issue #91: https://github.com/XTLS/Xray-core/issues/91
func generateVMessURI(spec contracts.SubscriptionSpec) (string, error) {
	transport := extString(spec.Extensions, "transport")
	security := extString(spec.Extensions, "security")

	// Issue #91: Only include aid when explicitly set (deprecated field)
	var aid uint32
	var hasAid bool
	if v, ok := spec.Extensions["alter_id"]; ok {
		switch val := v.(type) {
		case uint32:
			aid = val
			hasAid = val > 0
		case int:
			aid = uint32(val)
			hasAid = val > 0
		case float64:
			aid = uint32(val)
			hasAid = val > 0
		}
	}

	cfg := map[string]interface{}{
		"v":   "2",
		"ps":  spec.NodeName,
		"add": spec.Host,
		// Issue #91: Port must be integer (not string)
		"port": spec.Port,
		"id":   spec.Password,
		"net":  vmessTransportName(transport),
		// Issue #91: type defaults to "none", encryption defaults to "auto"
		"type":       "none",
		"encryption": "auto",
		"tls":        security,
	}

	// Issue #91: Only include aid when explicitly set (deprecated)
	if hasAid {
		cfg["aid"] = aid
	}

	// SNI logic: only output SNI when necessary (same as buildShareLinkParams)
	certSource := extString(spec.Extensions, "cert_source")
	sni := extString(spec.Extensions, "server_name")
	hostIsIP := net.ParseIP(spec.Host) != nil
	if sni != "" && (certSource == "self_signed" || hostIsIP) {
		cfg["sni"] = sni
	}

	switch transport {
	case "ws":
		cfg["host"] = extString(spec.Extensions, "ws_host")
		cfg["path"] = extString(spec.Extensions, "ws_path")
	case "grpc":
		cfg["path"] = extString(spec.Extensions, "grpc_service_name")
	case "http":
		cfg["net"] = "h2"
		cfg["host"] = extString(spec.Extensions, "http_host")
		cfg["path"] = extString(spec.Extensions, "http_path")
	case "mkcp":
		cfg["net"] = "kcp"
		cfg["type"] = extString(spec.Extensions, "mkcp_header_type")
		cfg["path"] = extString(spec.Extensions, "mkcp_seed")
	case "quic":
		cfg["host"] = extString(spec.Extensions, "quic_security")
		cfg["path"] = extString(spec.Extensions, "quic_key")
		cfg["type"] = extString(spec.Extensions, "quic_header_type")
	}

	b, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to marshal vmess config: %w", err)
	}

	return fmt.Sprintf("vmess://%s", base64.RawURLEncoding.EncodeToString(b)), nil
}

// generateTrojanURI builds a Trojan share link.
// Format: trojan://password@host:port?type=...&security=...&...#nodename
func generateTrojanURI(spec contracts.SubscriptionSpec) (string, error) {
	params := buildShareLinkParams(spec)

	// Add flow if specified, BUT NOT for xhttp/splithttp/h3 transports
	transport := extString(spec.Extensions, "transport")
	if flow := extString(spec.Extensions, "flow"); flow != "" {
		isXHTTPFamily := transport == "xhttp" || transport == "splithttp" || transport == "h3"
		if !isXHTTPFamily {
			params = append(params, "flow="+flow)
		}
	}

	return fmt.Sprintf("trojan://%s@%s:%d?%s#%s",
		url.QueryEscape(spec.Password),
		spec.Host,
		spec.Port,
		strings.Join(params, "&"),
		url.QueryEscape(spec.NodeName),
	), nil
}

// generateShadowsocksURI builds a Shadowsocks SIP002 share link.
// Format: ss://base64url(method:password)@hostname:port[/][?plugin=...][#tag]
// For AEAD-2022 ciphers (method starts with "2022-"), uses plain-text userinfo
// with percent-encoding per SIP002/SIP022 spec.
func generateShadowsocksURI(spec contracts.SubscriptionSpec) (string, error) {
	method := extString(spec.Extensions, "method")
	if method == "" {
		method = "aes-256-gcm" // xray default
	}

	var sb strings.Builder
	sb.WriteString("ss://")

	if strings.HasPrefix(method, "2022-") {
		// AEAD-2022 (SIP022): plain-text userinfo, percent-encoded
		sb.WriteString(url.QueryEscape(method))
		sb.WriteString(":")
		sb.WriteString(url.QueryEscape(spec.Password))
	} else {
		// Stream / AEAD: base64url-encode(method:password)
		userinfo := method + ":" + spec.Password
		sb.WriteString(base64.RawURLEncoding.EncodeToString([]byte(userinfo)))
	}

	sb.WriteString("@")
	sb.WriteString(spec.Host)
	sb.WriteString(fmt.Sprintf(":%d", spec.Port))

	// SIP003 plugin support
	plugin := extString(spec.Extensions, "ss_plugin")
	if plugin != "" {
		pluginStr := plugin
		if pluginOpts := extString(spec.Extensions, "ss_plugin_opts"); pluginOpts != "" {
			pluginStr += ";" + pluginOpts
		}
		sb.WriteString("/?plugin=")
		sb.WriteString(url.QueryEscape(pluginStr))
	}

	// Fragment: tag
	if spec.NodeName != "" {
		sb.WriteString("#")
		sb.WriteString(url.QueryEscape(spec.NodeName))
	}

	return sb.String(), nil
}

// generateSOCKS5URI builds a SOCKS5 share link.
// Format: socks://username:password@host:port#nodename
// If no username is provided in extensions, uses empty username.
func generateSOCKS5URI(spec contracts.SubscriptionSpec) (string, error) {
	username := extString(spec.Extensions, "username")

	// Build userinfo: username:password (base64 encoded)
	userinfo := username + ":" + spec.Password
	encodedUserinfo := base64.StdEncoding.EncodeToString([]byte(userinfo))

	return fmt.Sprintf("socks://%s@%s:%d#%s",
		encodedUserinfo,
		spec.Host,
		spec.Port,
		url.QueryEscape(spec.NodeName),
	), nil
}

// buildShareLinkParams builds the query parameters for VLESS/Trojan URIs.
// buildShareLinkParams builds the query parameters for VLESS/Trojan URIs.
// Aligned with Xray-core issue #91: https://github.com/XTLS/Xray-core/issues/91
func buildShareLinkParams(spec contracts.SubscriptionSpec) []string {
	var params []string

	transport := extString(spec.Extensions, "transport")
	security := extString(spec.Extensions, "security")

	// Issue #91: Always include type (consistent with existing behavior)
	if transport != "" {
		params = append(params, "type="+transport)
	}
	// Issue #91: Only add security if explicitly set (VLESS defaults to none, Trojan to tls)
	// This removes redundant security=none for VLESS
	if security != "" {
		params = append(params, "security="+security)
	}

	// Transport-specific params
	switch transport {
	case "ws":
		if p := extString(spec.Extensions, "ws_path"); p != "" {
			params = append(params, "path="+p)
		}
		if h := extString(spec.Extensions, "ws_host"); h != "" {
			params = append(params, "host="+h)
		}
	case "grpc":
		if sn := extString(spec.Extensions, "grpc_service_name"); sn != "" {
			params = append(params, "serviceName="+sn)
		}
		if m := extString(spec.Extensions, "grpc_mode"); m != "" {
			params = append(params, "mode="+m)
		}
	case "http":
		if h := extString(spec.Extensions, "http_host"); h != "" {
			params = append(params, "host="+h)
		}
		if p := extString(spec.Extensions, "http_path"); p != "" {
			params = append(params, "path="+p)
		}
	case "mkcp":
		if ht := extString(spec.Extensions, "mkcp_header_type"); ht != "" {
			params = append(params, "headerType="+ht)
		}
		if seed := extString(spec.Extensions, "mkcp_seed"); seed != "" {
			params = append(params, "seed="+seed)
		}
	case "quic":
		qs := extString(spec.Extensions, "quic_security")
		if qs != "" && qs != "none" {
			params = append(params, "quicSecurity="+qs)
			if key := extString(spec.Extensions, "quic_key"); key != "" {
				params = append(params, "key="+key)
			}
		}
		if ht := extString(spec.Extensions, "quic_header_type"); ht != "" {
			params = append(params, "headerType="+ht)
		}
	}

	// TLS/XTLS/Reality params
	if security == "tls" || security == "xtls" || security == "reality" {
		// SNI logic: only output SNI when necessary
		// - self_signed cert: always need SNI
		// - host is IP: need SNI (IP cannot be used as SNI by client)
		// - formal cert + domain host: NO SNI (client uses host automatically)
		certSource := extString(spec.Extensions, "cert_source")
		sni := extString(spec.Extensions, "server_name")
		hostIsIP := net.ParseIP(spec.Host) != nil

		// Only add SNI if:
		// 1. cert_source is "self_signed", OR
		// 2. host is an IP address
		// Otherwise, let client use host as SNI naturally
		if sni != "" && (certSource == "self_signed" || hostIsIP) {
			params = append(params, "sni="+sni)
		}
		// Issue #91: ALPN comma-separated list
		if alpnStr := extString(spec.Extensions, "alpn"); alpnStr != "" {
			params = append(params, "alpn="+alpnStr)
		} else if alpnSlice := extStringSlice(spec.Extensions, "alpn"); len(alpnSlice) > 0 {
			params = append(params, "alpn="+strings.Join(alpnSlice, ","))
		}
		// Issue #91: uTLS fingerprint parameter
		if fp := extString(spec.Extensions, "utls_fingerprint"); fp != "" {
			params = append(params, "fp="+fp)
		}
	}

	// Reality-specific params (issue #91)
	if security == "reality" {
		// Public key for Reality (required for client)
		if pbk := extString(spec.Extensions, "reality_public_key"); pbk != "" {
			params = append(params, "pbk="+url.QueryEscape(pbk))
		}
	}

	return params
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

func extStringSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	if v, ok := m[key]; ok {
		if s, ok := v.([]string); ok {
			return s
		}
	}
	return nil
}

func vmessTransportName(transport string) string {
	switch transport {
	case "http":
		return "h2"
	case "mkcp":
		return "kcp"
	default:
		return transport
	}
}

// GenerateShadowsocksURITest is an exported wrapper for testing and demo purposes.
// It generates a Shadowsocks SIP002 share link from a SubscriptionSpec.
func GenerateShadowsocksURITest(spec contracts.SubscriptionSpec) (string, error) {
	return generateShadowsocksURI(spec)
}
