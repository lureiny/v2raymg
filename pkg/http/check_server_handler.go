package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const setPingCheckHelp = `PUT /api/pingCheck
	设置ping检测
	body: {"target": "", "enable_ping_check": false}
	enable_ping_check: 是否开启ping检测`

// SetPingCheckReq is the request payload for PUT /pingCheck (MCP tool: set_ping_check).
type SetPingCheckReq struct {
	Target          string `json:"target,omitempty" jsonschema:"Target cluster node name, node_id, or 'all' (defaults to current node when empty)."`
	EnablePingCheck bool   `json:"enable_ping_check" jsonschema:"Whether to enable ping health checking on the target node(s)."`
}

func (s *HttpServer) setPingCheckEndpoint() Endpoint[SetPingCheckReq, map[string]any] {
	return Endpoint[SetPingCheckReq, map[string]any]{
		Method:         http.MethodPut,
		Path:           "/pingCheck",
		Scope:          ScopeAdmin,
		Help:           setPingCheckHelp,
		MCPName:        "set_ping_check",
		MCPDescription: "Enable or disable ping health checking on the target cluster node(s).",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req SetPingCheckReq) (map[string]any, error) {
			target := resolveTarget(req.Target, s.Name)

			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(http.StatusBadGateway, "no available node")
			}

			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.SetPingCheckType, &proto.SetPingCheckReq{
				EnablePingCheck: req.EnablePingCheck,
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

type PingCheckHandler struct{ HttpHandlerImp }

func (handler *PingCheckHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().setPingCheckEndpoint())(c)
}

func (handler *PingCheckHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *PingCheckHandler) getRelativePath() string { return "/pingCheck" }

func (handler *PingCheckHandler) help() string {
	return setPingCheckHelp
}
