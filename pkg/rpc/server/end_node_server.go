package server

import (
	context "context"
	"fmt"
	"net"
	"reflect"
	"strings"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/common"
	"github.com/lureiny/v2raymg/pkg/common/rpc"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/subscription"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"

	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

var endNodeServer = &EndNodeServer{}
var localNode = cluster.GetLocalNode()

type EndNodeServer struct {
	proto.UnimplementedEndNodeAccessServer
	cfg         appconfig.EndNodeConfig
	centerNode  *cluster.Node
	certManager CertManager

	// ServerConfig fields (inlined from server.ServerConfig)
	Host string
	Port int
	Type string
	Name string

	// injected cluster state
	clusterState ClusterState

	// injected metric collectors
	nodeMetricCol  NodeMetricCollector
	pingCollector  PingCollector
	statsCollector BandwidthStatsCollector

	// injected dependencies
	userMgr      *usermanager.UserManager
	containerMgr *container.ContainerMgr
	subMgr       *subscription.Manager

}

const (
	heartbeatInterval        = 10 * time.Second
	clearInvalidNodeInterval = 20 * time.Second
	heartbeatMaxDriftUs      = 30 * 1000 * 1000 // 30 seconds in microseconds
)

func GetEndNodeServer() *EndNodeServer {
	return endNodeServer
}

var methodPrefixLen = len("/proto.EndNodeAccess/")

var onlyGatewayMethods = "HeartBeat|RegisterNode|SetGatewayModel|GetPingMetric|GetNodeMetric|GetBandWidthStats|SetPingCheck|GetStatus"

func isOnlyGatewayMethod(fullMethod string) bool {
	return strings.Contains(onlyGatewayMethods, fullMethod[methodPrefixLen:])
}

var methodRspMap = map[string]interface{}{
	"GetUsers":             &proto.GetUsersRsp{},
	"GetProfile":           &proto.GetProfileRsp{},
	"AddUsers":             &proto.UserOpRsp{},
	"DeleteUsers":          &proto.UserOpRsp{},
	"UpdateUsers":          &proto.UserOpRsp{},
	"ResetUser":            &proto.UserOpRsp{},
	"ResetAuthToken":       &proto.ResetAuthTokenRsp{},
	"RotateInboundPort":    &proto.RotateInboundPortRsp{},
	"RotateAllPorts":       &proto.RotateAllPortsRsp{},
	"GetSub":               &proto.GetSubRsp{},
	"GetBandWidthStats":    &proto.GetBandwidthStatsRsp{},
	"HeartBeat":            &proto.HeartBeatRsp{},
	"RegisterNode":         &proto.RegisterNodeRsp{},
	"SetGatewayModel":      &proto.SetGatewayModelRsp{},
	"SetPingCheck":         &proto.SetPingCheckRsp{},
	"AddInbound":              &proto.InboundOpRsp{},
	"TransferInbound":         &proto.InboundOpRsp{},
	"CopyInbound":             &proto.InboundOpRsp{},
	"CopyUser":                &proto.InboundOpRsp{},
	"GetInbound":              &proto.GetInboundRsp{},
	"ListInbound":             &proto.ListInboundRsp{},
	"DeleteInboundByName":     &proto.InboundOpRsp{},
	"UpdateProxy":             &proto.UpdateProxyRsp{},
	"AddAdaptiveConfig":       &proto.AdaptiveRsp{},
	"DeleteAdaptiveConfig":    &proto.AdaptiveRsp{},
	"Adaptive":                &proto.AdaptiveRsp{},
	"FastAddInbound":       &proto.FastAddInboundRsp{},
	"ObtainNewCert":        &proto.ObtainNewCertRsp{},
	"TransferCert":         &proto.TransferCertRsp{},
	"GetCerts":             &proto.GetCertsRsp{},
	"DeleteCert":           &proto.DeleteCertRsp{},
	"GetPingMetric":           &proto.GetPingMetricRsp{},
	"GetNodeMetric":           &proto.GetNodeMetricRsp{},
	"GetNodeGroups":           &proto.GetNodeGroupsRsp{},
	"SetNodeGroups":           &proto.SetNodeGroupsRsp{},
	"UpsertClusterUsers":      &proto.UpsertClusterUsersRsp{},
	"GetStatus":               &proto.GetStatusRsp{},
	"GetContainers":           &proto.GetContainersRsp{},
}

func newEmptyRsp(fullMethod string) (interface{}, error) {
	return methodRspMap[fullMethod[methodPrefixLen:]], nil
}

func (s *EndNodeServer) authRemoteNode(req interface{}, fullMethod string) (bool, interface{}, *proto.Node) {
	reqValue := reflect.ValueOf(req)
	nodeAuthInfo := reqValue.Elem().FieldByName("NodeAuthInfo").Elem().Interface().(proto.NodeAuthInfo)
	if fullMethod[methodPrefixLen:] == "RegisterNode" {
		return true, nil, nodeAuthInfo.Node
	}
	node := &cluster.Node{
		Node:    nodeAuthInfo.Node,
		InToken: nodeAuthInfo.Token,
	}
	if err := s.clusterState.AuthRemoteNode(&node); err != nil && localNode.Token != node.InToken {
		errMsg := fmt.Sprintf("auth err > %v", err)
		log.Error("auth remote node failed",
			"err", errMsg,
			"src_host", node.Host, "src_port", node.Port,
			"src_name", node.Name, "api", fullMethod[methodPrefixLen:],
		)
		rspValue := reflect.ValueOf(methodRspMap[fullMethod[methodPrefixLen:]])
		rspValue.Elem().FieldByName("Code").SetInt(400)
		rspValue.Elem().FieldByName("Msg").SetString(errMsg)
		return false, rspValue.Interface(), nodeAuthInfo.Node
	}
	return true, nil, nodeAuthInfo.Node
}

// unaryServerInterceptor returns a gRPC unary interceptor bound to this server instance.
func (s *EndNodeServer) unaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, hander grpc.UnaryHandler) (interface{}, error) {
		if s.cfg.OnlyGateway && !isOnlyGatewayMethod(info.FullMethod) {
			return newEmptyRsp(info.FullMethod)
		}
		authOK, rsp, node := s.authRemoteNode(req, info.FullMethod)
		if !authOK {
			return rsp, nil
		}
		startPoint := time.Now().UnixMilli()
		rsp, err := hander(ctx, req)
		log.Debug("rpc call",
			"src_host", node.Host, "src_port", node.Port, "src_name", node.Name,
			"api", info.FullMethod[methodPrefixLen:],
			"delay_ms", time.Now().UnixMilli()-startPoint,
		)
		return rsp, err
	}
}

