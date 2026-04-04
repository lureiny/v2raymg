package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type UpdateHandler struct{ HttpHandlerImp }

func (handler *UpdateHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target     string `json:"target"`
		VersionTag string `json:"version_tag"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)
	if req.VersionTag == "" {
		req.VersionTag = "latest"
	}

	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
		client.UpdateProxyReqType,
		&proto.UpdateProxyReq{Tag: req.VersionTag},
		handler.getHttpServer().GetClusterToken(),
	)
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Target=%s|Tag=%s", errMsg, req.Target, req.VersionTag)
		jsonErr(c, 500, errMsg)
		return
	}
	jsonOK(c)
}

func (handler *UpdateHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UpdateHandler) getRelativePath() string { return "/update" }

func (handler *UpdateHandler) help() string {
	return `POST /api/update
	更新目标节点的proxy版本
	body: {"target": "", "version_tag": "latest"}
	version_tag: github上目标tag, 默认为最新版`
}
