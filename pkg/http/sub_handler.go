package http

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
	_ "github.com/lureiny/v2raymg/pkg/proxy/core/subscription/converter" // register converters
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type SubHandler struct{ HttpHandlerImp }

func (handler *SubHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	parasMap["user"] = c.Query("user")
	parasMap["pwd"] = c.Query("pwd")
	parasMap["token"] = c.Query("token")
	parasMap["excludeProtocols"] = c.DefaultQuery("exclude_protocols", "")
	parasMap["target"] = getTargetFromQuery(c)
	parasMap["client"] = c.Query("client")
	parasMap["useSNI"] = c.DefaultQuery("use_sni", "true")
	parasMap["fake"] = c.DefaultQuery("fake", "false")
	parasMap["proxy_groups_url"] = c.Query("proxy_groups_url")
	parasMap["rule_providers_url"] = c.Query("rule_providers_url")
	parasMap["rules_url"] = c.Query("rules_url")
	// sub_userinfo controls whether the Subscription-Userinfo header is sent.
	// Empty string (the default) means "use the node config" — see handlerFunc.
	parasMap["sub_userinfo"] = c.Query("sub_userinfo")
	// sub_userinfo_format overrides the header schema for one request. Empty
	// falls back to the node config; node config empty falls back to the
	// built-in DefaultSubUserInfoFormat.
	parasMap["sub_userinfo_format"] = c.Query("sub_userinfo_format")
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
	extSubs := c.QueryArray("ext_sub")

	// Authentication: resolve credentials to username at the HTTP layer.
	// RPC GetSub only receives username — no auth logic downstream.
	token := parasMap["token"]
	var username string
	// localUser is the access-node copy of the user. Used later to fill
	// total/expire in the Subscription-Userinfo header (see below).
	var localUser *contracts.User

	ul := handler.getHttpServer().userLister
	if token != "" {
		// Token auth: token uniquely identifies a user.
		type tokenFinder interface {
			FindUserByToken(token string) *contracts.User
		}
		finder, ok := ul.(tokenFinder)
		if !ok {
			log.Error("sub: server does not support token lookup")
			c.String(200, "invalid user")
			return
		}
		user := finder.FindUserByToken(token)
		if user == nil {
			log.Error("sub: invalid token", "target", parasMap["target"])
			c.String(200, "invalid user")
			return
		}
		username = user.Username
		localUser = user
	} else {
		// User+pwd auth: verify password locally.
		name, pwd := parasMap["user"], parasMap["pwd"]
		if name == "" || pwd == "" {
			log.Error("sub: missing credentials", "user", name, "target", parasMap["target"])
			c.String(200, "invalid user")
			return
		}
		user, err := ul.GetUser(name)
		if err != nil {
			log.Error("sub: user not found", "user", name)
			c.String(200, "invalid user")
			return
		}
		if !auth.VerifyLoginPassword(user.LoginPassword, pwd) {
			log.Error("sub: invalid password", "user", name)
			c.String(200, "invalid user")
			return
		}
		username = name
		localUser = user
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
		User:             &proto.User{Name: username},
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
			// Log only the count, never the URIs themselves: node share-links
			// embed UUIDs / passwords / SS PSKs in cleartext, and this handler
			// runs at Info on every /sub request.
			log.Info("[SubHandler] node URIs", "node", n, "count", len(v))
			allURIs = append(allURIs, v...)
		case string:
			log.Warn("[SubHandler] node returned string (expected []string)", "node", n)
		default:
			log.Warn("[SubHandler] node returned unexpected type", "node", n, "type", fmt.Sprintf("%T", v))
		}
	}
	log.Info("[SubHandler] total URIs from cluster", "count", len(allURIs))

	// 拉取外部扩展订阅（在 HTTP handler 层处理，不下沉到 RPC）
	if len(extSubs) > 0 {
		extURIs, truncated := subscription.FetchAndMergeExtSubs(extSubs)
		if truncated {
			log.Warn("ext_sub count exceeds limit, truncated", "count", len(extSubs), "max", subscription.MaxExtSubs)
		}
		allURIs = append(allURIs, extURIs...)
		log.Info("[SubHandler] total URIs after merge", "count", len(allURIs))
	}

	// 构建 ConvertOptions（仅 Clash 格式需要）
	var opts *subscription.ConvertOptions
	if isClashClient(formatHint) {
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

	// Subscription-Userinfo header (RFC-style: upload/download/total/expire).
	// Per-request override via ?sub_userinfo=true|false; empty falls back to the
	// node config (subscription.enable_userinfo_header). The extra GetProfile
	// fan-out only runs when the header is actually going to be sent.
	if shouldEmitSubUserInfo(parasMap["sub_userinfo"], handler.getHttpServer().enableSubUserInfoHeader) {
		format := resolveSubUserInfoFormat(parasMap["sub_userinfo_format"], handler.getHttpServer().subUserInfoHeaderFormat)
		handler.writeSubUserInfoHeader(c, username, localUser, nodes, format, isClashClient(formatHint))
	}

	c.String(200, result)
}

