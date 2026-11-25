package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const applyCertHelp = `POST /api/cert
	申请证书
	body: {"target": "", "domain": ""}`

// ApplyCertReq is the request payload for POST /cert (MCP tool: apply_cert).
type ApplyCertReq struct {
	Target string `json:"target,omitempty" jsonschema:"Target cluster node name, node_id, or 'all' (defaults to current node when empty)."`
	Domain string `json:"domain" jsonschema:"Domain name to obtain a TLS certificate for."`
}

func (s *HttpServer) applyCertEndpoint() Endpoint[ApplyCertReq, map[string]any] {
	return Endpoint[ApplyCertReq, map[string]any]{
		Method:         http.MethodPost,
		Path:           "/cert",
		Scope:          ScopeAdmin,
		Help:           applyCertHelp,
		MCPName:        "apply_cert",
		MCPDescription: "Apply for (obtain) a new TLS certificate for a domain via ACME on the target cluster node(s).",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req ApplyCertReq) (map[string]any, error) {
			target := resolveTarget(req.Target, s.Name)
			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return nil, NewEndpointErr(http.StatusBadGateway, "no available node")
			}
			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			_, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx, client.ObtainNewCertType, &proto.ObtainNewCertReq{Domain: req.Domain}, s.GetClusterToken())
			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s", errMsg)
				return nil, NewEndpointErr(http.StatusInternalServerError, errMsg)
			}
			return map[string]any{"code": 0, "msg": "ok"}, nil
		},
	}
}

type CertHandler struct{ HttpHandlerImp }

func (handler *CertHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().applyCertEndpoint())(c)
}

func (handler *CertHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *CertHandler) getRelativePath() string { return "/cert" }

func (handler *CertHandler) help() string {
	return applyCertHelp
}
