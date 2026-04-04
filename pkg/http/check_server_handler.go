package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type PingCheckHandler struct{ HttpHandlerImp }

func (handler *PingCheckHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target          string `json:"target"`
		EnablePingCheck bool   `json:"enable_ping_check"`
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
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.SetPingCheckType, &proto.SetPingCheckReq{
		EnablePingCheck: req.EnablePingCheck,
	}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Target=%s", errMsg, req.Target)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *PingCheckHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *PingCheckHandler) getRelativePath() string { return "/pingCheck" }

func (handler *PingCheckHandler) help() string {
	return `PUT /pingCheck
	设置ping检测
	body: {"target": "", "enable_ping_check": false}
	enable_ping_check: 是否开启ping检测`
}
