package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// NodeGroupsGetHandler GET /node/:name/groups
type NodeGroupsGetHandler struct{ HttpHandlerImp }

func (handler *NodeGroupsGetHandler) handlerFunc(c *gin.Context) {
	name := c.Param("name")
	nodes := handler.getHttpServer().GetTargetNodes(name)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.GetNodeGroupsReqType, &proto.GetNodeGroupsReq{}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Node=%s", errMsg, name)
		jsonErr(c, 500, errMsg)
		return
	}
	// Collect groups from the first (and typically only) node
	var groups []string
	for _, v := range succList {
		if gs, ok := v.([]string); ok {
			groups = gs
			break
		}
	}
	if groups == nil {
		groups = []string{}
	}
	c.JSON(200, gin.H{"groups": groups})
}

func (handler *NodeGroupsGetHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *NodeGroupsGetHandler) getRelativePath() string { return "/node/:name/groups" }

func (handler *NodeGroupsGetHandler) help() string {
	return `GET /api/node/:name/groups
	获取节点所属 groups
	path: name — 目标节点名`
}

// NodeGroupsSetHandler PUT /node/:name/groups
type NodeGroupsSetHandler struct{ HttpHandlerImp }

func (handler *NodeGroupsSetHandler) handlerFunc(c *gin.Context) {
	name := c.Param("name")
	var body struct {
		Groups []string `json:"groups"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonErr(c, 400, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	nodes := handler.getHttpServer().GetTargetNodes(name)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.SetNodeGroupsReqType, &proto.SetNodeGroupsReq{Groups: body.Groups}, handler.getHttpServer().GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|Node=%s", errMsg, name)
		jsonErr(c, 500, errMsg)
		return
	}
	jsonOK(c)
}

func (handler *NodeGroupsSetHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *NodeGroupsSetHandler) getRelativePath() string { return "/node/:name/groups" }

func (handler *NodeGroupsSetHandler) help() string {
	return `PUT /api/node/:name/groups
	设置节点所属 groups
	path: name — 目标节点名
	body: {"groups": ["default", "hk"]}`
}
