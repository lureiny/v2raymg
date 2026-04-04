package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
)

// RotatePortHandler POST /rotatePort — 端口轮换
//
// 认证规则（AuthMiddleware 已在路由层完成身份校验）：
//
//   - X-Token（break-glass admin）：
//     X-Token 不携带 username context，必须在 body 中显式提供 username。
//     缺少或空 username → 400（参数错误，非 401）。
//     有效 username → 轮换该用户端口。
//
//   - admin JWT：
//     可选 body username。指定时轮换目标用户；未指定时轮换 token 对应用户。
//
//   - 普通用户 JWT：
//     只能轮换自己的端口。body 中指定他人 username → 403。
type RotatePortHandler struct{ HttpHandlerImp }

func (handler *RotatePortHandler) handlerFunc(c *gin.Context) {
	callerUsername, _ := c.Get(auth.ContextKeyUsername)
	callerRole, _ := c.Get(auth.ContextKeyRole)
	caller, _ := callerUsername.(string)

	// Parse optional body username.
	var req struct {
		Username string `json:"username" form:"username"`
	}
	_ = c.ShouldBind(&req)

	var target string
	if caller == "" {
		// X-Token path: no user identity in context.
		// A target username must be provided explicitly in the body.
		if req.Username == "" {
			c.JSON(400, gin.H{"code": 400, "msg": "username required for X-Token requests"})
			return
		}
		target = req.Username
	} else {
		// JWT path: default to the caller's own username.
		target = caller
		if req.Username != "" && req.Username != caller {
			if callerRole != "admin" {
				c.JSON(403, gin.H{"code": 403, "msg": "only admin can rotate another user's port"})
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

	type portRotator interface {
		RotateUserPort(username string) error
	}
	rotator, ok := ul.(portRotator)
	if !ok {
		c.JSON(500, gin.H{"code": 500, "msg": "server does not support port rotation"})
		return
	}

	if err := rotator.RotateUserPort(target); err != nil {
		log.Error("[RotatePort] rotate failed", "caller", caller, "target", target, "err", err)
		c.JSON(200, gin.H{"code": 300, "msg": err.Error()})
		return
	}

	log.Info("[RotatePort] port rotated", "caller", caller, "target", target)
	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

func (handler *RotatePortHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *RotatePortHandler) getRelativePath() string { return "/rotatePort" }

func (handler *RotatePortHandler) help() string {
	return `/api/rotatePort
	POST /api/rotatePort
	Header: Authorization: Bearer <jwt>  OR  X-Token: <admin-token>
	Body (optional): {"username": "target-user"}

	X-Token: body username required (break-glass, no user context).
	admin JWT: optional body username; defaults to token's own user.
	normal JWT: no body username allowed (own port only); other username -> 403.`
}
