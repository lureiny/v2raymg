package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/global/cluster"
)

type NodeHandler struct{ HttpHandlerImp }

func (handler *NodeHandler) handlerFunc(c *gin.Context) {
	nodeList := cluster.GetAllNode()
	c.JSON(200, nodeList)
}

func (handler *NodeHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *NodeHandler) getRelativePath() string {
	return "/node"
}

func (handler *NodeHandler) help() string {
	usage := `/node
	/node?token={token}
	获取当前集群内的全部节点
	参数列表:
	token: 用于验证操作权限
	`
	return usage
}
