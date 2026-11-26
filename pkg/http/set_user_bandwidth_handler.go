package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const setUserBandwidthHelp = `PUT /api/user/:name/bandwidth
	Header: X-Token or Authorization: Bearer <admin-jwt>
	Body: {"upload_bps": 1000000, "download_bps": 500000, "target": ""}
	Sets bandwidth limits (bytes/sec). 0 removes the limit (unlimited).`

// SetUserBandwidthReq is the request payload for PUT /user/:name/bandwidth (MCP tool: set_user_bandwidth).
type SetUserBandwidthReq struct {
	Name        string `json:"name" path:"name" jsonschema:"Username whose bandwidth limits will be updated."`
	UploadBps   *int64 `json:"upload_bps,omitempty" jsonschema:"Upload bandwidth limit in bytes/sec (0 removes the limit; omit to leave unchanged)."`
	DownloadBps *int64 `json:"download_bps,omitempty" jsonschema:"Download bandwidth limit in bytes/sec (0 removes the limit; omit to leave unchanged)."`
	Target      string `json:"target,omitempty" jsonschema:"Target cluster node name or node_id, or 'all' (defaults to local node)."`
}

func (s *HttpServer) setUserBandwidthEndpoint() Endpoint[SetUserBandwidthReq, map[string]any] {
	return Endpoint[SetUserBandwidthReq, map[string]any]{
		Method:         http.MethodPut,
		Path:           "/user/:name/bandwidth",
		Scope:          ScopeAdmin,
		Help:           setUserBandwidthHelp,
		MCPName:        "set_user_bandwidth",
		MCPDescription: "Set or remove upload/download bandwidth limits (bytes/sec) for a user on target cluster node(s). Pass 0 to remove a limit, or omit a field to leave it unchanged.",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req SetUserBandwidthReq) (map[string]any, error) {
			if req.Name == "" {
				return nil, NewEndpointErr(http.StatusBadRequest, "username required")
			}
			if req.UploadBps == nil && req.DownloadBps == nil {
				return nil, NewEndpointErr(http.StatusBadRequest, "at least one of upload_bps or download_bps required")
			}
			if req.UploadBps != nil && *req.UploadBps < 0 {
				return nil, NewEndpointErr(http.StatusBadRequest, "upload_bps must be >= 0")
			}
			if req.DownloadBps != nil && *req.DownloadBps < 0 {
				return nil, NewEndpointErr(http.StatusBadRequest, "download_bps must be >= 0")
			}
			target := resolveTarget(req.Target, s.Name)

			userPoint := &proto.User{Name: req.Name}
			// Map JSON values to proto sentinel convention:
			// JSON null/absent → proto 0 (no change)
			// JSON 0 → proto -1 (remove limit)
			// JSON >0 → proto value (set limit)
			if req.UploadBps != nil {
				v := *req.UploadBps
				if v == 0 {
					v = -1
				}
				userPoint.UploadBps = v
			}
			if req.DownloadBps != nil {
				v := *req.DownloadBps
				if v == 0 {
					v = -1
				}
				userPoint.DownloadBps = v
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
				log.Errorf("[SetUserBandwidth] Err=%s|User=%s", errMsg, req.Name)
				return nil, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}

			log.Info("[SetUserBandwidth] updated", "username", req.Name)
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

// SetUserBandwidthHandler PUT /user/:name/bandwidth — sets bandwidth limits.
// Body: {"upload_bps": 1000000, "download_bps": 500000, "target": ""}
// A value of 0 means remove the limit (unlimited). Omitting a field means no change.
type SetUserBandwidthHandler struct{ HttpHandlerImp }

func (handler *SetUserBandwidthHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().setUserBandwidthEndpoint())(c)
}

func (handler *SetUserBandwidthHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *SetUserBandwidthHandler) getRelativePath() string {
	return "/user/:name/bandwidth"
}

func (handler *SetUserBandwidthHandler) help() string {
	return setUserBandwidthHelp
}
