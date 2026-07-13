package client

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"google.golang.org/grpc"
	pb "google.golang.org/protobuf/proto"
)

// TestNewNodeAuthInfo_FieldsAndFreshNonce pins the B1 anti-replay invariant:
// every call must stamp a fresh 16-byte random nonce and a current timestamp,
// and carry the token/node/dest as given. Reusing a nonce across calls would be
// rejected downstream as a replay, so uniqueness is the load-bearing property.
func TestNewNodeAuthInfo_FieldsAndFreshNonce(t *testing.T) {
	node := &proto.Node{Name: "n1", ClusterName: "c1"}
	ai := NewNodeAuthInfo("tok", node, "dest-node")

	if ai.GetToken() != "tok" {
		t.Errorf("token = %q, want tok", ai.GetToken())
	}
	if ai.GetNode().GetName() != "n1" {
		t.Errorf("node name = %q, want n1", ai.GetNode().GetName())
	}
	if ai.GetDestNode() != "dest-node" {
		t.Errorf("dest = %q, want dest-node", ai.GetDestNode())
	}
	if len(ai.GetNonce()) != 16 {
		t.Fatalf("nonce length = %d, want 16", len(ai.GetNonce()))
	}
	if ai.GetTimestampUs() == 0 {
		t.Error("timestamp must be set")
	}
	if drift := time.Since(time.UnixMicro(ai.GetTimestampUs())); drift < 0 || drift > 5*time.Second {
		t.Errorf("timestamp drift %v is implausible", drift)
	}

	// Fresh, distinct nonce on every call.
	const n = 200
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		nonce := NewNodeAuthInfo("tok", node, "dest").GetNonce()
		key := hex.EncodeToString(nonce)
		if _, dup := seen[key]; dup {
			t.Fatalf("duplicate nonce produced on iteration %d: %s", i, key)
		}
		seen[key] = struct{}{}
	}
}

func TestProcessUserOpRsp(t *testing.T) {
	// transport error passes through
	if _, err := processUserOpRsp(nil, context.DeadlineExceeded); err == nil {
		t.Error("transport error must pass through")
	}
	// non-zero code -> error carrying the message
	_, err := processUserOpRsp(&proto.UserOpRsp{Code: 300, Msg: "boom"}, nil)
	if err == nil || err.Error() != "boom" {
		t.Errorf("code!=0 should return error 'boom', got %v", err)
	}
	// success
	if _, err := processUserOpRsp(&proto.UserOpRsp{Code: 0}, nil); err != nil {
		t.Errorf("code==0 should succeed, got %v", err)
	}
}

func TestGetReqAndCallbackFunc(t *testing.T) {
	if getReqAndCallbakcFunc(AddUsersReqType) == nil {
		t.Error("AddUsersReqType must be registered by init()")
	}
	if getReqAndCallbakcFunc(ReqToEndNodeType(9999)) != nil {
		t.Error("unknown req type must return nil")
	}
}

// mockEndNodeAccessClient embeds the interface so only the methods a test
// actually invokes need implementing; any other call would nil-panic, which is
// the desired signal that a test touched an unexpected RPC.
type mockEndNodeAccessClient struct {
	proto.EndNodeAccessClient
	gotReq   *proto.UserOpReq
	addUsers func(*proto.UserOpReq) (*proto.UserOpRsp, error)
}

func (m *mockEndNodeAccessClient) AddUsers(ctx context.Context, in *proto.UserOpReq, opts ...grpc.CallOption) (*proto.UserOpRsp, error) {
	m.gotReq = in
	return m.addUsers(in)
}

func TestReqAddUsers_StampsAuthAndMapsResponse(t *testing.T) {
	reqData, err := pb.Marshal(&proto.UserOpReq{})
	if err != nil {
		t.Fatalf("marshal req: %v", err)
	}
	auth := NewNodeAuthInfo("tok", &proto.Node{Name: "n1"}, "dst")

	// success: rsp code 0 -> nil error, and the req must carry the auth info.
	m := &mockEndNodeAccessClient{addUsers: func(*proto.UserOpReq) (*proto.UserOpRsp, error) {
		return &proto.UserOpRsp{Code: 0}, nil
	}}
	if _, err := ReqAddUsers(context.Background(), reqData, m, auth, "tok"); err != nil {
		t.Fatalf("success path returned error: %v", err)
	}
	if m.gotReq == nil || m.gotReq.GetNodeAuthInfo() == nil {
		t.Fatal("ReqAddUsers must set NodeAuthInfo on the outgoing request")
	}
	if len(m.gotReq.GetNodeAuthInfo().GetNonce()) != 16 {
		t.Error("forwarded auth info lost its nonce")
	}

	// server-side rejection: code!=0 -> error with message.
	m2 := &mockEndNodeAccessClient{addUsers: func(*proto.UserOpReq) (*proto.UserOpRsp, error) {
		return &proto.UserOpRsp{Code: 300, Msg: "denied"}, nil
	}}
	if _, err := ReqAddUsers(context.Background(), reqData, m2, auth, "tok"); err == nil || err.Error() != "denied" {
		t.Errorf("code!=0 should surface 'denied', got %v", err)
	}

	// transport error propagates.
	m3 := &mockEndNodeAccessClient{addUsers: func(*proto.UserOpReq) (*proto.UserOpRsp, error) {
		return nil, context.Canceled
	}}
	if _, err := ReqAddUsers(context.Background(), reqData, m3, auth, "tok"); err == nil {
		t.Error("transport error must propagate")
	}
}
