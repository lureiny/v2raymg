package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const changePasswordHelp = `/api/profile/password
	PUT /api/profile/password
	Header: Authorization: Bearer <jwt-token>
	Body: {"target":"node1","old_password":"<current>","new_password":"<new>"}
	  target: 目标节点名，空则默认本机
	Changes the authenticated user's own password. JWT required.`

// ChangePasswordReq is the request payload for PUT /profile/password (MCP tool: change_password).
type ChangePasswordReq struct {
	Target      string `json:"target,omitempty" jsonschema:"Target cluster node name or node_id, or 'all' (defaults to local node)."`
	OldPassword string `json:"old_password" jsonschema:"Current login password of the authenticated user."`
	NewPassword string `json:"new_password" jsonschema:"New login password to replace the current password."`
}

func (s *HttpServer) changePasswordEndpoint() Endpoint[ChangePasswordReq, map[string]any] {
	return Endpoint[ChangePasswordReq, map[string]any]{
		Method:         http.MethodPut,
		Path:           "/profile/password",
		Scope:          ScopeUser,
		Help:           changePasswordHelp,
		MCPName:        "change_password",
		MCPDescription: "Change the authenticated JWT user's own login password after verifying the current password. Requires JWT authentication (X-Token is rejected).",
		ReadOnly:       false,
		Destructive:    true,
		Execute: func(ctx context.Context, caller Caller, req ChangePasswordReq) (map[string]any, error) {
			if caller.Username == "" {
				return nil, NewEndpointErr(http.StatusUnauthorized, "JWT required")
			}
			if req.OldPassword == "" || req.NewPassword == "" {
				return nil, NewEndpointErr(http.StatusBadRequest, "old_password and new_password are required")
			}
			if s.userLister == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "user lister not initialized")
			}

			user, err := s.userLister.GetUser(caller.Username)
			if err != nil {
				log.Warn("[ChangePassword] user not found", "username", caller.Username)
				return nil, NewEndpointErr(http.StatusNotFound, "user not found")
			}
			if !auth.VerifyLoginPassword(user.LoginPassword, req.OldPassword) {
				return nil, NewEndpointErr(http.StatusForbidden, "old password is incorrect")
			}

			target := resolveTarget(req.Target, s.Name)
			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return nil, NewBusinessErr(http.StatusOK, 300, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			userPoint := &proto.User{Name: caller.Username, Passwd: req.NewPassword}
			succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
				ctx,
				client.UpdateUsersReqType,
				&proto.UserOpReq{Users: []*proto.User{userPoint}},
				s.GetClusterToken(),
			)
			if len(failedList) != 0 && len(succList) == 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("[ChangePassword] update failed|User=%s|Err=%s", caller.Username, errMsg)
				return nil, NewBusinessErr(http.StatusOK, 300, errMsg)
			}

			msg := "ok"
			if len(failedList) != 0 {
				msg = joinFailedList(failedList)
			}
			log.Info("[ChangePassword] password updated", "username", caller.Username)
			return map[string]any{"code": 0, "msg": msg}, nil
		},
	}
}

// ChangePasswordHandler PUT /profile/password — 用户修改自身密码
// Auth: Bearer JWT (normal user or admin, userGroup)
// Body: { "old_password": "", "new_password": "" }
type ChangePasswordHandler struct{ HttpHandlerImp }

func (handler *ChangePasswordHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().changePasswordEndpoint())(c)
}

func (handler *ChangePasswordHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ChangePasswordHandler) getRelativePath() string { return "/profile/password" }

func (handler *ChangePasswordHandler) help() string {
	return changePasswordHelp
}
