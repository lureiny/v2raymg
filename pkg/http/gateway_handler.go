package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const setGatewayHelp = `PUT /api/gateway
	设置gateway模式
	body: {"target": "", "enable_gateway_model": false}
	enable_gateway_model: 是否开启gateway模式`

// SetGatewayReq is the request payload for PUT /gateway (MCP tool: set_gateway).
type SetGatewayReq struct {
	Target             string `json:"target,omitempty" jsonschema:"Target cluster node name, node_id, or 'all' (defaults to current node when empty)."`
	EnableGatewayModel bool   `json:"enable_gateway_model" jsonschema:"Whether to enable gateway-only mode on the target node(s)."`
}

func (s *HttpServer) setGatewayEndpoint() Endpoint[SetGatewayReq, map[string]any] {
	return Endpoint[SetGatewayReq, map[string]any]{
		Method:         http.MethodPut,
		Path:           "/gateway",
		Scope:          ScopeAdmin,
		Help:           setGatewayHelp,
		MCPName:        "set_gateway",
		MCPDescription: "Enable or disable gateway-only mode on the target cluster node(s).",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req SetGatewayReq) (map[string]any, error) {
			target := resolveTarget(req.Target, s.Name)

			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(http.StatusBadGateway, "no available node")
			}

			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.SetGatewayModelReqType, &proto.SetGatewayModelReq{
				EnableGatewayModel: req.EnableGatewayModel,
			}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|Target=%s", errMsg, target)
				return nil, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

type GatewayHandler struct{ HttpHandlerImp }

func (handler *GatewayHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().setGatewayEndpoint())(c)
}

func (handler *GatewayHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *GatewayHandler) getRelativePath() string { return "/gateway" }

func (handler *GatewayHandler) help() string {
	return setGatewayHelp
}
