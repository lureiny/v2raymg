package http

import (
	"context"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const fastAddInboundHelp = `POST /api/inbound/fast
	快速添加指定配置的inbound
	body: {"target":"", "tag":"", "protocol":"vless", "transport":"tcp", "domain":"", "security":"tls", "port":0, ...}
	protocol: vless, vmess, trojan, shadowsocks, hysteria2, tuic, anytls (hysteria2/tuic/anytls 仅 container=mihomo)
	transport: tcp, ws, h2(http), grpc, httpupgrade, xhttp, splithttp (也可用 stream 字段, 向后兼容)
	security: tls(默认), reality
	domain: 证书域名 (tls 时使用); 为空则自签
	reality_target, reality_server_names, reality_short_ids: Reality 参数
	flow: VLESS flow, 一般为 "xtls-rprx-vision" (配合 reality 使用)
	convenience fields: ws_path, grpc_service_name, http_path, http_host(逗号分隔), httpupgrade_path, xhttp_path, xhttp_mode, alpn, flow, sniffing_enabled
	hysteria2 (container=mihomo): obfs, obfs_password, up, down, masquerade, ignore_client_bandwidth
	tuic (container=mihomo): congestion_controller, udp_relay_mode, zero_rtt_handshake, heartbeat_interval(支持 "10s" / 纯毫秒数), disable_sni
	anytls (container=mihomo): padding_scheme(server-only inline 文本), idle_session_check_interval_seconds(int, 客户端), idle_session_timeout_seconds(int, 客户端), min_idle_session(int, 客户端)
	extra_params: map[string]string, 透传任意参数到 Executor (如 tls_min_version, tls_reject_unknown_sni, uuid, 以及尚未提升为便捷字段的协议参数)`

// FastAddInboundReq is the request payload for POST /api/inbound/fast and MCP tool fast_add_inbound.
type FastAddInboundReq struct {
	Target             string            `json:"target,omitempty" jsonschema:"Target node name, node_id, or 'all' (defaults to the local node when omitted)"`
	Tag                string            `json:"tag,omitempty" jsonschema:"Inbound tag name; auto-generated when omitted"`
	Protocol           string            `json:"protocol,omitempty" jsonschema:"Proxy protocol: 'vless' (default), 'vmess', 'trojan', 'ss', 'shadowsocks', 'hysteria2', 'tuic', or 'anytls' (hysteria2/tuic/anytls require container 'mihomo')"`
	Stream             string            `json:"stream,omitempty" jsonschema:"Legacy transport field kept for backwards compatibility ('tcp', 'ws', 'grpc', 'http'); prefer transport"`
	Transport          string            `json:"transport,omitempty" jsonschema:"Transport protocol: 'tcp' (default for pluggable protocols), 'ws', 'grpc', 'httpupgrade', 'xhttp', 'splithttp', 'h2' (or alias 'http'). Leave empty for QUIC-native hysteria2/tuic or TCP-native anytls."`
	Domain             string            `json:"domain,omitempty" jsonschema:"TLS certificate domain; when omitted under TLS security a self-signed certificate is generated"`
	IsXtls             bool              `json:"is_xtls,omitempty" jsonschema:"Deprecated XTLS flag; prefer setting the security field instead"`
	Port               int32             `json:"port,omitempty" jsonschema:"Listening port number; 0 auto-allocates an available port"`
	SelfSigned         bool              `json:"self_signed,omitempty" jsonschema:"Generate and use a self-signed TLS certificate"`
	Container          string            `json:"container,omitempty" jsonschema:"Target container type: 'xray' (default when omitted), 'mihomo', 'hysteria', or 'snell'"`
	Security           string            `json:"security,omitempty" jsonschema:"Security layer: 'tls' (default), 'reality', 'xtls', or 'none'"`
	RealityTarget      string            `json:"reality_target,omitempty" jsonschema:"Reality destination target (host:port) used when security is 'reality'"`
	RealityServerNames []string          `json:"reality_server_names,omitempty" jsonschema:"Allowed Reality SNI server names used when security is 'reality'"`
	RealityShortIDs    []string          `json:"reality_short_ids,omitempty" jsonschema:"Allowed Reality hex short IDs used when security is 'reality'"`
	ExtraParams        map[string]string `json:"extra_params,omitempty" jsonschema:"Additional key-value transport, security, sniffing, or protocol parameters passed through to the container executor (takes precedence over convenience fields)"`
	// Convenience fields that map into extra_params
	WSPath          string `json:"ws_path,omitempty" jsonschema:"WebSocket path (convenience alias for extra_params.ws_path)"`
	GRPCServiceName string `json:"grpc_service_name,omitempty" jsonschema:"gRPC service name (convenience alias for extra_params.grpc_service_name)"`
	HTTPPath        string `json:"http_path,omitempty" jsonschema:"HTTP/2 transport path (convenience alias for extra_params.http_path)"`
	HTTPHost        string `json:"http_host,omitempty" jsonschema:"Comma-separated HTTP/2 host list, e.g. 'host1.com,host2.com' (convenience alias for extra_params.http_host)"`
	HTTPUpgradePath string `json:"httpupgrade_path,omitempty" jsonschema:"HTTPUpgrade transport path (convenience alias for extra_params.httpupgrade_path)"`
	HTTPUpgradeHost string `json:"httpupgrade_host,omitempty" jsonschema:"HTTPUpgrade host header (convenience alias for extra_params.httpupgrade_host)"`
	XHTTPPath       string `json:"xhttp_path,omitempty" jsonschema:"XHTTP/SplitHTTP transport path (convenience alias for extra_params.xhttp_path)"`
	XHTTPMode       string `json:"xhttp_mode,omitempty" jsonschema:"XHTTP transport mode (convenience alias for extra_params.xhttp_mode)"`
	XHTTPHost       string `json:"xhttp_host,omitempty" jsonschema:"Comma-separated XHTTP host list (convenience alias for extra_params.xhttp_host)"`
	ALPN            string `json:"alpn,omitempty" jsonschema:"Comma-separated TLS ALPN protocols, e.g. 'h2,http/1.1' (convenience alias for extra_params.alpn)"`
	Flow            string `json:"flow,omitempty" jsonschema:"VLESS flow control, e.g. 'xtls-rprx-vision' (commonly used with Reality)"`
	SniffingEnabled bool   `json:"sniffing_enabled,omitempty" jsonschema:"Enable traffic sniffing on the inbound"`
	// SkipCertVerify:subscription-side override. true emits
	// skip-cert-verify on the client config regardless of cert source.
	SkipCertVerify bool `json:"skip_cert_verify,omitempty" jsonschema:"Subscription-side override emitting skip-cert-verify on client configs regardless of certificate source"`
	// Shadowsocks plugin convenience fields.
	Plugin         string `json:"plugin,omitempty" jsonschema:"Shadowsocks plugin name: 'obfs', 'v2ray-plugin', or 'shadow-tls'"`
	PluginMode     string `json:"plugin_mode,omitempty" jsonschema:"Shadowsocks plugin mode ('http' or 'tls' for obfs; 'websocket' or 'quic' for v2ray-plugin)"`
	PluginHost     string `json:"plugin_host,omitempty" jsonschema:"Shadowsocks plugin target host for obfs-local or shadow-tls"`
	PluginPath     string `json:"plugin_path,omitempty" jsonschema:"Shadowsocks v2ray-plugin path"`
	PluginTLS      bool   `json:"plugin_tls,omitempty" jsonschema:"Enable TLS for Shadowsocks v2ray-plugin"`
	PluginPassword string `json:"plugin_password,omitempty" jsonschema:"Password for Shadowsocks shadow-tls plugin"`
	PluginVersion  string `json:"plugin_version,omitempty" jsonschema:"Shadowsocks shadow-tls plugin version: '2' or '3'"`
	// Hysteria2 convenience fields. Map onto the protocolparams keys
	// of the same name (obfs/obfs_password/up/down/masquerade/
	// ignore_client_bandwidth).
	Obfs                  string `json:"obfs,omitempty" jsonschema:"Hysteria2 obfuscation mode: 'salamander' or empty"`
	ObfsPassword          string `json:"obfs_password,omitempty" jsonschema:"Hysteria2 obfuscation password (required when obfs is set)"`
	Up                    string `json:"up,omitempty" jsonschema:"Hysteria2 server upload bandwidth cap, e.g. '50 Mbps'"`
	Down                  string `json:"down,omitempty" jsonschema:"Hysteria2 server download bandwidth cap, e.g. '100 Mbps'"`
	Masquerade            string `json:"masquerade,omitempty" jsonschema:"Hysteria2 server-side masquerade decoy URL, reverse proxy, or file path"`
	IgnoreClientBandwidth bool   `json:"ignore_client_bandwidth,omitempty" jsonschema:"Hysteria2 flag to ignore client-advertised upload/download bandwidth limits"`
	// TUIC convenience fields. Map onto the protocolparams keys of the
	// same name. ZeroRTTHandshake / HeartbeatInterval / DisableSNI are
	// client-only knobs (mihomo listener has no field for them — they
	// reach the client via the subscription path); we accept them on
	// the listener-add request anyway so a single FastAdd call can pre-
	// populate the client-side hint.
	CongestionController string `json:"congestion_controller,omitempty" jsonschema:"TUIC congestion control algorithm: 'bbr', 'cubic', or 'new_reno'"`
	UDPRelayMode         string `json:"udp_relay_mode,omitempty" jsonschema:"TUIC UDP relay mode: 'native' or 'quic'"`
	ZeroRTTHandshake     bool   `json:"zero_rtt_handshake,omitempty" jsonschema:"TUIC client-only hint enabling 0-RTT handshake (reduce-rtt)"`
	HeartbeatInterval    string `json:"heartbeat_interval,omitempty" jsonschema:"TUIC client-only heartbeat interval, e.g. '10s' or milliseconds"`
	DisableSNI           bool   `json:"disable_sni,omitempty" jsonschema:"TUIC client-only flag disabling SNI"`
	// AnyTLS convenience fields. PaddingScheme is server-only inline
	// text; IdleSessionCheckIntervalSeconds / IdleSessionTimeoutSeconds /
	// MinIdleSession are client-only knobs that reach the subscriber via
	// Extensions. Empty / zero defers to mihomo runtime defaults.
	PaddingScheme                   string `json:"padding_scheme,omitempty" jsonschema:"AnyTLS server-only inline padding scheme text"`
	IdleSessionCheckIntervalSeconds int    `json:"idle_session_check_interval_seconds,omitempty" jsonschema:"AnyTLS client-only idle session check interval in seconds (0 uses runtime default)"`
	IdleSessionTimeoutSeconds       int    `json:"idle_session_timeout_seconds,omitempty" jsonschema:"AnyTLS client-only idle session timeout in seconds (0 uses runtime default)"`
	MinIdleSession                  int    `json:"min_idle_session,omitempty" jsonschema:"AnyTLS client-only minimum idle session count (0 uses runtime default)"`
}

type FastAddInboundHandler struct{ HttpHandlerImp }

// fastAddValidProtocols is the canonical whitelist for POST /api/inbound/fast's
// protocol field. Must stay in lockstep with getBuilderType below and with
// end_node_inbound.go's InboundBuilderType switch — any protocol listed here
// needs a builder mapping and a downstream case, or the request 500s at the
// RPC layer; any protocol the downstream recognises that is missing here gets
// 400'd by the handler before ever reaching the RPC. Exported at package level
// so tests can assert the three-layer alignment directly (see
// TestFastAddValidProtocolsMatchesGetBuilderType).
var fastAddValidProtocols = map[string]bool{
	"vless":       true,
	"vmess":       true,
	"trojan":      true,
	"ss":          true,
	"shadowsocks": true,
	"hysteria2":   true,
	"tuic":        true,
	"anytls":      true,
}

func getBuilderType(key string) proto.BuilderType {
	switch strings.ToLower(key) {
	case "vless":
		return proto.BuilderType_VLESSSettingBuilderType
	case "vmess":
		return proto.BuilderType_VMESSSettingBuilderType
	case "trojan":
		return proto.BuilderType_TrojanSettingBuilderType
	case "ss", "shadowsocks":
		return proto.BuilderType_SSSettingBuilderType
	case "hysteria2":
		return proto.BuilderType_Hysteria2SettingBuilderType
	case "tuic":
		return proto.BuilderType_TUICSettingBuilderType
	case "anytls":
		return proto.BuilderType_AnyTLSSettingBuilderType
	case "tcp":
		return proto.BuilderType_TCPBuilderType
	case "ws":
		return proto.BuilderType_WSBuilderType
	case "quic":
		return proto.BuilderType_QuicBuilderType
	case "mkcp":
		return proto.BuilderType_MkcpBuilderType
	case "grpc":
		return proto.BuilderType_GrpcBuilderType
	case "http":
		return proto.BuilderType_HttpBuilderType
	default:
		return proto.BuilderType_UnknowBuilderType
	}
}

func (s *HttpServer) fastAddInboundEndpoint() Endpoint[FastAddInboundReq, map[string]any] {
	return Endpoint[FastAddInboundReq, map[string]any]{
		Method:         "POST",
		Path:           "/inbound/fast",
		Scope:          ScopeAdmin,
		Help:           fastAddInboundHelp,
		MCPName:        "fast_add_inbound",
		MCPDescription: "Quickly create and add a proxy inbound on the target cluster node(s) using high-level protocol, transport, security, and container options. Supports protocols vless, vmess, trojan, ss/shadowsocks, hysteria2, tuic, and anytls across xray, mihomo, hysteria, and snell containers (hysteria2/tuic/anytls require container 'mihomo'). Target defaults to the local node when omitted.",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req FastAddInboundReq) (map[string]any, error) {
			req.Target = resolveTarget(req.Target, s.Name)
			if req.Protocol == "" {
				req.Protocol = "vless"
			}
			// Resolve transport: prefer "transport" field, fall back to "stream"
			transport := req.Transport
			if transport == "" {
				transport = req.Stream
			}
			// Only default to tcp for protocols that actually use a pluggable transport.
			// hysteria2 and tuic are QUIC-native; anytls is TCP-native with no
			// selectable transport — passing "tcp" triggers a validation error downstream.
			if transport == "" {
				switch strings.ToLower(req.Protocol) {
				case "hysteria2", "tuic", "anytls":
					// leave transport empty
				default:
					transport = "tcp"
				}
			}

			if !fastAddValidProtocols[strings.ToLower(req.Protocol)] {
				return nil, NewEndpointErr(400, fmt.Sprintf("unsupported protocol: %s", req.Protocol))
			}
			// Normalize transport aliases.
			// "quic" is the native transport for hysteria2/tuic — equivalent to leaving it empty.
			// "tcp" is the native transport for anytls — also equivalent to leaving it empty.
			if transport == "quic" {
				switch strings.ToLower(req.Protocol) {
				case "hysteria2", "tuic":
					transport = ""
				}
			}
			if transport == "tcp" && strings.ToLower(req.Protocol) == "anytls" {
				transport = ""
			}
			if transport == "http" {
				transport = "h2"
			}
			// Validate transport (skip for empty, which signals "protocol-native, no transport layer")
			validTransports := map[string]bool{
				"tcp": true, "ws": true, "grpc": true, "httpupgrade": true,
				"xhttp": true, "splithttp": true, "h2": true,
			}
			if transport != "" && !validTransports[transport] {
				if transport == "h3" {
					return nil, NewEndpointErr(400, fmt.Sprintf("transport %q is not supported; use xhttp or splithttp instead", transport))
				}
				return nil, NewEndpointErr(400, fmt.Sprintf("unsupported transport: %s", transport))
			}

			nodes := s.GetTargetNodes(req.Target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}

			// Build extra_params: merge explicit fields + caller-provided map
			extra := make(map[string]string)
			if req.ExtraParams != nil {
				for k, v := range req.ExtraParams {
					extra[k] = v
				}
			}
			// Map convenience fields into extra_params (don't overwrite explicit ones)
			convFields := map[string]string{
				"ws_path": req.WSPath, "grpc_service_name": req.GRPCServiceName,
				"http_path": req.HTTPPath, "http_host": req.HTTPHost,
				"httpupgrade_path": req.HTTPUpgradePath, "httpupgrade_host": req.HTTPUpgradeHost,
				"xhttp_path": req.XHTTPPath, "xhttp_mode": req.XHTTPMode, "xhttp_host": req.XHTTPHost,
				"alpn": req.ALPN,
				"flow": req.Flow,
				// Shadowsocks plugin
				"plugin": req.Plugin, "plugin_mode": req.PluginMode,
				"plugin_host": req.PluginHost, "plugin_path": req.PluginPath,
				"plugin_password": req.PluginPassword, "plugin_version": req.PluginVersion,
				// Hysteria2
				"obfs": req.Obfs, "obfs_password": req.ObfsPassword,
				"up": req.Up, "down": req.Down,
				"masquerade": req.Masquerade,
				// TUIC
				"congestion_controller": req.CongestionController,
				"udp_relay_mode":        req.UDPRelayMode,
				"heartbeat_interval":    req.HeartbeatInterval,
				// AnyTLS (string fields only — int fields handled below)
				"padding_scheme": req.PaddingScheme,
			}
			for k, v := range convFields {
				if v != "" {
					if _, exists := extra[k]; !exists {
						extra[k] = v
					}
				}
			}
			if req.SniffingEnabled {
				if _, exists := extra["sniffing_enabled"]; !exists {
					extra["sniffing_enabled"] = "true"
				}
			}
			if req.SelfSigned {
				if _, exists := extra["self_signed"]; !exists {
					extra["self_signed"] = "true"
				}
			}
			if req.SkipCertVerify {
				if _, exists := extra["skip_cert_verify"]; !exists {
					extra["skip_cert_verify"] = "true"
				}
			}
			if req.PluginTLS {
				extra["plugin_tls"] = "true"
			}
			if req.IgnoreClientBandwidth {
				extra["ignore_client_bandwidth"] = "true"
			}
			if req.ZeroRTTHandshake {
				if _, exists := extra["zero_rtt_handshake"]; !exists {
					extra["zero_rtt_handshake"] = "true"
				}
			}
			if req.DisableSNI {
				if _, exists := extra["disable_sni"]; !exists {
					extra["disable_sni"] = "true"
				}
			}
			// AnyTLS int convenience fields. Skip zeros so mihomo's runtime fallback
			// (≤5s → 30s, see session/client.go:46-51) can take over.
			if req.IdleSessionCheckIntervalSeconds > 0 {
				if _, exists := extra["idle_session_check_interval_seconds"]; !exists {
					extra["idle_session_check_interval_seconds"] = fmt.Sprintf("%d", req.IdleSessionCheckIntervalSeconds)
				}
			}
			if req.IdleSessionTimeoutSeconds > 0 {
				if _, exists := extra["idle_session_timeout_seconds"]; !exists {
					extra["idle_session_timeout_seconds"] = fmt.Sprintf("%d", req.IdleSessionTimeoutSeconds)
				}
			}
			if req.MinIdleSession > 0 {
				if _, exists := extra["min_idle_session"]; !exists {
					extra["min_idle_session"] = fmt.Sprintf("%d", req.MinIdleSession)
				}
			}

			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.FastAddInboundType, &proto.FastAddInboundReq{
				InboundBuilderType: getBuilderType(req.Protocol),
				StreamBuilderType:  getBuilderType(transport), // legacy field for backward compat
				Transport:          transport,                 // new string field
				Port:               req.Port,
				Domain:             req.Domain,
				IsXtls:             req.IsXtls,
				Tag:                req.Tag,
				ContainerType:      req.Container,
				Security:           req.Security,
				RealityTarget:      req.RealityTarget,
				RealityServerNames: req.RealityServerNames,
				RealityShortIds:    req.RealityShortIDs,
				ExtraParams:        extra,
			}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|Target=%s", errMsg, req.Target)
				return nil, NewEndpointErr(500, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

func (handler *FastAddInboundHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().fastAddInboundEndpoint())(c)
}

func (handler *FastAddInboundHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *FastAddInboundHandler) getRelativePath() string { return "/inbound/fast" }

func (handler *FastAddInboundHandler) help() string {
	return fastAddInboundHelp
}