func (s *EndNodeServer) Init(
	cfg appconfig.EndNodeConfig,
	certManager CertManager,
	clusterState ClusterState,
	userMgr *usermanager.UserManager,
	containerMgr *container.ContainerMgr,
	subMgr *subscription.Manager,
	statsCol BandwidthStatsCollector,
	pingCol PingCollector,
	nodeMetricCol NodeMetricCollector,
) {
	s.cfg = cfg
	s.certManager = certManager
	s.clusterState = clusterState
	s.userMgr = userMgr
	s.containerMgr = containerMgr
	s.subMgr = subMgr
	s.statsCollector = statsCol
	s.pingCollector = pingCol
	s.nodeMetricCol = nodeMetricCol

	s.Host = cfg.Listen
	s.Port = cfg.RpcPort
	s.Type = common.EndNodeType
	s.Name = cfg.Name

	InitNetSpeedMonitor(cfg.MonitorInterfaces)

	s.centerNode = &cluster.Node{
		Node: &proto.Node{
			Host: cfg.Cluster.CenterNodeHost,
			Port: int32(cfg.Cluster.CenterNodePort),
		},
	}
}


func (s *EndNodeServer) filter() {
	timeTicker := time.NewTicker(clearInvalidNodeInterval)
	for {
		<-timeTicker.C
		log.Debug("filter invalid nodes")
		s.clusterState.Filter(func(n *cluster.Node) bool {
			return n.IsValid() || n.IsLocal()
		})
	}
}

func isAddrValid(host string, port int) bool {
	return host != "" && port >= 1000
}

func (s *EndNodeServer) Start() {
	if !isAddrValid(s.Host, s.Port) {
		return
	}
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.Host, s.Port))
	if err != nil {
		log.Error("start server failed", "err", err.Error(), "host", s.Host, "port", s.Port)
		return
	}
	encoding.RegisterCodec(rpc.NewEncryptMessageCodec(s.cfg.Cluster.Token))
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(s.unaryServerInterceptor()))
	proto.RegisterEndNodeAccessServer(grpcServer, s)
	go s.heartBeatAndRegisterToNodeOrCenterNode()
	go s.filter()
	log.Info("server listening", "addr", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		log.Error("start server failed", "err", err.Error(), "host", s.Host, "port", s.Port)
	}
}


