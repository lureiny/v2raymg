package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/cluster"
)

type NodeHandler struct{ HttpHandlerImp }

func (handler *NodeHandler) handlerFunc(c *gin.Context) {
	nodeList := handler.getHttpServer().clusterNodes.GetNodesWithFilter(func(n *cluster.Node) bool {
		return true
	})
	c.JSON(200, nodeList)
}

func (handler *NodeHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *NodeHandler) getRelativePath() string { return "/node" }

func (handler *NodeHandler) help() string {
	return `/node?token={token} 获取当前集群内的全部节点`
}