// writeSubUserInfoHeader populates the Subscription-Userinfo response header.
//
// upload/download are summed across all `nodes` via a parallel GetProfile RPC
// — this is the only field set we can meaningfully aggregate, since per-user
// traffic counters live on each end node.
//
// total (TrafficLimit) and expire (ExpiryTime) are read from the access node's
// local user record only. We deliberately do NOT aggregate these across nodes:
// the project has no global traffic-accounting layer today, so each node
// independently holds its own copy of a user's limits. Cluster sync only
// propagates user identity/credentials, not enforcement state. The access
// node's view is therefore one valid snapshot, but it may diverge from what
// other nodes record for the same user (e.g. if limits were updated on one
// node and have not yet been mirrored elsewhere) — the value reported here
// may not match the user's actual cluster-wide quota. Treat it as advisory.
func (handler *SubHandler) writeSubUserInfoHeader(c *gin.Context, username string, localUser *contracts.User, nodes []*cluster.Node, format string, clashClient bool) {
	var upload, download int64
	if len(nodes) > 0 {
		rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
		profileSucc, profileFailed, _ := rpcClient.ReqToMultiEndNodeServer(
			c.Request.Context(),
			client.GetProfileReqType,
			&proto.GetProfileReq{Username: username},
			handler.getHttpServer().GetClusterToken(),
		)
		if len(profileFailed) != 0 {
			log.Warn("sub: get profile partially failed; userinfo upload/download may be undercounted",
				"err", joinFailedList(profileFailed), "user", username)
		}
		for n, v := range profileSucc {
			rsp, ok := v.(*proto.GetProfileRsp)
			if !ok {
				log.Warn("sub: unexpected GetProfile response type", "node", n, "type", fmt.Sprintf("%T", v))
				continue
			}
			upload += rsp.GetUplink()
			download += rsp.GetDownlink()
		}
	}

	var (
		total      int64
		expireUnix int64
		expiryTime time.Time
	)
	if localUser != nil {
		total = localUser.TrafficLimit
		expiryTime = localUser.ExpiryTime
		if !expiryTime.IsZero() {
			expireUnix = expiryTime.Unix()
		}
	}

	vars := buildSubUserInfoVarMap(subUserInfoVars{
		Username:   username,
		Upload:     upload,
		Download:   download,
		Total:      total,
		ExpireUnix: expireUnix,
		ExpiryTime: expiryTime,
	})
	header := renderSubUserInfoFormat(format, vars)
	// Clash-family clients prefer the standardized `total` / `expire` fields
	// to be absent when unset, rather than carrying the `-1` sentinel that
	// other clients tolerate. Only the conventional field names are stripped;
	// custom keys in user-defined schemas are left intact.
	if clashClient {
		header = stripClashEmptyFields(header, total <= 0, expireUnix <= 0)
	}
	c.Header("Subscription-Userinfo", header)
}

// shouldEmitSubUserInfo decides whether to emit the Subscription-Userinfo
// header for a request.
//
// queryVal is the raw ?sub_userinfo= query value. Empty (the user did not
// specify it) means defer to the node config (configDefault). Otherwise
// "true"/"1" → enabled, "false"/"0" → disabled. Anything else falls back
// to configDefault — we do not surface a parse error to the client.
func shouldEmitSubUserInfo(queryVal string, configDefault bool) bool {
	switch strings.ToLower(strings.TrimSpace(queryVal)) {
	case "":
		return configDefault
	case "true", "1":
		return true
	case "false", "0":
		return false
	default:
		return configDefault
	}
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

	Subscription-Userinfo 响应头:
	  sub_userinfo=true|false (可选)
	  - 控制是否返回 Subscription-Userinfo
	  - 未指定时回退到节点配置 subscription.enable_userinfo_header (默认 true)
	  - upload/download 从 target 指定的所有节点聚合, total/expire 仅来自访问节点

	  sub_userinfo_format=<schema> (可选)
	  - 自定义 header 的内容,使用 ${var} 占位符,未知变量替换为空串
	  - 未指定时回退到节点配置 subscription.userinfo_header_format
	  - 节点配置也为空时使用内置默认:
	      upload=${upload}; download=${download}; total=${total}; expire=${expire}
	  - 支持的变量:
	      流量类:    upload, upload_kb, upload_mb, upload_gb, upload_auto
	                 download, download_kb, download_mb, download_gb, download_auto
	                 total,    total_kb,    total_mb,    total_gb,    total_auto
	                 (total <= 0 时, ${total} 渲染为 -1 表示 "无限制";
	                  单位变体不应用此哨兵)
	      时间类:    expire (unix; 无过期或非正值统一为 -1)
	                 expire_string (本地时区 "YYYY-MM-DD hh:mm:ss"; 无过期为 "never")
	      标识类:    username
	  - 进制 1024;_kb/_mb/_gb/_auto 数值最多 2 位小数,去尾 0;_auto 自动选单位 (<1KB 整数 B)
	  - Clash 客户端特例: 当 User-Agent / client 命中 "clash" 时, 渲染结果中标准字段
	    "total=-1" / "expire=-1" 整段会被剥离 (自定义键名不受影响)

	扩展订阅:
	  ext_sub=URL
	  - 可重复传多个（最多 10 个，超出部分会被截断）
	  - handler 拉取链接内容后解码（base64 或明文，按换行分割）
	  - 解码后的 URI 与集群订阅合并，统一转换输出
	  - 单个链接拉取失败不影响其他结果
	  - 示例: ext_sub=https://example.com/sub1&ext_sub=https://example.com/sub2

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
