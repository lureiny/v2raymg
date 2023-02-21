package http

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/proxy/sub"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

type SubHandler struct{ HttpHandlerImp }

func (handler *SubHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	// sub  这里需要变更下token的问题
	parasMap["user"] = c.Query("user")
	parasMap["pwd"] = c.Query("pwd")
	parasMap["tags"] = c.DefaultQuery("tags", "") // 按照","分割
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	return parasMap
}

func (handler *SubHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)
	userAgent := c.GetHeader("User-Agent")

	tagList := common.StringList{}
	tagList = strings.Split(parasMap["tags"], ",")
	// 需要根据target做路由
	userPoint := &proto.User{
		Name:   parasMap["user"],
		Passwd: parasMap["pwd"],
		Tags:   tagList.Filter(func(t string) bool { return len(t) > 0 }),
	}

	if !common.IsUserComplete(userPoint, true) {
		logger.Error(
			"Err=%s|User=%s|Passwd=%s|Target=%s",
			"invalid user",
			parasMap["user"],
			parasMap["pwd"],
			parasMap["target"],
		)
		c.String(200, "invalid user")
		return
	}

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if nodes == nil {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, localNode)
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		client.GetSubReqType,
		&proto.GetSubReq{
			User: userPoint,
		},
	)

	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		logger.Error(
			"Err=%s|User=%s|Passwd=%s|Target=%s",
			errMsg,
			parasMap["user"],
			parasMap["pwd"],
			parasMap["target"],
		)
	}
	uris := []string{}
	for _, u := range succList {
		uris = append(uris, u.([]string)...)
	}

	uri := sub.TransferSubUri(uris, userAgent)
	c.String(200, uri)
}

func (handler *SubHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		handler.handlerFunc,
	}
}

func (handler *SubHandler) getRelativePath() string {
	return "/sub"
}

func (handler *SubHandler) help() string {
	usage := `/sub
	获取订阅
	/sub?target={target}&user={user}&pwd={pwd}&tags={tags}
	target: 目标节点
	user: user name
	pwd: password
	tags: inbound的tag列表, 使用","分隔
	`
	return usage
}
