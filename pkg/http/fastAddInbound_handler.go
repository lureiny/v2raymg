package http

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

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

func (handler *FastAddInboundHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target             string            `json:"target"`
		Tag                string            `json:"tag"`
		Protocol           string            `json:"protocol"`
		Stream             string            `json:"stream"`    // legacy: "tcp","ws","grpc","http"
		Transport          string            `json:"transport"` // preferred: "tcp","ws","grpc","httpupgrade","xhttp","splithttp"
		Domain             string            `json:"domain"`
		IsXtls             bool              `json:"is_xtls"` // deprecated: use security field
		Port               int32             `json:"port"`
		SelfSigned         bool              `json:"self_signed"`
		Container          string            `json:"container"`
		Security           string            `json:"security"`
		RealityTarget      string            `json:"reality_target"`
		RealityServerNames []string          `json:"reality_server_names"`
		RealityShortIDs    []string          `json:"reality_short_ids"`
		ExtraParams        map[string]string `json:"extra_params"` // transport/security/sniffing params
		// Convenience fields that map into extra_params
		WSPath          string `json:"ws_path,omitempty"`
		GRPCServiceName string `json:"grpc_service_name,omitempty"`
		HTTPPath        string `json:"http_path,omitempty"`
		HTTPHost        string `json:"http_host,omitempty"` // comma-separated, e.g. "host1.com,host2.com"
		HTTPUpgradePath string `json:"httpupgrade_path,omitempty"`
		HTTPUpgradeHost string `json:"httpupgrade_host,omitempty"`
		XHTTPPath       string `json:"xhttp_path,omitempty"`
		XHTTPMode       string `json:"xhttp_mode,omitempty"`
		XHTTPHost       string `json:"xhttp_host,omitempty"`
		ALPN            string `json:"alpn,omitempty"` // comma-separated, e.g. "h2,http/1.1"
		Flow            string `json:"flow,omitempty"` // vless flow, e.g. "xtls-rprx-vision"
		SniffingEnabled bool   `json:"sniffing_enabled,omitempty"`
		// SkipCertVerify:subscription-side override. true emits
		// skip-cert-verify on the client config regardless of cert source.
		SkipCertVerify bool `json:"skip_cert_verify,omitempty"`
		// Shadowsocks plugin convenience fields.
		Plugin         string `json:"plugin,omitempty"`          // "obfs" | "v2ray-plugin" | "shadow-tls"
		PluginMode     string `json:"plugin_mode,omitempty"`     // obfs: "http"|"tls"; v2ray-plugin: "websocket"|"quic"
		PluginHost     string `json:"plugin_host,omitempty"`     // obfs-local/shadow-tls target host
		PluginPath     string `json:"plugin_path,omitempty"`     // v2ray-plugin path
		PluginTLS      bool   `json:"plugin_tls,omitempty"`      // v2ray-plugin TLS flag
		PluginPassword string `json:"plugin_password,omitempty"` // shadow-tls password
		PluginVersion  string `json:"plugin_version,omitempty"`  // shadow-tls version: "2" or "3"
		// Hysteria2 convenience fields. Map onto the protocolparams keys
		// of the same name (obfs/obfs_password/up/down/masquerade/
		// ignore_client_bandwidth).
		Obfs                  string `json:"obfs,omitempty"`                    // "salamander" or empty
		ObfsPassword          string `json:"obfs_password,omitempty"`           // required when obfs is set
		Up                    string `json:"up,omitempty"`                      // e.g. "50 Mbps"
		Down                  string `json:"down,omitempty"`                    // e.g. "100 Mbps"
		Masquerade            string `json:"masquerade,omitempty"`              // server-side decoy URL/proxy/file
		IgnoreClientBandwidth bool   `json:"ignore_client_bandwidth,omitempty"` // hy2: ignore client up/down adv
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)
	if req.Protocol == "" {
		req.Protocol = "vless"
	}
	// Resolve transport: prefer "transport" field, fall back to "stream"
	transport := req.Transport
	if transport == "" {
		transport = req.Stream
	}
	if transport == "" {
		transport = "tcp"
	}

	if !fastAddValidProtocols[strings.ToLower(req.Protocol)] {
		jsonErr(c, 400, fmt.Sprintf("unsupported protocol: %s", req.Protocol))
		return
	}
	// Normalize transport alias: http -> h2
	if transport == "http" {
		transport = "h2"
	}
	// Validate transport
	validTransports := map[string]bool{
		"tcp": true, "ws": true, "grpc": true, "httpupgrade": true,
		"xhttp": true, "splithttp": true, "h2": true,
	}
	if !validTransports[transport] {
		if transport == "h3" {
			jsonErr(c, 400, fmt.Sprintf("transport %q is not supported; use xhttp or splithttp instead", transport))
		} else {
			jsonErr(c, 400, fmt.Sprintf("unsupported transport: %s", transport))
		}
		return
	}

	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
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

	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.FastAddInboundType, &proto.FastAddInboundReq{
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
	}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Target=%s", errMsg, req.Target)
		jsonErr(c, 500, errMsg)
		return
	}
	jsonOK(c)
}

func (handler *FastAddInboundHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *FastAddInboundHandler) getRelativePath() string { return "/inbound/fast" }

func (handler *FastAddInboundHandler) help() string {
	return `POST /api/inbound/fast
	快速添加指定配置的inbound
	body: {"target":"", "tag":"", "protocol":"vless", "transport":"tcp", "domain":"", "security":"tls", "port":0, ...}
	protocol: vless, vmess, trojan, shadowsocks, hysteria2 (hysteria2 仅 container=mihomo; tuic/anytls 计划分阶段接入)
	transport: tcp, ws, h2(http), grpc, httpupgrade, xhttp, splithttp (也可用 stream 字段, 向后兼容)
	security: tls(默认), reality
	domain: 证书域名 (tls 时使用); 为空则自签
	reality_target, reality_server_names, reality_short_ids: Reality 参数
	flow: VLESS flow, 一般为 "xtls-rprx-vision" (配合 reality 使用)
	convenience fields: ws_path, grpc_service_name, http_path, http_host(逗号分隔), httpupgrade_path, xhttp_path, xhttp_mode, alpn, flow, sniffing_enabled
	hysteria2 (container=mihomo): obfs, obfs_password, up, down, masquerade, ignore_client_bandwidth
	extra_params: map[string]string, 透传任意参数到 Executor (如 tls_min_version, tls_reject_unknown_sni, uuid, 以及尚未提升为便捷字段的协议参数)`
}
