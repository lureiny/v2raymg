package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// InboundListHandler GET /inbounds — 列出所有 inbound（跨所有 container）
type InboundListHandler struct{ HttpHandlerImp }

func (handler *InboundListHandler) handlerFunc(c *gin.Context) {
	target := getTargetFromQuery(c)
	nodes := handler.getHttpServer().GetTargetNodes(target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(),
		client.ListInboundReqType,
		&proto.ListInboundReq{},
		handler.getHttpServer().GetClusterToken(),
	)
	if len(succList) > 0 {
		c.JSON(200, succList)
		return
	}
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|OpType=listInbound|Target=%s", errMsg, target)
		jsonErr(c, 500, errMsg)
		return
	}
	c.JSON(200, []interface{}{})
}

func (handler *InboundListHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *InboundListHandler) getRelativePath() string { return "/inbounds" }

func (handler *InboundListHandler) help() string {
	return `GET /api/inbounds
	列出所有节点的所有 inbound（跨 xray/snell/hysteria/mihomo container）
	query: target（默认当前节点）
	返回: [{"container":"xray","name":"tag1"}, ...]`
}

// InboundDeleteByNameHandler DELETE /inbounds — 按 container + name 删除 inbound
type InboundDeleteByNameHandler struct{ HttpHandlerImp }

func (handler *InboundDeleteByNameHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target    string `json:"target"`
		Container string `json:"container"` // "xray" / "snell" / "hysteria" / "mihomo"
		Name      string `json:"name"`      // inbound tag
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	if req.Container == "" {
		jsonErr(c, 400, "container is required")
		return
	}
	if req.Name == "" {
		jsonErr(c, 400, "name is required")
		return
	}
	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(),
		client.DeleteInboundByNameReqType,
		&proto.DeleteInboundByNameReq{Container: req.Container, Name: req.Name},
		handler.getHttpServer().GetClusterToken(),
	)
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|OpType=deleteInboundByName|Target=%s|Container=%s|Name=%s", errMsg, req.Target, req.Container, req.Name)
		jsonErr(c, 500, errMsg)
		return
	}
	jsonOK(c)
}

func (handler *InboundDeleteByNameHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *InboundDeleteByNameHandler) getRelativePath() string { return "/inbounds" }

func (handler *InboundDeleteByNameHandler) help() string {
	return `DELETE /api/inbounds
	按 container 类型 + name 删除 inbound
	body: {"target": "", "container": "xray|snell|hysteria|mihomo", "name": "<inbound tag>"}`
}
