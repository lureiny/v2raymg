package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// RotateInboundPortHandler POST /rotateInboundPort — 用户自助更换指定 inbound 的前置转发端口。
// 认证由 AuthMiddleware 在路由层完成（userGroup），无需 handler 内部再做密码校验。
//
// 认证规则：
//   - X-Token（break-glass admin）：必须在 body 中显式提供 username。
//   - admin JWT：可选 body username；未指定时操作 token 对应用户。
//   - 普通用户 JWT：只能操作自己的 inbound；body 中指定他人 username → 403。
type RotateInboundPortHandler struct{ HttpHandlerImp }

func (handler *RotateInboundPortHandler) handlerFunc(c *gin.Context) {
	callerUsername, _ := c.Get(auth.ContextKeyUsername)
	callerRole, _ := c.Get(auth.ContextKeyRole)
	caller, _ := callerUsername.(string)

	var req struct {
		Target    string `json:"target"    form:"target"`
		Username  string `json:"username"  form:"username"`
		Container string `json:"container" form:"container"`
		Inbound   string `json:"inbound"   form:"inbound"`
		Port      uint32 `json:"port"      form:"port"` // 0 = 随机分配，>0 = 指定端口
	}

	if err := c.ShouldBind(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid request"})
		return
	}
	if req.Container == "" || req.Inbound == "" {
		c.JSON(400, gin.H{"code": 400, "msg": "container and inbound are required"})
		return
	}

	var target string
	if caller == "" {
		// X-Token path: no user identity in context, username required in body.
		if req.Username == "" {
			c.JSON(400, gin.H{"code": 400, "msg": "username required for X-Token requests"})
			return
		}
		target = req.Username
	} else {
		// JWT path: default to caller's own username.
		target = caller
		if req.Username != "" && req.Username != caller {
			if callerRole != "admin" {
				c.JSON(403, gin.H{"code": 403, "msg": "only admin can rotate another user's inbound port"})
				return
			}
			target = req.Username
		}
	}

	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(),
		client.RotateInboundPortReqType,
		&proto.RotateInboundPortReq{
			Username:  target,
			Container: req.Container,
			Inbound:   req.Inbound,
			Port:      req.Port,
		},
		handler.getHttpServer().GetClusterToken(),
	)
	// RPC transport failure — node unreachable / connection refused etc.
	if len(failedList) != 0 && len(succList) == 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("Err=%s|User=%s|Target=%s", errMsg, target, req.Target)
		jsonErr(c, 500, errMsg)
		return
	}

	// Extract structured response from the target node.
	for _, v := range succList {
		if rsp, ok := v.(*proto.RotateInboundPortRsp); ok {
			c.JSON(200, gin.H{"code": int(rsp.GetCode()), "port": rsp.GetPort(), "msg": rsp.GetMsg()})
			return
		}
	}

	jsonErr(c, 500, "no response from target node")
}

func (handler *RotateInboundPortHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *RotateInboundPortHandler) getRelativePath() string { return "/rotateInboundPort" }

func (handler *RotateInboundPortHandler) help() string {
	return `/api/rotateInboundPort
	用户自助更换指定 container+inbound 的前置转发端口（JWT 鉴权，用户只能操作自己的 inbound）
	POST /api/rotateInboundPort
	Header: Authorization: Bearer <jwt>  OR  X-Token: <admin-token>
	Body: {"target":"node1","container":"xray","inbound":"<tag>","port":0}
	  target: 目标节点名，空则默认本机
	  X-Token: body 必须提供 username 字段
	  admin JWT: 可选 username 字段；不填默认操作自身
	  普通用户 JWT: 不允许指定他人 username（→ 403）
	port=0 随机分配，port>0 指定新端口
	Response: {"code":0,"port":12345,"msg":"ok"}`
}
