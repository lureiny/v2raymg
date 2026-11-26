package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const updateNodeHelp = `POST /api/update
	更新目标节点的proxy版本
	body: {"target": "", "version_tag": "latest"}
	version_tag: github上目标tag, 默认为最新版`

// UpdateNodeReq is the request payload for POST /update (MCP tool: update_node_binary).
type UpdateNodeReq struct {
	Target     string `json:"target,omitempty" jsonschema:"Target cluster node name, node_id, or 'all' (defaults to current node when empty)."`
	VersionTag string `json:"version_tag,omitempty" default:"latest" jsonschema:"GitHub release version tag to update the proxy binary to (defaults to 'latest')."`
}

func (s *HttpServer) updateNodeEndpoint() Endpoint[UpdateNodeReq, map[string]any] {
	return Endpoint[UpdateNodeReq, map[string]any]{
		Method:         http.MethodPost,
		Path:           "/update",
		Scope:          ScopeAdmin,
		Help:           updateNodeHelp,
		MCPName:        "update_node_binary",
		MCPDescription: "WARNING: Destructive operation — downloads and swaps the proxy binary on the target cluster node(s) to the requested GitHub release version_tag (defaults to 'latest') and triggers a binary update/restart.",
		ReadOnly:       false,
		Destructive:    true,
		Execute: func(ctx context.Context, caller Caller, req UpdateNodeReq) (map[string]any, error) {
			target := resolveTarget(req.Target, s.Name)
			versionTag := req.VersionTag
			if versionTag == "" {
				versionTag = "latest"
			}

			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(http.StatusBadGateway, "no available node")
			}

			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx,
				client.UpdateProxyReqType,
				&proto.UpdateProxyReq{Tag: versionTag},
				s.GetClusterToken(),
			)
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|Target=%s|Tag=%s", errMsg, target, versionTag)
				return nil, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

type UpdateHandler struct{ HttpHandlerImp }

func (handler *UpdateHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().updateNodeEndpoint())(c)
}

func (handler *UpdateHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *UpdateHandler) getRelativePath() string { return "/update" }

func (handler *UpdateHandler) help() string {
	return updateNodeHelp
}
