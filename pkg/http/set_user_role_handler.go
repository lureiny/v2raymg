package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
)

// SetUserRoleHandler PUT /user/:name/role — sets a user's role (admin/normal).
// Accepts X-Token (break-glass admin, no user account required) or admin JWT.
// X-Token is the primary bootstrap path: use it to promote the first admin user
// when no admin account exists yet.
type SetUserRoleHandler struct{ HttpHandlerImp }

type roleSetter interface {
	SetUserRole(username, role string) error
}

func (handler *SetUserRoleHandler) handlerFunc(c *gin.Context) {
	username := c.Param("name")
	if username == "" {
		c.JSON(400, gin.H{"code": 400, "msg": "username required"})
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Role == "" {
		c.JSON(400, gin.H{"code": 400, "msg": "role required"})
		return
	}
	if req.Role != "admin" && req.Role != "normal" {
		c.JSON(400, gin.H{"code": 400, "msg": "role must be 'admin' or 'normal'"})
		return
	}

	rs, ok := handler.getHttpServer().userLister.(roleSetter)
	if !ok {
		c.JSON(500, gin.H{"code": 500, "msg": "role management not supported"})
		return
	}

	if err := rs.SetUserRole(username, req.Role); err != nil {
		log.Warn("[SetUserRole] failed", "username", username, "role", req.Role, "err", err)
		c.JSON(404, gin.H{"code": 404, "msg": "user not found"})
		return
	}

	log.Info("[SetUserRole] updated", "username", username, "role", req.Role)
	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

func (handler *SetUserRoleHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *SetUserRoleHandler) getRelativePath() string { return "/user/:name/role" }

func (handler *SetUserRoleHandler) help() string {
	return `/user/:name/role
	PUT /user/:name/role
	Header: X-Token: <admin-token>  OR  Authorization: Bearer <admin-jwt>
	Body: {"role":"admin"|"normal"}
	Sets the frontend login role for the specified user.
	X-Token is the break-glass path (no admin account needed); admin JWT also accepted.`
}
