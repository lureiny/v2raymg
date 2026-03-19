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
		Target     string `json:"target"`
		Tag        string `json:"tag"`
		Protocol   string `json:"protocol"`
		Stream     string `json:"stream"`
		Domain     string `json:"domain"`
		IsXtls     bool   `json:"is_xtls"`
		Port       int32  `json:"port"`
		SelfSigned bool   `json:"self_signed"`
		Container  string `json:"container"` // "xray"(default) or "snell"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(400, "invalid request body: %v", err)
		return
	}
	if req.Target == "" {
		req.Target = handler.getHttpServer().Name
	}
	if req.Protocol == "" {
		req.Protocol = "vless"
	}
	if req.Stream == "" {
		req.Stream = "tcp"
	}

	if err := checkBuilder(req.Protocol, req.Stream); err != nil {
		c.String(200, err.Error())
		return
	}

	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
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
	}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Target=%s", errMsg, req.Target)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *FastAddInboundHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{getAuthHandlerFunc(handler.httpServer), handler.handlerFunc}
}

func (handler *FastAddInboundHandler) getRelativePath() string { return "/inbound/fast" }

func (handler *FastAddInboundHandler) help() string {
	return `POST /inbound/fast
	快速添加指定配置的inbound
	body: {"target": "", "tag": "", "protocol": "vless", "stream": "tcp", "domain": "", "is_xtls": false, "port": 0, "self_signed": false, "container": "xray"}
	protocol: 协议类型, 支持vless, vmess, trojan
	stream: 传输层协议, 支持tcp, ws, quic, mkcp, grpc, http
	is_xtls: 是否使用xtls, 默认使用tls
	domain: 证书的域名
	container: container类型, 支持xray(默认), snell（snell不支持FastAddInbound）`
}
