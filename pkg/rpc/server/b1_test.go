package server

import (
	"context"
	"testing"
	"time"

	commonrpc "github.com/lureiny/v2raymg/pkg/common/rpc"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"google.golang.org/grpc"
)

func authReq(nonce, dest, cluster, token string) *proto.HeartBeatReq {
	return &proto.HeartBeatReq{
		NodeAuthInfo: &proto.NodeAuthInfo{
			Token:       token,
			Node:        &proto.Node{Name: "n1", ClusterName: cluster},
			TimestampUs: time.Now().UnixMicro(),
			Nonce:       []byte(nonce),
			DestNode:    dest,
		},
	}
}

func TestCheckReplay_ValidAndDuplicate(t *testing.T) {
	if err := checkReplay(authReq("b1-nonce-valid", "self", "c1", "tok"), "self"); err != nil {
		t.Fatalf("valid request should pass: %v", err)
	}
	// Same nonce again = replay.
	if err := checkReplay(authReq("b1-nonce-valid", "self", "c1", "tok"), "self"); err == nil {
		t.Fatal("duplicate nonce must be rejected as a replay")
	}
}

func TestCheckReplay_Rejections(t *testing.T) {
	// missing timestamp
	r := authReq("b1-nonce-ts", "self", "c1", "tok")
	r.NodeAuthInfo.TimestampUs = 0
	if err := checkReplay(r, "self"); err == nil {
		t.Error("missing timestamp must be rejected")
	}
	// stale timestamp (> 30s)
	r = authReq("b1-nonce-stale", "self", "c1", "tok")
	r.NodeAuthInfo.TimestampUs = time.Now().Add(-60 * time.Second).UnixMicro()
	if err := checkReplay(r, "self"); err == nil {
		t.Error("stale timestamp must be rejected")
	}
	// missing nonce
	r = authReq("", "self", "c1", "tok")
	if err := checkReplay(r, "self"); err == nil {
		t.Error("missing nonce must be rejected")
	}
	// cross-node replay: dest bound to another node
	if err := checkReplay(authReq("b1-nonce-dest", "other-node", "c1", "tok"), "self"); err == nil {
		t.Error("a request destined for another node must be rejected")
	}
}

func TestCenterInterceptor_TokenAuth(t *testing.T) {
	const good = "strong-cluster-token-01"
	s := &CenterNodeServer{Name: "center", clusterTokens: map[string]string{"c1": good}}
	itc := s.centerInterceptor()
	handled := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		handled = true
		return &proto.HeartBeatRsp{}, nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: "/proto.CenterNodeAccess/HeartBeat"}
	// mk builds a request and method-stamps it exactly as the client interceptor
	// would, so it passes method binding and reaches the token-auth logic here.
	mk := func(nonce, dest, cluster, token string) *proto.HeartBeatReq {
		r := authReq(nonce, dest, cluster, token)
		commonrpc.StampDestMethod(r, info.FullMethod)
		return r
	}

	// wrong token -> rejected, handler not reached
	if _, err := itc(context.Background(), mk("b1-c-1", "", "c1", "wrong-token"), info, handler); err == nil {
		t.Error("wrong token must be rejected")
	}
	if handled {
		t.Fatal("handler must not run on auth failure")
	}
	// unknown cluster -> rejected
	if _, err := itc(context.Background(), mk("b1-c-2", "", "unknown", good), info, handler); err == nil {
		t.Error("unknown cluster must be rejected")
	}
	// valid -> handler runs
	if _, err := itc(context.Background(), mk("b1-c-3", "", "c1", good), info, handler); err != nil {
		t.Fatalf("valid request must pass: %v", err)
	}
	if !handled {
		t.Fatal("handler must run for a valid, authorized request")
	}
}

func TestEncryptMessageCodec_RoundTripAndTypeBinding(t *testing.T) {
	codec := commonrpc.NewEncryptMessageCodec("a-strong-cluster-token")
	data, err := codec.Marshal(&proto.HeartBeatReq{TimestampUs: 12345})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := &proto.HeartBeatReq{}
	if err := codec.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.GetTimestampUs() != 12345 {
		t.Fatalf("round-trip mismatch: got %d", out.GetTimestampUs())
	}
	// AAD binds the proto message type: decoding into a different type must fail.
	if err := codec.Unmarshal(data, &proto.GetUsersRsp{}); err == nil {
		t.Error("decoding into a different message type must fail (AAD type binding)")
	}
	// A different token derives a different key -> decrypt fails.
	if err := commonrpc.NewEncryptMessageCodec("different-token").Unmarshal(data, &proto.HeartBeatReq{}); err == nil {
		t.Error("decrypt with a codec built from a different token must fail")
	}
}
