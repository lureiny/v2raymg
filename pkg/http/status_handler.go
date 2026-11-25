package http

import (
	"context"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

const statusHelp = `GET /api/status
	Header: Authorization: Bearer <token> 或 X-Token: <token>
	query: target — 目标节点名（默认当前节点，"all" 查询所有节点）
	返回: {"nodes": [{"node_name":"...", "only_gateway":false, "cluster_user_enabled":true, "mem_alloc":..., "mem_sys":..., "mem_heap_inuse":..., "num_goroutine":..., "cpu_percent":..., "net_rx_bytes":..., "net_tx_bytes":..., "tcp_connections":...}], "failed": {"node2": "error msg"}}`

// StatusReq is the request payload for GET /status (MCP tool: get_node_status).
type StatusReq struct {
	Target string `json:"target,omitempty" query:"target" default:"all" jsonschema:"Target cluster node name or node_id, or 'all' to query all available cluster nodes (defaults to 'all')."`
}

// NodeStatusItem holds runtime status metrics for a single cluster node.
type NodeStatusItem struct {
	NodeName           string  `json:"node_name"`
	OnlyGateway        bool    `json:"only_gateway"`
	ClusterUserEnabled bool    `json:"cluster_user_enabled"`
	MemTotal           uint64  `json:"mem_total"`
	MemUsed            uint64  `json:"mem_used"`
	MemAvailable       uint64  `json:"mem_available"`
	NumGoroutine       int32   `json:"num_goroutine"`
	CpuPercent         float64 `json:"cpu_percent"`
	NetRxBytes         uint64  `json:"net_rx_bytes"`
	NetTxBytes         uint64  `json:"net_tx_bytes"`
	TcpConnections     int32   `json:"tcp_connections"`
	NetRxSpeed         float64 `json:"net_rx_speed"`
	NetTxSpeed         float64 `json:"net_tx_speed"`
	Version            string  `json:"version"`
}

// StatusResp is the response payload for GET /status.
type StatusResp struct {
	Nodes  []NodeStatusItem  `json:"nodes"`
	Failed map[string]string `json:"failed"`
}

func (s *HttpServer) statusEndpoint() Endpoint[StatusReq, StatusResp] {
	return Endpoint[StatusReq, StatusResp]{
		Method:         http.MethodGet,
		Path:           "/status",
		Scope:          ScopeUser,
		Help:           statusHelp,
		MCPName:        "get_node_status",
		MCPDescription: "Get runtime status metrics (CPU, memory, goroutines, network traffic and speed, TCP connections, version, and gateway/cluster-user flags) for target cluster nodes.",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req StatusReq) (StatusResp, error) {
			target := req.Target
			if target == "" {
				target = "all"
			}
			nodes := s.GetTargetNodes(target)
			if len(nodes) == 0 {
				return StatusResp{}, NewEndpointErr(http.StatusBadGateway, "no available node")
			}

			rpcClient := client.NewEndNodeClient(nodes, s.GetLocalNode())
			if rpcClient == nil {
				return StatusResp{}, NewEndpointErr(http.StatusInternalServerError, "local node not initialized")
			}
			succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
				ctx,
				client.GetStatusReqType,
				&proto.GetStatusReq{},
				s.GetClusterToken(),
			)

			if failedList == nil {
				failedList = map[string]string{}
			}

			nodesList := make([]NodeStatusItem, 0, len(succList))
			for nodeName, v := range succList {
				ns, ok := v.(*proto.NodeStatus)
				if !ok || ns == nil {
					failedList[nodeName] = "unexpected response type"
					continue
				}
				nodesList = append(nodesList, NodeStatusItem{
					NodeName:           ns.GetNodeName(),
					OnlyGateway:        ns.GetOnlyGateway(),
					ClusterUserEnabled: ns.GetClusterUserEnabled(),
					MemTotal:           ns.GetMemTotal(),
					MemUsed:            ns.GetMemUsed(),
					MemAvailable:       ns.GetMemAvailable(),
					NumGoroutine:       ns.GetNumGoroutine(),
					CpuPercent:         ns.GetCpuPercent(),
					NetRxBytes:         ns.GetNetRxBytes(),
					NetTxBytes:         ns.GetNetTxBytes(),
					TcpConnections:     ns.GetTcpConnections(),
					NetRxSpeed:         ns.GetNetRxSpeed(),
					NetTxSpeed:         ns.GetNetTxSpeed(),
					Version:            ns.GetVersion(),
				})
			}
			sort.Slice(nodesList, func(i, j int) bool {
				return nodesList[i].NodeName < nodesList[j].NodeName
			})

			if len(failedList) != 0 {
				errMsg := joinFailedList(failedList)
				log.Errorf("Err=%s|OpType=getStatus|Target=%s", errMsg, target)
			}

			return StatusResp{
				Nodes:  nodesList,
				Failed: failedList,
			}, nil
		},
	}
}

type StatusHandler struct{ HttpHandlerImp }

func (handler *StatusHandler) handlerFunc(c *gin.Context) {
	endpointHandlerFunc(handler.getHttpServer().statusEndpoint())(c)
}

func (handler *StatusHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *StatusHandler) getRelativePath() string { return "/status" }

func (handler *StatusHandler) help() string {
	return statusHelp
}
