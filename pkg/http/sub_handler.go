package http

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
	_ "github.com/lureiny/v2raymg/pkg/proxy/core/subscription/converter" // register converters
)

type SubHandler struct{ HttpHandlerImp }

func (handler *SubHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	parasMap["user"] = c.Query("user")
	parasMap["pwd"] = c.Query("pwd")
	parasMap["tags"] = c.DefaultQuery("tags", "")
	parasMap["excludeProtocols"] = c.DefaultQuery("exclude_protocols", "")
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["useSNI"] = c.DefaultQuery("use_sni", "true")
	parasMap["fake"] = c.DefaultQuery("fake", "false")
	return parasMap
}

func (handler *SubHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)
	if parasMap["fake"] == "true" {
		c.String(200, subscription.GenerateFakeSSSub())
		return
	}
	userAgent := c.GetHeader("User-Agent")

	tagList := splitAndFilter(parasMap["tags"])
	excludeProtocols := splitAndFilter(parasMap["excludeProtocols"])

	userPoint := &proto.User{
		Name:   parasMap["user"],
		Passwd: parasMap["pwd"],
		Tags:   tagList,
	}

	if userPoint.Name == "" || userPoint.Passwd == "" {
		log.Error("sub: invalid user", "user", parasMap["user"], "target", parasMap["target"])
		c.String(200, "invalid user")
		return
	}

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if nodes == nil {
		c.String(200, "no avaliable node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
		client.GetSubReqType,
		&proto.GetSubReq{
			User:             userPoint,
			ExcludeProtocols: excludeProtocols,
			UseSni:           parasMap["useSNI"] == "true",
			UserAgent:        userAgent,
		},
		handler.getHttpServer().GetClusterToken(),
	)

	if len(failedList) != 0 {
		log.Error("get sub failed", "err", joinFailedList(failedList),
			"user", parasMap["user"], "target", parasMap["target"])
	}

	// Each node returns []string of URIs. Concatenate and convert via user-agent.
	succNodes := []string{}
	for node := range succList {
		succNodes = append(succNodes, node)
	}
	sort.Strings(succNodes)

	var allURIs []string
	for _, n := range succNodes {
		switch v := succList[n].(type) {
		case []string:
			log.Info("[SubHandler] node URIs", "node", n, "count", len(v), "uris", v)
			allURIs = append(allURIs, v...)
		case string:
			log.Warn("[SubHandler] node returned string (expected []string)", "node", n, "value", v)
		default:
			log.Warn("[SubHandler] node returned unexpected type", "node", n, "type", fmt.Sprintf("%T", v))
		}
	}
	log.Info("[SubHandler] total URIs", "count", len(allURIs))

	result, err := subscription.ConvertURIs(strings.ToLower(userAgent), allURIs)
	if err != nil {
		log.Error("convert sub uri failed", "err", err)
	}
	c.String(200, result)
}

func (handler *SubHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *SubHandler) getRelativePath() string { return "/sub" }

func (handler *SubHandler) help() string {
	return `/sub
	获取订阅
	/sub?target={target}&user={user}&pwd={pwd}&tags={tags}&exclude_protocols={exclude_protocols}&use_sni={use_sni}&fake={fake}`
}
