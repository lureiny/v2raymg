package http

import (
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	client "github.com/lureiny/v2raymg/client/rpc"
	"github.com/lureiny/v2raymg/cluster"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/common/util"
	globalCluster "github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/proxy/sub/converter"
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

	tagList := util.StringList{}
	tagList = strings.Split(parasMap["tags"], ",")
	excludeProtocols := util.StringList{}
	excludeProtocols = strings.Split(parasMap["excludeProtocols"], ",")
	// 需要根据target做路由
	userPoint := &proto.User{
		Name:   parasMap["user"],
		Passwd: parasMap["pwd"],
		Tags:   tagList.Filter(func(t string) bool { return len(t) > 0 }),
	}

	if !cluster.IsUserComplete(userPoint, true) {
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

	rpcClient := client.NewEndNodeClient(nodes, nil)
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
		client.GetSubReqType,
		&proto.GetSubReq{
			User:             userPoint,
			ExcludeProtocols: excludeProtocols.Filter(func(t string) bool { return len(t) > 0 }),
			UseSni:           parasMap["useSNI"] == "true",
		},
		globalCluster.GetClusterToken(),
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
	succNodes := []string{}
	for node := range succList {
		succNodes = append(succNodes, node)
	}

	sort.Strings(succNodes)
	for _, n := range succNodes {
		uris = append(uris, succList[n].([]string)...)
	}

	uri, err := converter.ConvertSubUri(strings.ToLower(userAgent), uris)
	if err != nil {
		logger.Error("Err=%s|URI=%s", err.Error(), uri)
	}
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
