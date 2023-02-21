package http

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type FastAddInboundHandler struct{ HttpHandlerImp }

func (handler *FastAddInboundHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}

	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["tag"] = c.DefaultQuery("tag", "")
	parasMap["protocol"] = c.DefaultQuery("protocol", "vless")
	parasMap["port"] = c.DefaultQuery("port", "0")
	parasMap["stream"] = c.DefaultQuery("stream", "tcp")
	parasMap["isXtls"] = c.DefaultQuery("isXtls", "false")
	parasMap["domain"] = c.DefaultQuery("domain", "")
	return parasMap
}

func getBuilderType(key string) proto.BuilderType {
	switch strings.ToLower(key) {
	case "vless":
		return proto.BuilderType_VLESSSettingBuilderType
	case "vmess":
		return proto.BuilderType_VMESSSettingBuilderType
	case "trojan":
		return proto.BuilderType_TrojanSettingBuilderType
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
	parasMap := handler.parseParam(c)

	if err := checkBuilder(parasMap["protocol"], parasMap["stream"]); err != nil {
		c.String(200, err.Error())
		return
	}

	port, err := strconv.ParseUint(parasMap["port"], 10, 64)
	if err != nil {
		c.String(200, fmt.Sprintf("wrong port: %s", parasMap["port"]))
		return
	}

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(client.FastAddInboundType, &proto.FastAddInboundReq{
		InboundBuilderType: getBuilderType(parasMap["protocol"]),
		StreamBuilderType:  getBuilderType(parasMap["stream"]),
		Port:               int32(port),
		Domain:             parasMap["domain"],
		IsXtls:             parasMap["isXtls"] == "true",
		Tag:                parasMap["tag"],
	})
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		logger.Error(
			"Err=%s|Target=%s",
			errMsg,
			parasMap["target"],
		)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *FastAddInboundHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *FastAddInboundHandler) getRelativePath() string {
	return "/fastAddInbound"
}

func (handler *FastAddInboundHandler) help() string {
	usage := `/fastAddInbound
	/fastAddInbound?token={token}&target={target}&tag={tag}&protocol={protocol}&port={port}&stream={stream}&isXtls={isXtls}&domain={domain}
	快速添加指定配置的inbound
	参数列表:
	token: 用于验证操作权限
	target: 目标节点名称
	tag: inbound tag, 不可以和已有节点重复
	protocol: 协议类型, 默认为vless, 目前只支持vless, vmess, trojan
	port: inbound port
	stream: 传输层协议, 默认为tcp
	isXtls: true/false, 是否使用xtls, 默认使用tls
	domain: 证书的域名, 需配合证书管理功能使用
	`
	return usage
}
