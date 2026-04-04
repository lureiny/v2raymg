package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// ChangePasswordHandler PUT /profile/password — 用户修改自身密码
// Auth: Bearer JWT (normal user or admin, userGroup)
// Body: { "old_password": "", "new_password": "" }
type ChangePasswordHandler struct{ HttpHandlerImp }

func (handler *ChangePasswordHandler) handlerFunc(c *gin.Context) {
	usernameVal, exists := c.Get(auth.ContextKeyUsername)
	if !exists {
		c.JSON(401, gin.H{"code": 401, "msg": "JWT required"})
		return
	}
	username, _ := usernameVal.(string)

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.OldPassword == "" || req.NewPassword == "" {
		c.JSON(400, gin.H{"code": 400, "msg": "old_password and new_password are required"})
		return
	}

	ul := handler.getHttpServer().userLister
	user, err := ul.GetUser(username)
	if err != nil {
		log.Warn("[ChangePassword] user not found", "username", username)
		c.JSON(404, gin.H{"code": 404, "msg": "user not found"})
		return
	}

	if !auth.VerifyLoginPassword(user.LoginPassword, req.OldPassword) {
		c.JSON(403, gin.H{"code": 403, "msg": "old password is incorrect"})
		return
	}

	s := handler.getHttpServer()
	target := s.Name
	nodes := s.GetTargetNodes(target)
	if len(nodes) == 0 {
		c.JSON(200, gin.H{"code": 300, "msg": "no available node"})
		return
	}
	rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
	userPoint := &proto.User{Name: username, Passwd: req.NewPassword}
	_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(), client.UpdateUsersReqType, &proto.UserOpReq{Users: []*proto.User{userPoint}}, s.GetClusterToken())
	if len(failedList) != 0 {
		log.Errorf("[ChangePassword] update failed|User=%s|Err=%s", username, joinFailedList(failedList))
		c.JSON(200, gin.H{"code": 300, "msg": joinFailedList(failedList)})
		return
	}

	log.Info("[ChangePassword] password updated", "username", username)
	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

func (handler *ChangePasswordHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ChangePasswordHandler) getRelativePath() string { return "/profile/password" }

func (handler *ChangePasswordHandler) help() string {
	return `/api/profile/password
	PUT /api/profile/password
	Header: Authorization: Bearer <jwt-token>
	Body: {"old_password":"<current>","new_password":"<new>"}
	Changes the authenticated user's own password. JWT required.`
}
