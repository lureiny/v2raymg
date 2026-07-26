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
	// defaultHeartbeatInterval is used when end_node.cluster.heartbeat_interval_sec
	// is unset. See EndNodeServer.heartbeatInterval.
	defaultHeartbeatInterval = 10 * time.Second
	clearInvalidNodeInterval = 20 * time.Second
	heartbeatMaxDriftUs      = 30 * 1000 * 1000 // 30 seconds in microseconds

	// Node-directory reconcile damping. A healthy disagreement clears in one
	// round, so once a peer has disagreed for this many consecutive rounds the
	// gap is structural (something one side will never merge) and we stop paying
	// for it every tick, retrying only every nodesReconcileBackoffRounds rounds
	// — at the 10s heartbeat interval, roughly once a minute.
	nodesReconcileStreakLimit   = 3
	nodesReconcileBackoffRounds = 6
)

func GetEndNodeServer() *EndNodeServer {
	return endNodeServer
}

// heartbeatInterval returns the configured cluster heartbeat period, falling back
// to defaultHeartbeatInterval when unset. appconfig.Validate bounds the configured
// value to 1..30s; the fallback here also covers servers built directly in tests,
// which leave cfg zero-valued.
func (s *EndNodeServer) heartbeatInterval() time.Duration {
	if sec := s.cfg.Cluster.HeartbeatIntervalSec; sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return defaultHeartbeatInterval
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
	"ResetUserTraffic":     &proto.UserOpRsp{},
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

// newEmptyRsp returns a fresh, per-call response instance for the given method.
// methodRspMap holds shared prototype pointers; returning them directly (as the
// old code did) let concurrent requests share one struct — a data race under the
// codec plus cross-request field bleed-through. reflect.New mints a private *T
// each call so callers never share state (finding #3).
func newEmptyRsp(fullMethod string) (interface{}, error) {
	proto := methodRspMap[fullMethod[methodPrefixLen:]]
	if proto == nil {
		return nil, nil
	}
	return reflect.New(reflect.TypeOf(proto).Elem()).Interface(), nil
}

func (s *EndNodeServer) authRemoteNode(req interface{}, fullMethod string) (bool, interface{}, *proto.Node) {
	reqValue := reflect.ValueOf(req)
	nodeAuthInfo := reqValue.Elem().FieldByName("NodeAuthInfo").Elem().Interface().(proto.NodeAuthInfo)
	if fullMethod[methodPrefixLen:] == "RegisterNode" {
		return true, nil, nodeAuthInfo.Node
	}
	node := &cluster.Node{Node: nodeAuthInfo.Node}
	node.SetInToken(nodeAuthInfo.Token)
	if err := s.clusterState.AuthRemoteNode(&node); err != nil && localNode.Token != node.GetInToken() {
		errMsg := fmt.Sprintf("auth err > %v", err)
		log.Error("auth remote node failed",
			"err", errMsg,
			"src_host", node.Host, "src_port", node.Port,
			"src_name", node.Name, "api", fullMethod[methodPrefixLen:],
		)
		// Build a private response instance (not the shared prototype) before
		// reflect-writing Code/Msg, so concurrent auth failures don't clobber
		// each other's error message (finding #3).
		rspValue := reflect.New(reflect.TypeOf(methodRspMap[fullMethod[methodPrefixLen:]]).Elem())
		rspValue.Elem().FieldByName("Code").SetInt(400)
		rspValue.Elem().FieldByName("Msg").SetString(errMsg)
		return false, rspValue.Interface(), nodeAuthInfo.Node
	}
	return true, nil, nodeAuthInfo.Node
}

// unaryServerInterceptor returns a gRPC unary interceptor bound to this server instance.
func (s *EndNodeServer) unaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, hander grpc.UnaryHandler) (interface{}, error) {
		// Anti-replay: reject stale/duplicate/cross-node frames before any
		// handler runs. Runs for every method (incl. RegisterNode).
		if err := checkReplay(req, s.Name); err != nil {
			log.Error("rpc replay check failed", "api", info.FullMethod[methodPrefixLen:], "err", err)
			return nil, fmt.Errorf("replay check failed: %w", err)
		}
		// Method binding: the payload must declare the method it was built for,
		// so a captured ciphertext can't be redirected to a sibling method that
		// shares its request type (finding #2). Runs for every method.
		if err := rpc.VerifyDestMethod(req, info.FullMethod); err != nil {
			log.Error("rpc method binding failed", "api", info.FullMethod[methodPrefixLen:], "err", err)
			return nil, fmt.Errorf("method binding failed: %w", err)
		}
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

// Listen binds the RPC listener synchronously so a genuine bind failure
// (port in use, permission) surfaces to the caller for fail-fast handling
// instead of being swallowed inside a goroutine. When the address is simply
// not configured (isAddrValid false) it returns (nil, nil) — a disabled RPC
// plane, which is a legitimate single-machine HTTP-only deployment — so the
// caller skips Serve rather than exiting.
func (s *EndNodeServer) Listen() (net.Listener, error) {
	if !isAddrValid(s.Host, s.Port) {
		log.Info("rpc server disabled: address not configured", "host", s.Host, "port", s.Port)
		return nil, nil
	}
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.Host, s.Port))
	if err != nil {
		return nil, fmt.Errorf("rpc listen %s:%d: %w", s.Host, s.Port, err)
	}
	return lis, nil
}

// Serve registers gRPC services on an already-bound listener and blocks.
func (s *EndNodeServer) Serve(lis net.Listener) {
	encoding.RegisterCodec(rpc.NewEncryptMessageCodec(s.cfg.Cluster.Token))
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(s.unaryServerInterceptor()))
	proto.RegisterEndNodeAccessServer(grpcServer, s)
	go s.heartBeatAndRegisterToNodeOrCenterNode()
	go s.filter()
	log.Info("server listening", "addr", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		log.Error("rpc server exited", "err", err.Error(), "host", s.Host, "port", s.Port)
	}
}


