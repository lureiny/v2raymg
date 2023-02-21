package http

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type AdaptiveOpHandler struct{ HttpHandlerImp }

func (handler *AdaptiveOpHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}

	parasMap["type"] = c.DefaultQuery("type", "")
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["ports"] = c.DefaultQuery("ports", "")
	parasMap["tags"] = c.DefaultQuery("tags", "")
	return parasMap
}

func (handler *AdaptiveOpHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if len(*nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	tagList := common.StringList{}
	tagList = strings.Split(parasMap["tags"], ",")
	portList := common.StringList{}
	portList = strings.Split(parasMap["ports"], ",")

	req := &proto.AdaptiveOpReq{
		Ports: portList.Filter(func(p string) bool { return len(p) > 0 }),
		Tags:  tagList.Filter(func(t string) bool { return len(t) > 0 }),
	}
	var reqType client.ReqToEndNodeType = -1
	switch strings.ToLower(parasMap["type"]) {
	case "del":
		reqType = client.DeleteAdaptiveConfigReqType
	default:
		reqType = client.AddAdaptiveConfigReqType
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(reqType, req)
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		logger.Error(
			"Err=%s|Tags=%s|Ports=%s|Type=%s",
			errMsg,
			strings.Join(tagList, ","),
			strings.Join(portList, ","),
			parasMap["type"],
		)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *AdaptiveOpHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *AdaptiveOpHandler) getRelativePath() string {
	return "/adaptiveOp"
}

func (handler *AdaptiveOpHandler) help() string {
	usage := `/adaptiveOp
	修改端口区间
	请求示例: /adaptiveOp?type=add&target=target1&tags=tag1,tag2&ports=10000&token={token}
	参数列表: 
	token: 用于验证操作权限
	type: 操作类型, 可选值为add, del, 默认值为add
	target: 目标node的名称
	tags: 需要操作的inbound tag, 使用","分割
	ports: 添加/删除的端口, 支持单个port及端口范围(10000-10004)
	`
	return usage
}
