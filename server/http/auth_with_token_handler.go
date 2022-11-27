package http

import (
	"github.com/gin-gonic/gin"
)

type AuthWithTokenHandler struct{ HttpHandlerImp }

func (handler AuthWithTokenHandler) handlerFunc(c *gin.Context) {
	token := c.DefaultQuery("token", "")
	if token != handler.getHttpServer().token {
		logger.Error("Err=invalid token|HttpPath=%s", c.FullPath())
		c.String(401, "invalide token")
		c.Abort()
		return
	}
	c.Next()
}
