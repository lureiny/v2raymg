package http

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type ClearUserHandler struct{ HttpHandlerImp }

func (handler *ClearUserHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Target string `json:"target"`
		Users  string `json:"users"`
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
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
		client.ClearUsersType,
		&proto.ClearUsersReq{Users: strings.Split(req.Users, ",")},
		handler.getHttpServer().GetClusterToken(),
	)
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Target=%s", errMsg, req.Target)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *ClearUserHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{getAuthHandlerFunc(handler.httpServer), handler.handlerFunc}
}

func (handler *ClearUserHandler) getRelativePath() string { return "/users" }

func (handler *ClearUserHandler) help() string {
	return `DELETE /users
	清理用户, 用户级别删除
	body: {"target": "", "users": ""}
	users: 需要清理的用户列表, 使用","分隔`
}
