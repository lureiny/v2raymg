package http

import (
	"strings"

	"github.com/gin-gonic/gin"
	client "github.com/lureiny/v2raymg/client/rpc"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type ClearUserHandler struct{ HttpHandlerImp }

func (handler *ClearUserHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	parasMap["users"] = c.Query("users")
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	return parasMap
}

func (handler *ClearUserHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, nil)

	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		client.ClearUsersType,
		&proto.ClearUsersReq{
			Users: strings.Split(parasMap["users"], ","),
		},
	)
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

func (handler *ClearUserHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *ClearUserHandler) getRelativePath() string {
	return "/clearUsers"
}

func (handler *ClearUserHandler) help() string {
	usage := `/clearUsers
	清理用户, 用户级别删除, delete接口是在tag级别删除用户
	/clearUsers?target={target}&users={users}&token={token}
	参数列表:
	target: 目标node
	token: 用于验证操作权
	users: 需要清理的用户列表, 使用","分隔
	`
	return usage
}
