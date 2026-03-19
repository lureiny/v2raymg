package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// InboundAddHandler POST /inbound — 添加inbound
type InboundAddHandler struct{ HttpHandlerImp }

func (handler *InboundAddHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target         string `json:"target"`
		BoundRawString string `json:"bound_raw_string"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(400, "invalid request body: %v", err)
		return
	}
	if req.Target == "" {
		req.Target = handler.getHttpServer().Name
	}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.AddInboundReqType, &proto.InboundOpReq{InboundInfo: req.BoundRawString}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|OpType=addInbound|Target=%s", errMsg, req.Target)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *InboundAddHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{getAuthHandlerFunc(handler.httpServer), handler.handlerFunc}
}

func (handler *InboundAddHandler) getRelativePath() string { return "/inbound" }

func (handler *InboundAddHandler) help() string {
	return `POST /inbound
	添加inbound
	body: {"target": "", "bound_raw_string": ""}
	bound_raw_string: json中inbound配置base64编码后的字符串`
}

// InboundDeleteHandler DELETE /inbound — 删除inbound
type InboundDeleteHandler struct{ HttpHandlerImp }

func (handler *InboundDeleteHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target string `json:"target"`
		SrcTag string `json:"src_tag"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.String(400, "invalid request body: %v", err)
		return
	}
	if req.Target == "" {
		req.Target = handler.getHttpServer().Name
	}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.DeleteInboundReqType, &proto.InboundOpReq{InboundInfo: req.SrcTag}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|OpType=deleteInbound|Target=%s", errMsg, req.Target)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *InboundDeleteHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{getAuthHandlerFunc(handler.httpServer), handler.handlerFunc}
}

func (handler *InboundDeleteHandler) getRelativePath() string { return "/inbound" }

func (handler *InboundDeleteHandler) help() string {
	return `DELETE /inbound
	删除inbound
	body: {"target": "", "src_tag": ""}
	src_tag: 要删除inbound的tag`
}

// InboundGetHandler GET /inbound — 获取inbound
type InboundGetHandler struct{ HttpHandlerImp }

func (handler *InboundGetHandler) handlerFunc(c *gin.Context) {
	target := c.DefaultQuery("target", handler.getHttpServer().Name)
	srcTag := c.DefaultQuery("src_tag", "")
	nodes := handler.getHttpServer().GetTargetNodes(target)
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.GetInboundReqType, &proto.GetInboundReq{Tag: srcTag}, handler.getHttpServer().GetClusterToken())
	if len(succList) > 0 {
		c.JSON(200, succList)
		return
	}
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|OpType=getInbound|Target=%s", errMsg, target)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *InboundGetHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{getAuthHandlerFunc(handler.httpServer), handler.handlerFunc}
}

func (handler *InboundGetHandler) getRelativePath() string { return "/inbound" }

func (handler *InboundGetHandler) help() string {
	return `GET /inbound
	获取inbound详细配置
	query: target, src_tag`
}
