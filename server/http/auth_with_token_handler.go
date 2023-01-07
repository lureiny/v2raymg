package http

import (
	"github.com/gin-gonic/gin"
)

func getAuthHandlerFunc(httpServer *HttpServer) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.DefaultQuery("token", "")
		if token != httpServer.token {
			logger.Error("Err=invalid token|HttpPath=%s", c.FullPath())
			c.String(401, "invalide token")
			c.Abort()
			return
		}
		c.Next()
	}
}
