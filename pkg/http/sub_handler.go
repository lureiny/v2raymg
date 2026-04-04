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
	parasMap["token"] = c.Query("token")
	parasMap["excludeProtocols"] = c.DefaultQuery("exclude_protocols", "")
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	parasMap["client"] = c.Query("client")
	parasMap["useSNI"] = c.DefaultQuery("use_sni", "true")
	parasMap["fake"] = c.DefaultQuery("fake", "false")
	parasMap["proxy_groups_url"] = c.Query("proxy_groups_url")
	parasMap["rule_providers_url"] = c.Query("rule_providers_url")
	parasMap["rules_url"] = c.Query("rules_url")
	return parasMap
}

func (handler *SubHandler) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)
	if parasMap["fake"] == "true" {
		c.String(200, subscription.GenerateFakeSSSub())
		return
	}
	userAgent := c.GetHeader("User-Agent")
	clientName := strings.TrimSpace(parasMap["client"])
	formatHint := userAgent
	if clientName != "" {
		formatHint = clientName
	}

	excludeProtocols := splitAndFilter(parasMap["excludeProtocols"])

	// 解析新参数
	proxyGroups := c.QueryArray("proxy_group")
	ruleProviders := c.QueryArray("rule_provider")
	rules := c.QueryArray("rule")

	token := parasMap["token"]
	userPoint := &proto.User{
		Name:   parasMap["user"],
		Passwd: parasMap["pwd"],
	}

	// Dual auth: token takes priority; otherwise require user+pwd.
	if token == "" && (userPoint.Name == "" || userPoint.Passwd == "") {
		log.Error("sub: missing credentials", "user", parasMap["user"], "target", parasMap["target"])
		c.String(200, "invalid user")
		return
	}

	nodes := handler.getHttpServer().GetTargetNodes(parasMap["target"])
	if nodes == nil {
		c.String(200, "no avaliable node")
		return
	}

	getSubReq := &proto.GetSubReq{
		ExcludeProtocols: excludeProtocols,
		UseSni:           parasMap["useSNI"] == "true",
		UserAgent:        formatHint,
	}
	if token != "" {
		getSubReq.Token = token
	} else {
		getSubReq.User = userPoint
	}

	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
		client.GetSubReqType,
		getSubReq,
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

	// 构建 ConvertOptions（仅 Clash 格式需要）
	var opts *subscription.ConvertOptions
	if strings.ToLower(formatHint) == "clash" || strings.Contains(strings.ToLower(formatHint), "clash") {
		opts = subscription.BuildConvertOptions(
			proxyGroups,
			ruleProviders,
			rules,
			parasMap["proxy_groups_url"],
			parasMap["rule_providers_url"],
			parasMap["rules_url"],
		)
	}

	result, err := subscription.ConvertURIsWithOptions(strings.ToLower(formatHint), allURIs, opts)
	if err != nil {
		log.Error("convert sub uri failed", "err", err)
		c.String(500, err.Error())
		return
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
	基础参数:
	  /sub?target={target}&user={user}&pwd={pwd}&exclude_protocols={exclude_protocols}&use_sni={use_sni}&fake={fake}

	客户端格式:
	  client=clash|surge|qv2ray (可选)
	  - client 有值时优先于 User-Agent
	  - client 未指定或为空时，回退到 User-Agent

	Clash/Mihomo 自定义配置:
	  当前 Clash/Mihomo 输出强制依赖远程模板：
	  1. 先拉取远程模板
	  2. 注入当前用户的 proxies
	  3. 按模板语义补齐/填充 Manual、Auto 等 group
	  4. 再对用户传入的 proxy_group / rule_provider / rule 做 patch
	  5. 若模板拉取或 patch 失败，返回 5xx + 错误文本，不会静默 fallback

	模板补齐逻辑:
	  - 若模板已有 Auto，则按现有模板逻辑填充 proxies
	  - 若模板已有 Manual，则填充为 [all proxies, DIRECT]
	  - 若模板没有 Manual，则自动生成:
	      Manual -> [all proxies, DIRECT]
	    并把 Manual 添加到其他 proxy-group 中

	proxy_group 参数:
	  proxy_group=name:type:proxies[:inject_into_groups]
	  - 可重复传多个 proxy_group
	  - inject_into_groups 可选，支持逗号分隔多个目标 group
	  - 未提供或指定目标均不存在时，默认添加到所有其他非自定义 group
	  - 同一次请求新增的多个自定义 group 不会互相添加，避免循环依赖
	  - 若自定义 group 名与模板/默认 group 重名，会先清理该 group 在其他 group 中的旧引用，再按当前配置重新填充

	proxy_group 中 proxies 字段的取值规则:
	  - all                -> 所有 proxy
	  - DIRECT/REJECT/PASS/COMPATIBLE -> 内置策略
	  - 已存在 group 名      -> 引用该 group
	  - 其他字符串           -> 按正则匹配 proxy 名称
	  - 空值                -> 等价于 all（便于使用）

	proxy_group 示例:
	  - Manual:select:all,DIRECT
	  - Manual:select::Proxy,Streaming     # 空 proxies 等价于 all
	  - HK:select:HK|TW|SG:Proxy
	  - Fallback:fallback:US,JP:Proxy,Streaming

	rule_provider 参数:
	  rule_provider=name:type:behavior:url
	  - 可重复传多个
	  - 示例: reject:http:domain:https://example.com/reject.yaml

	rule 参数:
	  rule=type,value,policy
	  - 可重复传多个
	  - 新增规则会优先插到模板原有 rules 前面
	  - policy 必须是已存在的 proxy-group 或内置策略，否则返回 5xx
	  - 示例:
	      rule=DOMAIN-SUFFIX,google.com,Manual
	      rule=RULE-SET,reject,REJECT
	      rule=MATCH,,DIRECT

	远程配置片段:
	  proxy_groups_url=URL
	  rule_providers_url=URL
	  rules_url=URL`
}
