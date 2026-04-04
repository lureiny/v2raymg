package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// SetUserRoleHandler PUT /user/:name/role — sets a user's role (admin/normal).
// Accepts X-Token (break-glass admin, no user account required) or admin JWT.
// X-Token is the primary bootstrap path: use it to promote the first admin user
// when no admin account exists yet.
// Routes through the standard HTTP→gRPC pipeline so that version fields are
// stamped and cluster sync propagates the change automatically.
type SetUserRoleHandler struct{ HttpHandlerImp }

func (handler *SetUserRoleHandler) handlerFunc(c *gin.Context) {
	username := c.Param("name")
	if username == "" {
		c.JSON(400, gin.H{"code": 400, "msg": "username required"})
		return
	}

	var req struct {
		Role   string `json:"role"`
		Target string `json:"target"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Role == "" {
		c.JSON(400, gin.H{"code": 400, "msg": "role required"})
		return
	}
	if req.Role != "admin" && req.Role != "normal" {
		c.JSON(400, gin.H{"code": 400, "msg": "role must be 'admin' or 'normal'"})
		return
	}
	req.Target = resolveTarget(req.Target, handler.getHttpServer().Name)

	userPoint := &proto.User{Name: username, Role: req.Role}
	nodes := handler.getHttpServer().GetTargetNodes(req.Target)
	if len(nodes) == 0 {
		c.JSON(200, gin.H{"code": 404, "msg": "no available node"})
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(), client.UpdateUsersReqType,
		&proto.UserOpReq{Users: []*proto.User{userPoint}},
		handler.getHttpServer().GetClusterToken(),
	)
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		log.Errorf("[SetUserRole] Err=%s|User=%s|Target=%s", errMsg, username, req.Target)
		c.JSON(500, gin.H{"code": 500, "msg": errMsg})
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
	return `/api/user/:name/role
	PUT /api/user/:name/role
	Header: X-Token: <admin-token>  OR  Authorization: Bearer <admin-jwt>
	Body: {"role":"admin"|"normal", "target":""}
	Sets the frontend login role for the specified user.
	X-Token is the break-glass path (no admin account needed); admin JWT also accepted.`
}
