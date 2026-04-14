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

func checkBuilder(protocol, stream string) error {
	if builderType := getBuilderType(protocol); builderType == proto.BuilderType_UnknowBuilderType {
		return fmt.Errorf("unsopport protocol: %s", protocol)
	}
	if builderType := getBuilderType(stream); builderType == proto.BuilderType_UnknowBuilderType {
		return fmt.Errorf("unsopport stream type: %s", stream)
	}
	return nil
}

func (handler *FastAddInboundHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target             string   `json:"target"`
		Tag                string   `json:"tag"`
		Protocol           string   `json:"protocol"`
		Stream             string   `json:"stream"`
		Domain             string   `json:"domain"`
		IsXtls             bool     `json:"is_xtls"`   // deprecated: use security field instead
		Port               int32    `json:"port"`
		SelfSigned         bool     `json:"self_signed"`
		Container          string   `json:"container"` // "xray"(default) or "snell"
		Security           string   `json:"security"`                // "tls"/"xtls"/"reality"
		RealityTarget      string   `json:"reality_target"`          // e.g. "www.example.com:443"
		RealityServerNames []string `json:"reality_server_names"`
		RealityShortIDs    []string `json:"reality_short_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)
	if req.Protocol == "" {
		req.Protocol = "vless"
	}
	if req.Stream == "" {
		req.Stream = "tcp"
	}

	if err := checkBuilder(req.Protocol, req.Stream); err != nil {
		jsonErr(c, 400, err.Error())
		return
	}

	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.FastAddInboundType, &proto.FastAddInboundReq{
		InboundBuilderType: getBuilderType(req.Protocol),
		StreamBuilderType:  getBuilderType(req.Stream),
		Port:               req.Port,
		Domain:             req.Domain,
		IsXtls:             req.IsXtls,
		Tag:                req.Tag,
		ContainerType:      req.Container,
		Security:           req.Security,
		RealityTarget:      req.RealityTarget,
		RealityServerNames: req.RealityServerNames,
		RealityShortIds:    req.RealityShortIDs,
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
	body: {"target": "", "tag": "", "protocol": "vless", "stream": "tcp", "domain": "", "security": "tls", "port": 0, "container": "xray", "reality_target": "", "reality_server_names": [], "reality_short_ids": []}
	protocol: 协议类型, 支持vless, vmess, trojan
	stream: 传输层协议, 支持tcp, ws, quic, mkcp, grpc, http
	security: 安全类型, 支持tls(默认), xtls, reality
	domain: 证书的域名 (tls/xtls 时使用)
	reality_target: Reality 伪装目标, 如 "www.example.com:443" (security=reality 时使用)
	reality_server_names: 允许的 SNI 列表 (security=reality 时使用)
	reality_short_ids: short ID 列表 (security=reality 时使用)
	container: container类型, 支持xray(默认), snell（snell不支持FastAddInbound）`
}
