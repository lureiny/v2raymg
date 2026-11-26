package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const setUserRoleHelp = `/api/user/:name/role
	PUT /api/user/:name/role
	Header: X-Token: <admin-token>  OR  Authorization: Bearer <admin-jwt>
	Body: {"role":"admin"|"normal", "target":""}
	Sets the frontend login role for the specified user.
	X-Token is the break-glass path (no admin account needed); admin JWT also accepted.`

// SetUserRoleReq is the request payload for PUT /user/:name/role (MCP tool: set_user_role).
type SetUserRoleReq struct {
	Name   string `json:"name" path:"name" jsonschema:"Username whose login role will be updated."`
	Role   string `json:"role" jsonschema:"Login role to assign: must be 'admin' or 'normal'."`
	Target string `json:"target,omitempty" jsonschema:"Target cluster node name or node_id, or 'all' (defaults to local node)."`
}

func (s *HttpServer) setUserRoleEndpoint() Endpoint[SetUserRoleReq, map[string]any] {
	return Endpoint[SetUserRoleReq, map[string]any]{
		Method:         http.MethodPut,
		Path:           "/user/:name/role",
		Scope:          ScopeAdmin,
		Help:           setUserRoleHelp,
		MCPName:        "set_user_role",
		MCPDescription: "Set a user's frontend login role ('admin' or 'normal') on target cluster node(s). Propagates via cluster sync when enabled.",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req SetUserRoleReq) (map[string]any, error) {
			if req.Name == "" {
				return nil, NewEndpointErr(http.StatusBadRequest, "username required")
			}
			if req.Role == "" {
				return nil, NewEndpointErr(http.StatusBadRequest, "role required")
			}
			if req.Role != "admin" && req.Role != "normal" {
				return nil, NewEndpointErr(http.StatusBadRequest, "role must be 'admin' or 'normal'")
			}
			target := resolveTarget(req.Target, s.Name)

			userPoint := &proto.User{Name: req.Name, Role: req.Role}
			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return nil, NewBusinessErr(http.StatusOK, 404, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
				ctx, client.UpdateUsersReqType,
				&proto.UserOpReq{Users: []*proto.User{userPoint}},
				s.GetClusterToken(),
			)
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("[SetUserRole] Err=%s|User=%s|Target=%s", errMsg, req.Name, target)
				return nil, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}

			log.Info("[SetUserRole] updated", "username", req.Name, "role", req.Role)
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

// SetUserRoleHandler PUT /user/:name/role — sets a user's role (admin/normal).
// Accepts X-Token (break-glass admin, no user account required) or admin JWT.
// X-Token is the primary bootstrap path: use it to promote the first admin user
// when no admin account exists yet.
// Routes through the standard HTTP→gRPC pipeline so that version fields are
// stamped and cluster sync propagates the change automatically.
type SetUserRoleHandler struct{ HttpHandlerImp }

func (handler *SetUserRoleHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().setUserRoleEndpoint())(c)
}

func (handler *SetUserRoleHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *SetUserRoleHandler) getRelativePath() string { return "/user/:name/role" }

func (handler *SetUserRoleHandler) help() string {
	return setUserRoleHelp
}
