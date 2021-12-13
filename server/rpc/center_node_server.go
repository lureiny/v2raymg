package rpc

import (
	context "context"
	"fmt"
	"net"
	"time"

	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/server"

	"github.com/lureiny/v2raymg/server/rpc/proto"
	grpc "google.golang.org/grpc"
)

type NodeMap map[string]*proto.Node

var configManager = common.GetGlobalConfigManager()

type CenterNodeServer struct {
	proto.UnimplementedCenterNodeAccessServer
	clusters common.CenterClusterManager
	server.ServerConfig
	localNode proto.Node
}

var logger = common.LoggerImp

func (s *CenterNodeServer) HeartBeat(ctx context.Context, heartBeatReq *proto.HeartBeatReq) (*proto.HeartBeatRsp, error) {
	heartBeatRsp := &proto.HeartBeatRsp{}
	node := &common.Node{
		Node:             heartBeatReq.GetNodeAuthInfo().GetNode(),
		CreateTime:       time.Now().Unix(),
		GetHeartBeatTime: time.Now().Unix(),
	}
	if !node.IsComplete() {
		logger.Error(
			"Err=%s|Src=%s:%d|SrcName=%s|Cluster=%s",
			"not complete node",
			node.GetHost(),
			node.GetPort(),
			node.GetName(),
			node.GetClusterName(),
		)
		heartBeatRsp.Code = 500
		heartBeatRsp.Msg = "not complete node"
		return heartBeatRsp, nil
	}
	logger.Info(
		"Src=%s:%d|SrcName=%s|Cluster=%s",
		node.GetHost(),
		node.GetPort(),
		node.GetName(),
		node.GetClusterName(),
	)
	nodeName := node.GetName()
	clusterName := node.GetClusterName()
	if cluster := s.clusters.GetCluster(clusterName); cluster != nil {
		// 集群已经存在
		if n := cluster.Get(nodeName); n != nil {
			// 存在该节点, 更新探活时间
			n.GetHeartBeatTime = time.Now().Unix()
		} else {
			logger.Error(
				"Msg=%s|Src=%s:%d|SrcName=%s|Cluster=%s",
				"New Node",
				node.GetHost(),
				node.GetPort(),
				node.GetName(),
				clusterName,
			)
			cluster.Add(node)
		}
		// 只返回有效节点
		heartBeatRsp.NodesMap = cluster.GetNodes(true)
	} else {
		// 集群不存在，创建新集群并添加节点
		logger.Info("Msg=create new cluster: %s", clusterName)
		logger.Info(
			"Msg=%s|Src=%s:%d|SrcName=%s|Cluster=%s",
			"new node register",
			node.Host,
			node.Port,
			nodeName,
			clusterName,
		)
		s.clusters.Add(clusterName, node)
	}
	return heartBeatRsp, nil
}

// 过滤各个集群下的无效节点
func (s *CenterNodeServer) filter() {
	// 10s 过滤一次
	clearCycle := time.Second * 10
	timeTicker := time.NewTicker(clearCycle)
	for {
		<-timeTicker.C
		logger.Info("Msg=filter invalid node")
		s.clusters.Filter()
	}
}

func (s *CenterNodeServer) Init() {
	s.Host = configManager.GetString("server.listen")
	s.Port = configManager.GetInt("server.rpc.port")
	s.Type = "Center"
	serverName := configManager.GetString("server.name")
	accessHost := configManager.GetString("proxy.host")
	if serverName == "" {
		serverName = fmt.Sprintf("%s:%d", accessHost, s.Port)
	}
	s.Name = serverName
	s.clusters.Init()
	logger.Init()
	logger.SetLogLevel(0)
	logger.SetServerName(serverName)
	logger.SetNodeType("Center")
}

func (s *CenterNodeServer) Start() {
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		errMsg := fmt.Sprintf("failed to listen: %v", err)
		logger.Fatalf(
			"Err=%s|Addr=%s",
			errMsg,
			addr,
		)
	}
	grpcServer := grpc.NewServer()
	proto.RegisterCenterNodeAccessServer(grpcServer, s)
	go s.filter()
	logger.Info("Msg=center node server listening at %v", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		errMsg := fmt.Sprintf("failed to serve > %v", err)
		logger.Fatalf(
			"Err=%s|Addr=%d:%s",
			errMsg,
			s.Host,
			s.Port,
		)
	}
}
