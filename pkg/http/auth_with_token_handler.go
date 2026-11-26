package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
)

// getAuthHandlerFunc guards the legacy X-Token-only routes (/api/metrics).
//
// The empty check is load-bearing, not defensive. Without it a blank
// httpServer.token compares equal to the absent header, and the route answers
// every unauthenticated request — which is what happened whenever
// end_node.http_token was left unset. The server now generates a token when the
// config omits one, but this must reject on its own: a route that is only
// secure because some other component remembered to fill in a value is not
// secure, it is lucky.
func getAuthHandlerFunc(httpServer *HttpServer) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-Token")
		if token == "" || httpServer.token == "" || token != httpServer.token {
			log.Errorf("Err=invalid token|HttpPath=%s", c.FullPath())
			c.String(401, "invalide token")
			c.Abort()
			return
		}
		c.Next()
	}
}
