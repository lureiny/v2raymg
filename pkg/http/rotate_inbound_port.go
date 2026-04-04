package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// RotateInboundPortHandler POST /rotateInboundPort — 用户自助更换指定 inbound 的前置转发端口。
// 认证由 AuthMiddleware 在路由层完成（userGroup），无需 handler 内部再做密码校验。
//
// 认证规则（与 RotatePortHandler 一致）：
//   - X-Token（break-glass admin）：必须在 body 中显式提供 username。
//   - admin JWT：可选 body username；未指定时操作 token 对应用户。
//   - 普通用户 JWT：只能操作自己的 inbound；body 中指定他人 username → 403。
type RotateInboundPortHandler struct{ HttpHandlerImp }

func (handler *RotateInboundPortHandler) handlerFunc(c *gin.Context) {
	callerUsername, _ := c.Get(auth.ContextKeyUsername)
	callerRole, _ := c.Get(auth.ContextKeyRole)
	caller, _ := callerUsername.(string)

	var req struct {
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

	ul := handler.getHttpServer().userLister
	if ul == nil {
		c.JSON(500, gin.H{"code": 500, "msg": "user lister not available"})
		return
	}

	// 类型断言：要求 userLister 实现 RotateUserPortForInbound
	type inboundPortRotator interface {
		RotateUserPortForInbound(username string, containerType contracts.ContainerType, inboundTag string, preferredPort uint32) (uint32, error)
	}
	rotator, ok := ul.(inboundPortRotator)
	if !ok {
		c.JSON(500, gin.H{"code": 500, "msg": "server does not support inbound port rotation"})
		return
	}

	newPort, err := rotator.RotateUserPortForInbound(target, contracts.ContainerType(req.Container), req.Inbound, req.Port)
	if err != nil {
		log.Error("[RotateInboundPort] rotate failed", "caller", caller, "target", target, "container", req.Container, "inbound", req.Inbound, "err", err)
		c.JSON(200, gin.H{"code": 300, "msg": err.Error()})
		return
	}

	log.Info("[RotateInboundPort] port rotated", "caller", caller, "target", target, "container", req.Container, "inbound", req.Inbound, "port", newPort)
	c.JSON(200, gin.H{"code": 0, "port": newPort, "msg": "ok"})
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
	Body: {"container":"xray","inbound":"<tag>","port":0}
	  X-Token: body 必须提供 username 字段
	  admin JWT: 可选 username 字段；不填默认操作自身
	  普通用户 JWT: 不允许指定他人 username（→ 403）
	port=0 随机分配，port>0 指定新端口
	Response: {"code":0,"port":12345,"msg":"ok"}`
}
