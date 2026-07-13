package server

import (
	"context"
	"net"
	"testing"
	"time"

	commonrpc "github.com/lureiny/v2raymg/pkg/common/rpc"
	rpcClient "github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
)

// TestCenterChannel_EncryptedAndAuthenticated is the end-to-end regression for
// finding #31: the end->center channel is now wrapped in an AES envelope keyed
// by a dedicated centerToken (separate from the cluster token), while the inner
// per-cluster token auth is unchanged. It drives a real localhost gRPC
// connection through the actual centerInterceptor and asserts:
//   1. correct centerToken + correct cluster token  -> accepted;
//   2. wrong centerToken                            -> server can't decrypt -> rejected;
//   3. correct centerToken but wrong cluster token  -> inner auth rejects (isolation kept).
func TestCenterChannel_EncryptedAndAuthenticated(t *testing.T) {
	const centerTok = "center-envelope-secret-token-01"
	const clusterTok = "cluster-shared-token-abcdef01"

	// Register the center codec exactly as CenterNodeServer.Start does. No sibling
	// test in this package performs encoded RPC, so the global registry is safe.
	encoding.RegisterCodec(commonrpc.NewEncryptMessageCodec(centerTok))

	s := &CenterNodeServer{
		Name:          "center",
		clusterTokens: map[string]string{"c1": clusterTok},
		centerToken:   centerTok,
	}
	s.clusters.Init()

	srv := grpc.NewServer(grpc.UnaryInterceptor(s.centerInterceptor()))
	proto.RegisterCenterNodeAccessServer(srv, s)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	dial := func() *grpc.ClientConn {
		conn, err := grpc.Dial(lis.Addr().String(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithChainUnaryInterceptor(commonrpc.StampDestMethodClientInterceptor),
		)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		return conn
	}
	conn := dial()
	defer conn.Close()
	cli := proto.NewCenterNodeAccessClient(conn)

	node := func(name string, port int32) *proto.Node {
		return &proto.Node{Name: name, Host: "127.0.0.1", Port: port, ClusterName: "c1"}
	}
	call := func(codecTok, clusterToken string, n *proto.Node) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req := &proto.HeartBeatReq{NodeAuthInfo: rpcClient.NewNodeAuthInfo(clusterToken, n, "")}
		_, err := cli.HeartBeat(ctx, req, grpc.ForceCodec(commonrpc.NewEncryptMessageCodec(codecTok)))
		return err
	}

	// 1. correct envelope + correct inner token -> accepted.
	if err := call(centerTok, clusterTok, node("end-1", 5000)); err != nil {
		t.Fatalf("valid encrypted heartbeat must succeed: %v", err)
	}

	// 2. wrong centerToken -> the server cannot decrypt the envelope -> rejected.
	if err := call("wrong-center-token-xxxxxxx", clusterTok, node("end-2", 5001)); err == nil {
		t.Error("heartbeat with the wrong centerToken must fail (envelope not decryptable)")
	}

	// 3. correct envelope but wrong inner cluster token -> inner auth rejects,
	//    proving per-cluster isolation survives the envelope.
	if err := call(centerTok, "wrong-cluster-token", node("end-3", 5002)); err == nil {
		t.Error("correct envelope but wrong cluster token must be rejected by inner auth")
	}
}
