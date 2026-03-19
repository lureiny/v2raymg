package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
)

func getAuthHandlerFunc(httpServer *HttpServer) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-Token")
		if token != httpServer.token {
			log.Errorf("Err=invalid token|HttpPath=%s", c.FullPath())
			c.String(401, "invalide token")
			c.Abort()
			return
		}
		c.Next()
	}
}
