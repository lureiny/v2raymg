package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const setUserClientLimitHelp = `PUT /api/user/:name/client-limit
	Header: X-Token or Authorization: Bearer <admin-jwt>
	Body: {"max_clients": 3, "recycle_delay_sec": 60, "drain_sec": 2, "target": ""}
	Sets client connection limits. max_clients is required; 0 removes the limit.
	recycle_delay_sec and drain_sec are optional (unchanged if omitted).`

// SetUserClientLimitReq is the request payload for PUT /user/:name/client-limit (MCP tool: set_user_client_limit).
type SetUserClientLimitReq struct {
	Name            string `json:"name" path:"name" jsonschema:"Username whose client connection limits will be updated."`
	MaxClients      *int   `json:"max_clients" jsonschema:"Maximum concurrent client connections (required; 0 removes the limit)."`
	RecycleDelaySec *int   `json:"recycle_delay_sec,omitempty" jsonschema:"Client slot recycle delay in seconds (optional; omit to leave unchanged)."`
	DrainSec        *int   `json:"drain_sec,omitempty" jsonschema:"Client connection drain grace period in seconds (optional; omit to leave unchanged)."`
	Target          string `json:"target,omitempty" jsonschema:"Target cluster node name or node_id, or 'all' (defaults to local node)."`
}

func (s *HttpServer) setUserClientLimitEndpoint() Endpoint[SetUserClientLimitReq, map[string]any] {
	return Endpoint[SetUserClientLimitReq, map[string]any]{
		Method:         http.MethodPut,
		Path:           "/user/:name/client-limit",
		Scope:          ScopeAdmin,
		Help:           setUserClientLimitHelp,
		MCPName:        "set_user_client_limit",
		MCPDescription: "Set or remove concurrent client connection limits for a user on target cluster node(s). Pass max_clients=0 to remove the limit; recycle_delay_sec and drain_sec are optional.",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req SetUserClientLimitReq) (map[string]any, error) {
			if req.Name == "" {
				return nil, NewEndpointErr(http.StatusBadRequest, "username required")
			}
			if req.MaxClients == nil {
				return nil, NewEndpointErr(http.StatusBadRequest, "max_clients required")
			}
			if *req.MaxClients < 0 {
				return nil, NewEndpointErr(http.StatusBadRequest, "max_clients must be >= 0")
			}
			if req.RecycleDelaySec != nil && *req.RecycleDelaySec < 0 {
				return nil, NewEndpointErr(http.StatusBadRequest, "recycle_delay_sec must be >= 0")
			}
			if req.DrainSec != nil && *req.DrainSec < 0 {
				return nil, NewEndpointErr(http.StatusBadRequest, "drain_sec must be >= 0")
			}
			target := resolveTarget(req.Target, s.Name)

			userPoint := &proto.User{Name: req.Name}
			// Map JSON max_clients to proto sentinel:
			// JSON 0 → proto -1 (remove limit)
			// JSON >0 → proto value (set limit)
			mc := int32(*req.MaxClients)
			if mc == 0 {
				mc = -1
			}
			userPoint.MaxClients = mc
			if req.RecycleDelaySec != nil {
				userPoint.ClientRecycleDelaySec = int32(*req.RecycleDelaySec)
			}
			if req.DrainSec != nil {
				userPoint.ClientDrainSec = int32(*req.DrainSec)
			}

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
				log.Errorf("[SetUserClientLimit] Err=%s|User=%s", errMsg, req.Name)
				return nil, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}

			log.Info("[SetUserClientLimit] updated", "username", req.Name)
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

// SetUserClientLimitHandler PUT /user/:name/client-limit — sets client limits.
// Body: {"max_clients": 3, "recycle_delay_sec": 60, "drain_sec": 2, "target": ""}
// All three fields (max_clients, recycle_delay_sec, drain_sec) must be specified together.
// max_clients=0 means remove the limit (unlimited).
type SetUserClientLimitHandler struct{ HttpHandlerImp }

func (handler *SetUserClientLimitHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().setUserClientLimitEndpoint())(c)
}

func (handler *SetUserClientLimitHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *SetUserClientLimitHandler) getRelativePath() string {
	return "/user/:name/client-limit"
}

func (handler *SetUserClientLimitHandler) help() string {
	return setUserClientLimitHelp
}
