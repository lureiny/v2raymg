package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
)

// LogoutHandler POST /logout — revokes the current JWT by adding its jti to the blacklist.
// Requires Bearer JWT authentication (applied via AuthMiddleware on the route).
type LogoutHandler struct{ HttpHandlerImp }

func (handler *LogoutHandler) handlerFunc(c *gin.Context) {
	// X-Token has no associated JWT jti to blacklist.
	// ContextKeyUsername is only set for JWT auth, not for X-Token.
	if _, exists := c.Get(auth.ContextKeyUsername); !exists {
		c.JSON(401, gin.H{"code": 401, "msg": "JWT required for /logout"})
		return
	}

	authHeader := c.GetHeader("Authorization")
	if len(authHeader) > 7 {
		s := handler.getHttpServer()
		if claims, err := auth.ParseJWT(s.jwtSecret, authHeader[7:]); err == nil {
			auth.BlacklistAdd(claims.JTI)
			log.Info("[Logout] token revoked", "username", claims.Username, "jti", claims.JTI)
		}
	}
	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

func (handler *LogoutHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *LogoutHandler) getRelativePath() string { return "/logout" }

func (handler *LogoutHandler) help() string {
	return `/api/logout
	POST /api/logout
	Header: Authorization: Bearer <token>
	Revokes the current JWT token.`
}
