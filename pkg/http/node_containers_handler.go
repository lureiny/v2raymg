package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type ContainersHandler struct{ HttpHandlerImp }

func (handler *ContainersHandler) handlerFunc(c *gin.Context) {
	target := getTargetFromQuery(c)
	nodes := handler.getHttpServer().GetTargetNodes(target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}

	s := handler.getHttpServer()
	rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(),
		client.GetContainersReqType,
		&proto.GetContainersReq{},
		s.GetClusterToken(),
	)

	result := make(map[string][]string, len(succList))
	for nodeName, v := range succList {
		cs, ok := v.([]string)
		if !ok {
			if failedList == nil {
				failedList = map[string]string{}
			}
			failedList[nodeName] = "unexpected response type"
			continue
		}
		result[nodeName] = cs
	}

	if len(failedList) != 0 {
		log.Errorf("Err=%s|OpType=getContainers|Target=%s", joinFailedList(failedList), target)
	}

	c.JSON(200, gin.H{
		"nodes":  result,
		"failed": failedList,
	})
}

func (handler *ContainersHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ContainersHandler) getRelativePath() string { return "/containers" }

func (handler *ContainersHandler) help() string {
	return `GET /api/containers?target=<node|all> 获取节点已启用的 container 类型列表`
}
