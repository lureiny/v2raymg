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
	parasMap["tags"] = c.DefaultQuery("tags", "")                          // 按照","分隔
	parasMap["excludeProtocols"] = c.DefaultQuery("exclude_protocols", "") // 按照","分隔
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["useSNI"] = c.DefaultQuery("use_sni", "true")
	return parasMap
}

func (handler *SubHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)
	userAgent := c.GetHeader("User-Agent")

	tagList := common.StringList{}
	tagList = strings.Split(parasMap["tags"], ",")
	excludeProtocols := common.StringList{}
	excludeProtocols = strings.Split(parasMap["excludeProtocols"], ",")
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
			User:             userPoint,
			ExcludeProtocols: excludeProtocols.Filter(func(t string) bool { return len(t) > 0 }),
			UseSni:           parasMap["useSNI"] == "true",
		},
	)

	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		logger.Error(
			"Err=%s|User=%s|Passwd=%s|Target=%s|ExincludeProtocols=%s",
			errMsg,
			parasMap["user"],
			parasMap["pwd"],
			parasMap["target"],
			parasMap["excludeProtocols"],
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
	/sub?target={target}&user={user}&pwd={pwd}&tags={tags}&exclude_protocols={exclude_protocols}&use_sni={use_sni}
	target: 目标节点
	user: user name
	pwd: password
	tags: inbound的tag列表, 使用","分隔
	exclude_protocols: 过滤掉的协议订阅, 使用","分隔
	use_sni: 是否包含sni信息, 解决sni封锁问题
	`
	return usage
}
