package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
)

// RotateAllPortsHandler POST /rotateAllPorts — 用户自助重置自身所有 inbound 的前置转发端口。
// 认证由 AuthMiddleware 在路由层完成（userGroup），无需 handler 内部再做密码校验。
// 对每个 inbound 独立执行 make-before-break，返回所有新端口映射。
//
// 认证规则（与 RotatePortHandler 一致）：
//   - X-Token（break-glass admin）：必须在 body 中显式提供 username。
//   - admin JWT：可选 body username；未指定时操作 token 对应用户。
//   - 普通用户 JWT：只能操作自己的所有 inbound；body 中指定他人 username → 403。
type RotateAllPortsHandler struct{ HttpHandlerImp }

func (handler *RotateAllPortsHandler) handlerFunc(c *gin.Context) {
	callerUsername, _ := c.Get(auth.ContextKeyUsername)
	callerRole, _ := c.Get(auth.ContextKeyRole)
	caller, _ := callerUsername.(string)

	var req struct {
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

	ul := handler.getHttpServer().userLister
	if ul == nil {
		c.JSON(500, gin.H{"code": 500, "msg": "user lister not available"})
		return
	}

	type allPortRotator interface {
		RotateAllUserPorts(username string) (map[string]uint32, error)
	}
	rotator, ok := ul.(allPortRotator)
	if !ok {
		c.JSON(500, gin.H{"code": 500, "msg": "server does not support port rotation"})
		return
	}

	portMap, err := rotator.RotateAllUserPorts(target)
	if err != nil {
		log.Error("[RotateAllPorts] rotation failed", "caller", caller, "target", target, "err", err)
		c.JSON(200, gin.H{"code": 300, "msg": err.Error(), "ports": portMap})
		return
	}

	log.Info("[RotateAllPorts] all ports rotated", "caller", caller, "target", target, "count", len(portMap))
	c.JSON(200, gin.H{"code": 0, "ports": portMap, "msg": "ok"})
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
	Body (optional): {"username": "target-user"}
	  X-Token: body 必须提供 username 字段
	  admin JWT: 可选 username 字段；不填默认操作自身
	  普通用户 JWT: 不允许指定他人 username（→ 403）
	Response: {"code":0,"ports":{"vless-tcp":23456,"trojan-tls":34567},"msg":"ok"}
	部分失败时 code=300，ports 包含已成功轮换的 inbound`
}
