package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// RotateAllPortsHandler POST /rotateAllPorts — 用户自助重置自身所有 inbound 的前置转发端口。
// 认证由 AuthMiddleware 在路由层完成（userGroup），无需 handler 内部再做密码校验。
// 对每个 inbound 独立执行 make-before-break，返回所有新端口映射。
//
// 认证规则：
//   - X-Token（break-glass admin）：必须在 body 中显式提供 username。
//   - admin JWT：可选 body username；未指定时操作 token 对应用户。
//   - 普通用户 JWT：只能操作自己的所有 inbound；body 中指定他人 username → 403。
type RotateAllPortsHandler struct{ HttpHandlerImp }

func (handler *RotateAllPortsHandler) handlerFunc(c *gin.Context) {
	callerUsername, _ := c.Get(auth.ContextKeyUsername)
	callerRole, _ := c.Get(auth.ContextKeyRole)
	caller, _ := callerUsername.(string)

	var req struct {
		Target   string `json:"target"   form:"target"`
		Username string `json:"username" form:"username"`
	}
	_ = c.ShouldBind(&req)

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
				c.JSON(403, gin.H{"code": 403, "msg": "only admin can rotate another user's ports"})
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
		client.RotateAllPortsReqType,
		&proto.RotateAllPortsReq{Username: target},
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
	// code=0: all succeeded; code=301: partial success (ports has succeeded inbounds);
	// code=300: full failure.
	for _, v := range succList {
		if rsp, ok := v.(*proto.RotateAllPortsRsp); ok {
			c.JSON(200, gin.H{"code": int(rsp.GetCode()), "ports": rsp.GetPorts(), "msg": rsp.GetMsg()})
			return
		}
	}

	jsonErr(c, 500, "no response from target node")
}

func (handler *RotateAllPortsHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *RotateAllPortsHandler) getRelativePath() string { return "/rotateAllPorts" }

func (handler *RotateAllPortsHandler) help() string {
	return `/api/rotateAllPorts
	用户自助重置自身所有 inbound 的前置转发端口（JWT 鉴权，用户只能操作自己的 inbound）
	POST /api/rotateAllPorts
	Header: Authorization: Bearer <jwt>  OR  X-Token: <admin-token>
	Body (optional): {"target":"node1","username": "target-user"}
	  target: 目标节点名，空则默认本机
	  X-Token: body 必须提供 username 字段
	  admin JWT: 可选 username 字段；不填默认操作自身
	  普通用户 JWT: 不允许指定他人 username（→ 403）
	Response: {"code":0,"ports":{"xray:vless-tcp":23456,"xray:trojan-tls":34567},"msg":"ok"}
	部分成功时 code=301，ports 包含已成功轮换的 inbound，msg 包含失败原因
	全部失败时 code=300，ports 为空`
}
