package http

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const transferCertHelp = `POST /api/cert/transfer
	将本机证书文件传输到指定节点上
	body: {"target": "", "domain": ""}`

// TransferCertReq is the request payload for POST /cert/transfer (MCP tool: transfer_cert).
type TransferCertReq struct {
	Target string `json:"target,omitempty" jsonschema:"Target peer cluster node name, node_id, or 'all' (current node is excluded; defaults to current node name when empty)."`
	Domain string `json:"domain" jsonschema:"Domain name whose local certificate and private key will be transferred."`
}

func (s *HttpServer) transferCertEndpoint() Endpoint[TransferCertReq, map[string]any] {
	return Endpoint[TransferCertReq, map[string]any]{
		Method:         http.MethodPost,
		Path:           "/cert/transfer",
		Scope:          ScopeAdmin,
		Help:           transferCertHelp,
		MCPName:        "transfer_cert",
		MCPDescription: "Transfer an existing local TLS certificate and private key for a domain from the current node to target peer cluster node(s).",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req TransferCertReq) (map[string]any, error) {
			target := resolveTarget(req.Target, s.Name)
			domain := req.Domain

			cr := s.certReader
			if cr == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "cert reader not configured")
			}
			certFile, keyFile, ok := cr.GetCertFiles(domain)
			if !ok {
				return nil, NewEndpointErr(http.StatusNotFound, fmt.Sprintf("can't find domain's[%s] cert", domain))
			}

			certData, err := os.ReadFile(certFile)
			if err != nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, fmt.Sprintf("read cert file err > %v", err))
			}
			keyData, err := os.ReadFile(keyFile)
			if err != nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, fmt.Sprintf("read key file err > %v", err))
			}

			nodes := s.GetTargetNodes(target)
			filtered := make([]*cluster.Node, 0, len(nodes))
			for _, node := range nodes {
				if node.Name != s.Name {
					filtered = append(filtered, node)
				}
			}
			if len(filtered) == 0 {
				return nil, NewEndpointErr(http.StatusBadGateway, "no available node")
			}

			rpcClient := client.NewEndNodeClient(filtered, s.GetLocalNode())
			if rpcClient == nil {
				return nil, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(ctx,
				client.TransferCertType,
				&proto.TransferCertReq{
					Domain:   domain,
					CertData: certData,
					KeyDatas: keyData,
				},
				s.GetClusterToken(),
			)
			if len(failedList) != 0 {
				log.Error("transfer cert failed", "err", joinFailedList(failedList), "target", target)
			}
			return succList, nil
		},
	}
}

type TransferCertHandler struct{ HttpHandlerImp }

func (handler *TransferCertHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().transferCertEndpoint())(c)
}

func (handler *TransferCertHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *TransferCertHandler) getRelativePath() string { return "/cert/transfer" }

func (handler *TransferCertHandler) help() string {
	return transferCertHelp
}
