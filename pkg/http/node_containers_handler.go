package http

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const containersHelp = `GET /api/containers?target=<node|all> 获取节点已启用的 container 类型列表`

// ContainersReq is the request payload for GET /containers (MCP tool: list_containers).
type ContainersReq struct {
	Target string `json:"target,omitempty" query:"target" default:"all" jsonschema:"Target cluster node name or node_id, or 'all' to query all available cluster nodes (defaults to 'all')."`
}

// ContainerDetail describes a container type and whether it supports chain outbounds.
type ContainerDetail struct {
	Type          string `json:"type"`
	SupportsChain bool   `json:"supports_chain"`
}

// ContainersResp is the response payload for GET /containers.
type ContainersResp struct {
	Nodes   map[string][]string          `json:"nodes"`
	Failed  map[string]string            `json:"failed"`
	Details map[string][]ContainerDetail `json:"details,omitempty"`
}

func (s *HttpServer) containersEndpoint() Endpoint[ContainersReq, ContainersResp] {
	return Endpoint[ContainersReq, ContainersResp]{
		Method:         http.MethodGet,
		Path:           "/containers",
		Scope:          ScopeAdmin,
		Help:           containersHelp,
		MCPName:        "list_containers",
		MCPDescription: "List enabled proxy container types (such as xray, hysteria, snell, mihomo) on target cluster nodes.",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req ContainersReq) (ContainersResp, error) {
			target := req.Target
			if target == "" {
				target = "all"
			}
			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return ContainersResp{}, NewEndpointErr(http.StatusBadGateway, "no available node")
			}

			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return ContainersResp{}, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
				ctx,
				client.GetContainersReqType,
				&proto.GetContainersReq{},
				s.GetClusterToken(),
			)

			if failedList == nil {
				failedList = map[string]string{}
			}

			result := make(map[string][]string, len(succList))
			var details map[string][]ContainerDetail
			for nodeName, v := range succList {
				switch resp := v.(type) {
				case *proto.GetContainersRsp:
					cs := append([]string(nil), resp.GetContainers()...)
					if len(cs) == 0 && len(resp.GetContainerInfos()) > 0 {
						cs = make([]string, 0, len(resp.GetContainerInfos()))
						for _, info := range resp.GetContainerInfos() {
							cs = append(cs, info.GetType())
						}
					}
					result[nodeName] = cs
					if infos := resp.GetContainerInfos(); len(infos) > 0 {
						if details == nil {
							details = make(map[string][]ContainerDetail)
						}
						nodeDetails := make([]ContainerDetail, 0, len(infos))
						for _, info := range infos {
							nodeDetails = append(nodeDetails, ContainerDetail{
								Type:          info.GetType(),
								SupportsChain: info.GetSupportsChain(),
							})
						}
						details[nodeName] = nodeDetails
					}
				case []string:
					result[nodeName] = resp
				default:
					failedList[nodeName] = "unexpected response type"
				}
			}

			if len(failedList) != 0 {
				log.Errorf("Err=%s|OpType=getContainers|Target=%s", joinFailedList(failedList), target)
			}

			return ContainersResp{
				Nodes:   result,
				Failed:  failedList,
				Details: details,
			}, nil
		},
	}
}

type ContainersHandler struct{ HttpHandlerImp }

func (handler *ContainersHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().containersEndpoint())(c)
}

func (handler *ContainersHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *ContainersHandler) getRelativePath() string { return "/containers" }

func (handler *ContainersHandler) help() string {
	return containersHelp
}
