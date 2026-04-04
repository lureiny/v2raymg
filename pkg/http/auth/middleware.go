package auth

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// ContextKeyUsername is the gin context key for the authenticated username.
	ContextKeyUsername = "username"
	// ContextKeyRole is the gin context key for the authenticated role.
	ContextKeyRole = "role"
)

// AuthMiddleware returns a gin middleware that validates X-Token or Bearer JWT.
// X-Token takes priority: if present it must exactly match staticToken (→ admin).
// Bearer JWT is checked next; the token must be valid and not blacklisted.
// Requests with no valid credential receive 401.
func AuthMiddleware(staticToken, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. X-Token (admin static token)
		if xtoken := c.GetHeader("X-Token"); xtoken != "" {
			if xtoken == staticToken {
				c.Set(ContextKeyRole, "admin")
				c.Next()
				return
			}
			// X-Token provided but wrong — reject immediately
			c.String(401, "Unauthorized")
			c.Abort()
			return
		}

		// 2. Bearer JWT — only when jwtSecret is configured.
		//    An empty secret would allow forged tokens; reject silently instead.
		if jwtSecret != "" {
			authHeader := c.GetHeader("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				claims, err := ParseJWT(jwtSecret, authHeader[7:])
				if err == nil && !BlacklistContains(claims.JTI) {
					c.Set(ContextKeyUsername, claims.Username)
					c.Set(ContextKeyRole, claims.Role)
					c.Next()
					return
				}
			}
		}

		c.String(401, "Unauthorized")
		c.Abort()
	}
}

// AdminOnly is a middleware that rejects requests where role != "admin".
// Must be placed after AuthMiddleware.
func AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get(ContextKeyRole)
		if role != "admin" {
			c.String(403, "Admin only")
			c.Abort()
			return
		}
		c.Next()
	}
}
