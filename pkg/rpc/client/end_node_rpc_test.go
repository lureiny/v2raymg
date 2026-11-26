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
	node := &proto.Node{Name: "n1"}
	ai := NewNodeAuthInfo("tok", node, &proto.Node{Name: "dest-node", NodeId: "dest-id"})

	if ai.GetToken() != "tok" {
		t.Errorf("token = %q, want tok", ai.GetToken())
	}
	if ai.GetNode().GetName() != "n1" {
		t.Errorf("node name = %q, want n1", ai.GetNode().GetName())
	}
	if ai.GetDestNode() != "dest-node" {
		t.Errorf("dest = %q, want dest-node", ai.GetDestNode())
	}
	// The identity binding is the one that still holds when two nodes are
	// misconfigured with the same name, which the identity-keyed directory now
	// permits rather than refusing outright.
	if ai.GetDestNodeId() != "dest-id" {
		t.Errorf("dest node id = %q, want dest-id", ai.GetDestNodeId())
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
		nonce := NewNodeAuthInfo("tok", node, &proto.Node{Name: "dest"}).GetNonce()
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
	auth := NewNodeAuthInfo("tok", &proto.Node{Name: "n1"}, &proto.Node{Name: "dst"})

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

type mockChainEndNodeAccessClient struct {
	proto.EndNodeAccessClient
	gotSetTags *proto.SetInboundAccessTagsReq
	setTagsRsp *proto.InboundOpRsp
	setTagsErr error
	listRsp    *proto.ListInboundRsp
	contRsp    *proto.GetContainersRsp
}

func (m *mockChainEndNodeAccessClient) SetInboundAccessTags(ctx context.Context, in *proto.SetInboundAccessTagsReq, opts ...grpc.CallOption) (*proto.InboundOpRsp, error) {
	m.gotSetTags = in
	return m.setTagsRsp, m.setTagsErr
}

func (m *mockChainEndNodeAccessClient) ListInbound(ctx context.Context, in *proto.ListInboundReq, opts ...grpc.CallOption) (*proto.ListInboundRsp, error) {
	return m.listRsp, nil
}

func (m *mockChainEndNodeAccessClient) GetContainers(ctx context.Context, in *proto.GetContainersReq, opts ...grpc.CallOption) (*proto.GetContainersRsp, error) {
	return m.contRsp, nil
}

func TestSetInboundAccessTagsAndChainClientRPC(t *testing.T) {
	if getReqAndCallbakcFunc(SetInboundAccessTagsReqType) == nil {
		t.Fatal("SetInboundAccessTagsReqType must be registered by init()")
	}
	auth := NewNodeAuthInfo("tok", &proto.Node{Name: "n1"}, &proto.Node{Name: "dst"})

	reqBytes, err := pb.Marshal(&proto.SetInboundAccessTagsReq{
		Container:  "mihomo",
		Name:       "hy2-chain",
		AccessTags: []string{"hk", "vip"},
	})
	if err != nil {
		t.Fatalf("marshal SetInboundAccessTagsReq: %v", err)
	}
	m := &mockChainEndNodeAccessClient{
		setTagsRsp: &proto.InboundOpRsp{Code: 0},
		listRsp: &proto.ListInboundRsp{
			Code: 0,
			Inbounds: []*proto.InboundInfo{
				{
					Container:     "mihomo",
					Name:          "hy2-chain",
					InboundType:   "chain",
					AccessTags:    []string{"hk", "vip"},
					Remark:        "HK Exit",
					ChainProtocol: "socks5",
					ChainTarget:   "1.2.3.4:1080",
				},
			},
		},
		contRsp: &proto.GetContainersRsp{
			Code:       0,
			Containers: []string{"xray", "mihomo", "snell"},
			ContainerInfos: []*proto.ContainerInfo{
				{Type: "xray", SupportsChain: true},
				{Type: "mihomo", SupportsChain: true},
				{Type: "snell", SupportsChain: false},
			},
		},
	}

	if _, err := ReqSetInboundAccessTags(context.Background(), reqBytes, m, auth, "tok"); err != nil {
		t.Fatalf("ReqSetInboundAccessTags failed: %v", err)
	}
	if m.gotSetTags == nil || m.gotSetTags.GetNodeAuthInfo() == nil || m.gotSetTags.GetName() != "hy2-chain" {
		t.Fatalf("unexpected outgoing SetInboundAccessTagsReq: %+v", m.gotSetTags)
	}

	mErr := &mockChainEndNodeAccessClient{setTagsRsp: &proto.InboundOpRsp{Code: 601, Msg: "not supported"}}
	if _, err := ReqSetInboundAccessTags(context.Background(), reqBytes, mErr, auth, "tok"); err == nil || err.Error() != "not supported" {
		t.Fatalf("expected 'not supported', got %v", err)
	}

	listReq, _ := pb.Marshal(&proto.ListInboundReq{})
	listRes, err := ReqListInbound(context.Background(), listReq, m, auth, "tok")
	if err != nil {
		t.Fatalf("ReqListInbound failed: %v", err)
	}
	inbs := listRes.([]*proto.InboundInfo)
	if len(inbs) != 1 || inbs[0].GetInboundType() != "chain" || inbs[0].GetChainProtocol() != "socks5" || inbs[0].GetChainTarget() != "1.2.3.4:1080" || inbs[0].GetRemark() != "HK Exit" {
		t.Fatalf("ReqListInbound dropped chain metadata: %+v", inbs)
	}

	contReq, _ := pb.Marshal(&proto.GetContainersReq{})
	contRes, err := ReqGetContainers(context.Background(), contReq, m, auth, "tok")
	if err != nil {
		t.Fatalf("ReqGetContainers failed: %v", err)
	}
	rsp, ok := contRes.(*proto.GetContainersRsp)
	if !ok || len(rsp.GetContainers()) != 3 || len(rsp.GetContainerInfos()) != 3 || !rsp.GetContainerInfos()[1].GetSupportsChain() {
		t.Fatalf("unexpected ReqGetContainers response: %#v", contRes)
	}
}
