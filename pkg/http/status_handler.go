package http

import (
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type StatusHandler struct{ HttpHandlerImp }

func (handler *StatusHandler) handlerFunc(c *gin.Context) {
	target := getTargetFromQuery(c)
	nodes := handler.getHttpServer().GetTargetNodes(target)
	if len(nodes) == 0 {
		jsonErr(c, 502, "no available node")
		return
	}

	rpcClient := client.NewEndNodeClient(nodes, handler.getHttpServer().GetLocalNode())
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		c.Request.Context(),
		client.GetStatusReqType,
		&proto.GetStatusReq{},
		handler.getHttpServer().GetClusterToken(),
	)

	type nodeStatusJSON struct {
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

	if failedList == nil {
		failedList = map[string]string{}
	}

	nodesList := make([]nodeStatusJSON, 0, len(succList))
	for nodeName, v := range succList {
		ns, ok := v.(*proto.NodeStatus)
		if !ok || ns == nil {
			failedList[nodeName] = "unexpected response type"
			continue
		}
		nodesList = append(nodesList, nodeStatusJSON{
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

	c.JSON(200, gin.H{
		"nodes":  nodesList,
		"failed": failedList,
	})
}

func (handler *StatusHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *StatusHandler) getRelativePath() string { return "/status" }

func (handler *StatusHandler) help() string {
	return `GET /api/status
	Header: Authorization: Bearer <token> 或 X-Token: <token>
	query: target — 目标节点名（默认当前节点，"all" 查询所有节点）
	返回: {"nodes": [{"node_name":"...", "only_gateway":false, "cluster_user_enabled":true, "mem_alloc":..., "mem_sys":..., "mem_heap_inuse":..., "num_goroutine":..., "cpu_percent":..., "net_rx_bytes":..., "net_tx_bytes":..., "tcp_connections":...}], "failed": {"node2": "error msg"}}`
}
