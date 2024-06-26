package http

import (
	"strings"

	"github.com/gin-gonic/gin"
	client "github.com/lureiny/v2raymg/client/rpc"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/common/util"
	globalCluster "github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type AdaptiveHandler struct{ HttpHandlerImp }

func (handler *AdaptiveHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}

	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["tags"] = c.DefaultQuery("tags", "")
	return parasMap
}

func (handler *AdaptiveHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if len(nodes) == 0 {
		c.String(200, "no avaliable node")
		return
	}
	tagList := util.StringList{}
	tagList = strings.Split(parasMap["tags"], ",")

	rpcClient := client.NewEndNodeClient(nodes, nil)
	req := &proto.AdaptiveReq{
		Tags: tagList.Filter(func(t string) bool { return len(t) > 0 }),
	}
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.AdaptiveReqType, req, globalCluster.GetClusterToken())
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		logger.Error(
			"Err=%s|Tags=%s",
			errMsg,
			strings.Join(tagList, ","),
		)
		c.String(200, errMsg)
		return
	}
	c.String(200, "Succ")
}

func (handler *AdaptiveHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
	}
}

func (handler *AdaptiveHandler) getRelativePath() string {
	return "/adaptive"
}

func (handler *AdaptiveHandler) help() string {
	usage := `/adaptive
	对每一个指定tag的inbound, 从配置的port库中随机选择一个, 更新指定tag的端口
	请求示例: /adaptive?tags=tag1,tag2&target=target&token={token}
	参数列表: 
	token: 用于验证操作权限
	tags: 需要操作的inbound tag, 使用","分割
	target: 目标node的名称
	`
	return usage
}
