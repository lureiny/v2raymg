package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
)

// RotatePortHandler POST /rotatePort — 用户自助端口轮换
// 使用用户自身密码鉴权，只允许操作自己的端口映射。
// 不需要 admin token。
type RotatePortHandler struct{ HttpHandlerImp }

func (handler *RotatePortHandler) handlerFunc(c *gin.Context) {
	var req struct {
		User string `json:"user" form:"user"`
		Pwd  string `json:"pwd"  form:"pwd"`
	}

	// 支持 JSON body 或 form 参数
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "msg": "invalid request"})
		return
	}

	if req.User == "" || req.Pwd == "" {
		c.JSON(400, gin.H{"code": 400, "msg": "user and pwd are required"})
		return
	}

	// 用 userLister 鉴权：比对用户名 + 密码
	ul := handler.getHttpServer().userLister
	if ul == nil {
		c.JSON(500, gin.H{"code": 500, "msg": "user lister not available"})
		return
	}
	users := ul.ListUsersWithPasswd()
	passwd, ok := users[req.User]
	if !ok || passwd != req.Pwd {
		log.Warn("[RotatePort] auth failed", "user", req.User)
		c.JSON(401, gin.H{"code": 401, "msg": "invalid user or password"})
		return
	}

	// 调用 userMgr.RotateUserPort
	// userLister 实际上是 *usermanager.UserManager，实现了 RotateUserPort
	type portRotator interface {
		RotateUserPort(username string) error
	}
	rotator, ok := ul.(portRotator)
	if !ok {
		c.JSON(500, gin.H{"code": 500, "msg": "server does not support port rotation"})
		return
	}

	if err := rotator.RotateUserPort(req.User); err != nil {
		log.Error("[RotatePort] rotate failed", "user", req.User, "err", err)
		c.JSON(200, gin.H{"code": 300, "msg": err.Error()})
		return
	}

	log.Info("[RotatePort] port rotated", "user", req.User)
	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

func (handler *RotatePortHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *RotatePortHandler) getRelativePath() string { return "/rotatePort" }

func (handler *RotatePortHandler) help() string {
	return `/rotatePort
	用户自助轮换端口（使用用户密码鉴权，无需 admin token）
	POST /rotatePort
	Body: {"user":"<username>","pwd":"<password>"}`
}
