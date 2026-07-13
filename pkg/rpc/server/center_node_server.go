package server

import (
	context "context"
	"crypto/subtle"
	"fmt"
	"net"
	"time"

	"github.com/lureiny/v2raymg/pkg/common"
	rpc "github.com/lureiny/v2raymg/pkg/common/rpc"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"

	c "github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

type NodeMap map[string]*proto.Node

type CenterNodeServer struct {
	proto.UnimplementedCenterNodeAccessServer
	clusters c.CenterClusterManager

	// clusterTokens maps cluster name -> authorized shared token, used to
	// authenticate heartbeats (a center may serve multiple clusters). Nil/empty
	// means no cluster is authorized.
	clusterTokens map[string]string

	// centerToken keys the AES envelope over the end->center channel. Empty =
	// plaintext center channel (legacy). Distinct from clusterTokens, which
	// still authenticate cluster membership inside the envelope.
	centerToken string

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
		Node:       heartBeatReq.GetNodeAuthInfo().GetNode(),
		CreateTime: time.Now().Unix(),
	}
	node.SetRecvHeartBeatTime(time.Now().Unix())
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
			n.SetRecvHeartBeatTime(time.Now().Unix())
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
	s.clusterTokens = cfg.ClusterTokens
	s.centerToken = cfg.CenterToken
	s.clusters.Init()
}

// centerInterceptor authenticates every incoming request BEFORE the handler.
// The center serves multiple clusters with different tokens, so a single global
// encryption codec can't work; instead the request's NodeAuthInfo.Token must
// match the configured token for its cluster (app-layer auth). This token check
// is LOAD-BEARING (not defense-in-depth): it is the only thing stopping an
// unauthenticated attacker from poisoning/enumerating the node directory. It
// also runs the shared anti-replay check.
func (s *CenterNodeServer) centerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if err := checkReplay(req, s.Name); err != nil {
			return nil, fmt.Errorf("replay check failed: %w", err)
		}
		// Bind the payload to its method (finding #2); harmless for the center's
		// two distinct-typed methods but keeps the check uniform across channels.
		if err := rpc.VerifyDestMethod(req, info.FullMethod); err != nil {
			return nil, fmt.Errorf("method binding failed: %w", err)
		}
		carrier, ok := req.(authInfoCarrier)
		if !ok || carrier.GetNodeAuthInfo() == nil || carrier.GetNodeAuthInfo().GetNode() == nil {
			return nil, fmt.Errorf("missing node auth info")
		}
		ai := carrier.GetNodeAuthInfo()
		clusterName := ai.GetNode().GetClusterName()
		want, known := s.clusterTokens[clusterName]
		if !known || subtle.ConstantTimeCompare([]byte(ai.GetToken()), []byte(want)) != 1 {
			log.Error("center: unauthorized heartbeat", "cluster", clusterName,
				"src_name", ai.GetNode().GetName())
			return nil, fmt.Errorf("unauthorized: cluster %q not authorized", clusterName)
		}
		return handler(ctx, req)
	}
}

func (s *CenterNodeServer) Start() {
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("center node failed to listen", "addr", addr, "err", err)
		return
	}
	// Register the AES codec so the end->center channel (heartbeat/discovery) is
	// encrypted: end nodes ForceCodec with the same centerToken, and this codec
	// decrypts the request and encrypts the node-directory response. Empty
	// centerToken keeps the channel in plaintext (legacy). A center process runs
	// only the center server, so this global codec never collides with the
	// end-node server's cluster-token codec.
	if s.centerToken != "" {
		encoding.RegisterCodec(rpc.NewEncryptMessageCodec(s.centerToken))
		log.Info("center node channel encryption enabled")
	}
	grpcServer := grpc.NewServer(grpc.UnaryInterceptor(s.centerInterceptor()))
	proto.RegisterCenterNodeAccessServer(grpcServer, s)
	go s.filter()
	log.Info("center node server listening", "addr", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		log.Error("center node failed to serve", "addr", addr, "err", err)
	}
}
