package server

import (
	context "context"
	"fmt"
	"net"
	"time"

	"github.com/lureiny/v2raymg/pkg/common"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"

	c "github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	grpc "google.golang.org/grpc"
)

type NodeMap map[string]*proto.Node

type CenterNodeServer struct {
	proto.UnimplementedCenterNodeAccessServer
	clusters c.CenterClusterManager

	// ServerConfig fields (inlined)
	Host string
	Port int
	Type string
	Name string
}

func (s *CenterNodeServer) HeartBeat(ctx context.Context, heartBeatReq *proto.HeartBeatReq) (*proto.HeartBeatRsp, error) {
	heartBeatRsp := &proto.HeartBeatRsp{}

	// Validate heartbeat timestamp (skip if 0 for backward compatibility).
	if ts := heartBeatReq.GetTimestampUs(); ts != 0 {
		drift := time.Now().UnixMicro() - ts
		if drift < 0 {
			drift = -drift
		}
		if drift > heartbeatMaxDriftUs {
			heartBeatRsp.Code = 105
			heartBeatRsp.Msg = "heartbeat timestamp drift too large"
			log.Error("heartbeat rejected", "err", "timestamp drift exceeds 30s",
				"src_name", heartBeatReq.GetNodeAuthInfo().GetNode().GetName(),
				"drift_ms", drift/1000)
			return heartBeatRsp, nil
		}
	}

	node := &c.Node{
		Node:             heartBeatReq.GetNodeAuthInfo().GetNode(),
		CreateTime:       time.Now().Unix(),
		GetHeartBeatTime: time.Now().Unix(),
	}
	if !node.IsComplete() {
		log.Error("heartbeat: not complete node",
			"src", fmt.Sprintf("%s:%d", node.GetHost(), node.GetPort()),
			"name", node.GetName(), "cluster", node.GetClusterName())
		heartBeatRsp.Code = 500
		heartBeatRsp.Msg = "not complete node"
		return heartBeatRsp, nil
	}
	log.Info("heartbeat received",
		"src", fmt.Sprintf("%s:%d", node.GetHost(), node.GetPort()),
		"name", node.GetName(), "cluster", node.GetClusterName())
	nodeName := node.GetName()
	clusterName := node.GetClusterName()
	if cluster := s.clusters.GetCluster(clusterName); cluster != nil {
		if n := cluster.Get(nodeName); n != nil {
			n.GetHeartBeatTime = time.Now().Unix()
		} else {
			log.Info("new node joining cluster",
				"src", fmt.Sprintf("%s:%d", node.GetHost(), node.GetPort()),
				"name", node.GetName(), "cluster", clusterName)
			cluster.Add(node)
		}
		heartBeatRsp.NodesMap = cluster.GetProtoNodesWithFilter(
			func(node *c.Node) bool {
				return node.IsValid()
			},
		)
	} else {
		log.Info("creating new cluster", "cluster", clusterName,
			"src", fmt.Sprintf("%s:%d", node.Host, node.Port), "name", nodeName)
		s.clusters.Add(clusterName, node)
	}
	return heartBeatRsp, nil
}

func (s *CenterNodeServer) filter() {
	timeTicker := time.NewTicker(10 * time.Second)
	for {
		<-timeTicker.C
		log.Info("filtering invalid nodes")
		s.clusters.Filter()
	}
}

// Init initializes the CenterNodeServer from CenterNodeConfig.
func (s *CenterNodeServer) Init(cfg appconfig.CenterNodeConfig) {
	s.Host = cfg.Listen
	s.Port = cfg.RpcPort
	s.Type = common.CenterNodeType
	name := cfg.Name
	if name == "" {
		name = fmt.Sprintf("%s:%d", cfg.ProxyHost, cfg.RpcPort)
	}
	s.Name = name
	s.clusters.Init()
}

func (s *CenterNodeServer) Start() {
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("center node failed to listen", "addr", addr, "err", err)
		return
	}
	grpcServer := grpc.NewServer()
	proto.RegisterCenterNodeAccessServer(grpcServer, s)
	go s.filter()
	log.Info("center node server listening", "addr", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		log.Error("center node failed to serve", "addr", addr, "err", err)
	}
}
