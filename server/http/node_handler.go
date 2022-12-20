package http

import (
	"github.com/gin-gonic/gin"
)

type NodeHandler struct{ HttpHandlerImp }

func (handler *NodeHandler) handlerFunc(c *gin.Context) {
	nodeList := handler.getHttpServer().clusterManager.GetNodeNameList()
	nodeList = append(nodeList, localNode.Name)
	c.JSON(200, nodeList)
}

func (handler *NodeHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
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
