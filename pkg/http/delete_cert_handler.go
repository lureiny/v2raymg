package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const deleteCertHelp = `DELETE /api/cert
	删除证书
	body: {"target": "", "domain": ""}`

// DeleteCertReq is the request payload for DELETE /cert (MCP tool: delete_cert).
type DeleteCertReq struct {
	Target string `json:"target,omitempty" jsonschema:"Target cluster node name, node_id, or 'all' (defaults to current node when empty)."`
	Domain string `json:"domain" jsonschema:"Domain name of the TLS certificate to delete (required)."`
}

func (s *HttpServer) deleteCertEndpoint() Endpoint[DeleteCertReq, map[string]any] {
	return Endpoint[DeleteCertReq, map[string]any]{
		Method:         http.MethodDelete,
		Path:           "/cert",
		Scope:          ScopeAdmin,
		Help:           deleteCertHelp,
		MCPName:        "delete_cert",
		MCPDescription: "Delete a managed TLS certificate for a domain from the target cluster node(s).",
		ReadOnly:       false,
		Destructive:    true,
		Execute: func(ctx context.Context, caller Caller, req DeleteCertReq) (map[string]any, error) {
			if req.Domain == "" {
				return nil, NewEndpointErr(http.StatusBadRequest, "domain is required")
			}
			target := resolveTarget(req.Target, s.Name)
			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(http.StatusBadGateway, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.DeleteCertReqType, &proto.DeleteCertReq{Domain: req.Domain}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s", errMsg)
				return nil, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

type DeleteCertHandler struct{ HttpHandlerImp }

func (handler *DeleteCertHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().deleteCertEndpoint())(c)
}

func (handler *DeleteCertHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *DeleteCertHandler) getRelativePath() string { return "/cert" }

func (handler *DeleteCertHandler) help() string {
	return deleteCertHelp
}
