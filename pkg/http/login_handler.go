package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
)

// LoginHandler POST /login — username + password → JWT
type LoginHandler struct{ HttpHandlerImp }

func (handler *LoginHandler) handlerFunc(c *gin.Context) {
	var req struct {
		Username string `json:"username" form:"username"`
		Password string `json:"password" form:"password"`
	}
	if err := c.ShouldBind(&req); err != nil || req.Username == "" || req.Password == "" {
		c.JSON(400, gin.H{"code": 400, "msg": "username and password are required"})
		return
	}

	s := handler.getHttpServer()
	user, err := s.userLister.GetUser(req.Username)
	if err != nil || !auth.VerifyLoginPassword(user.LoginPassword, req.Password) {
		// Unified 401: do not distinguish "user not found" from "wrong password"
		log.Warn("[Login] auth failed", "username", req.Username)
		c.JSON(401, gin.H{"code": 401, "msg": "invalid username or password"})
		return
	}

	role := user.Role
	if role == "" {
		role = "normal"
	}

	tokenStr, expire, err := auth.GenerateJWT(auth.JWTConfig{
		Secret:      s.jwtSecret,
		ExpireHours: s.jwtExpireHours,
	}, user.Username, role)
	if err != nil {
		log.Error("[Login] generate JWT failed", "err", err)
		c.JSON(500, gin.H{"code": 500, "msg": "internal error"})
		return
	}

	c.JSON(200, gin.H{
		"token":    tokenStr,
		"expire":   expire,
		"role":     role,
		"username": user.Username,
	})
}

func (handler *LoginHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *LoginHandler) getRelativePath() string { return "/api/login" }

func (handler *LoginHandler) help() string {
	return `/api/login
	POST /api/login
	Body: {"username":"<name>","password":"<pass>"}
	Returns JWT token for frontend authentication.`
}
