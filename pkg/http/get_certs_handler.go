package http

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const certsListHelp = `/api/getCerts
	获取证书列表
	/api/getCerts?target={target}
	target: 目标节点
	`

// GetCertsReq is the request payload for GET /api/getCerts and MCP tool list_certs.
type GetCertsReq struct {
	Target string `json:"target,omitempty" query:"target" default:"all" jsonschema:"Target node name, node_id, or 'all' to query every cluster node (defaults to 'all')"`
}

func (s *HttpServer) certsListEndpoint() Endpoint[GetCertsReq, any] {
	return Endpoint[GetCertsReq, any]{
		Method:         "GET",
		Path:           "/getCerts",
		Scope:          ScopeAdmin,
		Help:           certsListHelp,
		MCPName:        "list_certs",
		MCPDescription: "List TLS certificates managed on the target cluster node(s). Target defaults to 'all' (every cluster node) or accepts a specific node name or node_id.",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req GetCertsReq) (any, error) {
			target := req.Target
			if target == "" {
				target = "all"
			}
			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(502, "no available node")
			}

			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx,
				client.GetCertsType,
				&proto.GetCertsReq{},
				s.GetClusterToken(),
			)

			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|Target=%s", errMsg, target)
			}

			return succList, nil
		},
	}
}

type GetCertsHandler struct{ HttpHandlerImp }

func (handler *GetCertsHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().certsListEndpoint())(c)
}

func (handler *GetCertsHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		handler.handlerFunc,
	}
}

func (handler *GetCertsHandler) getRelativePath() string {
	return "/getCerts"
}

func (handler *GetCertsHandler) help() string {
	return certsListHelp
}
