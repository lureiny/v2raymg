package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	commonrpc "github.com/lureiny/v2raymg/pkg/common/rpc"
	"github.com/lureiny/v2raymg/pkg/proxy/core/container"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
)

type chainTestRPCServer struct {
	proto.UnimplementedEndNodeAccessServer

	mu                 sync.Mutex
	fastAddReqs        []*proto.FastAddInboundReq
	setTagsReqs        []*proto.SetInboundAccessTagsReq
	addUserReqs        []*proto.UserOpReq
	updateUserReqs     []*proto.UserOpReq
	users              []*proto.User
	inbounds           []*proto.InboundInfo
	containers         []string
	containerInfos     []*proto.ContainerInfo
	setTagsErrMsg      string
	updateUsersErrCode int32
	updateUsersErrMsg  string
}

func (s *chainTestRPCServer) FastAddInbound(_ context.Context, req *proto.FastAddInboundReq) (*proto.FastAddInboundRsp, error) {
	s.mu.Lock()
	s.fastAddReqs = append(s.fastAddReqs, req)
	s.mu.Unlock()
	return &proto.FastAddInboundRsp{Code: 0}, nil
}

func (s *chainTestRPCServer) SetInboundAccessTags(_ context.Context, req *proto.SetInboundAccessTagsReq) (*proto.InboundOpRsp, error) {
	s.mu.Lock()
	s.setTagsReqs = append(s.setTagsReqs, req)
	errMsg := s.setTagsErrMsg
	s.mu.Unlock()
	if errMsg != "" {
		return &proto.InboundOpRsp{Code: 601, Msg: errMsg}, nil
	}
	return &proto.InboundOpRsp{Code: 0}, nil
}

func (s *chainTestRPCServer) ListInbound(_ context.Context, _ *proto.ListInboundReq) (*proto.ListInboundRsp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &proto.ListInboundRsp{Code: 0, Inbounds: s.inbounds}, nil
}

func (s *chainTestRPCServer) GetContainers(_ context.Context, _ *proto.GetContainersReq) (*proto.GetContainersRsp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &proto.GetContainersRsp{
		Code:           0,
		Containers:     s.containers,
		ContainerInfos: s.containerInfos,
	}, nil
}

func (s *chainTestRPCServer) GetUsers(_ context.Context, _ *proto.GetUsersReq) (*proto.GetUsersRsp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &proto.GetUsersRsp{Code: 0, Users: s.users}, nil
}

func (s *chainTestRPCServer) AddUsers(_ context.Context, req *proto.UserOpReq) (*proto.UserOpRsp, error) {
	s.mu.Lock()
	s.addUserReqs = append(s.addUserReqs, req)
	s.mu.Unlock()
	return &proto.UserOpRsp{Code: 0}, nil
}

func (s *chainTestRPCServer) UpdateUsers(_ context.Context, req *proto.UserOpReq) (*proto.UserOpRsp, error) {
	s.mu.Lock()
	s.updateUserReqs = append(s.updateUserReqs, req)
	errCode := s.updateUsersErrCode
	errMsg := s.updateUsersErrMsg
	s.mu.Unlock()
	if errCode != 0 {
		return &proto.UserOpRsp{Code: errCode, Msg: errMsg}, nil
	}
	return &proto.UserOpRsp{Code: 0}, nil
}

func startChainTestNode(t *testing.T, name, nodeID, token string, rpcSrv *chainTestRPCServer) *cluster.Node {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	gs := grpc.NewServer(grpc.ForceServerCodec(commonrpc.NewEncryptMessageCodec(token)))
	proto.RegisterEndNodeAccessServer(gs, rpcSrv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(func() {
		gs.Stop()
		_ = lis.Close()
	})
	addr := lis.Addr().(*net.TCPAddr)
	now := time.Now().Unix()
	n := &cluster.Node{
		Node: &proto.Node{
			Name:   name,
			Host:   "127.0.0.1",
			Port:   int32(addr.Port),
			NodeId: nodeID,
		},
		CreateTime: now,
	}
	n.SetOutToken("peer-token")
	n.SetReportHeartBeatTime(now)
	t.Cleanup(n.CloseConn)
	return n
}

func setupChainHTTPServer(t *testing.T, clusterToken string, nodes ...*cluster.Node) (*HttpServer, *httptest.Server) {
	t.Helper()
	s := NewHttpServer()
	s.Init(
		HttpServerConfig{
			Token:          "admin-secret",
			Name:           "node-a",
			JWTSecret:      "chain-isolation-http-jwt-secret-32b",
			JWTExpireHours: 1,
			EnableMCP:      true,
			MCPReadOnly:    false,
		},
		&cluster.LocalNode{Node: proto.Node{Name: "node-a", NodeId: "id-a", Host: "127.0.0.1", Port: 19001}},
		&testClusterNodesList{nodes: nodes, token: clusterToken},
		nil,
		nil,
		false,
	)
	RegisterMCP(s)
	ts := httptest.NewServer(s.RestfulServer)
	t.Cleanup(ts.Close)
	return s, ts
}

func TestChainHTTPAndMCP_InboundsContainersUsers(t *testing.T) {
	const token = "chain-http-cluster-token"
	rpcA := &chainTestRPCServer{
		inbounds: []*proto.InboundInfo{
			{
				Container:     "mihomo",
				Name:          "hy2-chain",
				InboundType:   "chain",
				AccessTags:    []string{"hk", "vip"},
				Remark:        "HK Exit",
				ChainProtocol: "socks5",
				ChainTarget:   "203.0.113.10:1080",
			},
		},
		containers: []string{"xray", "mihomo", "snell"},
		containerInfos: []*proto.ContainerInfo{
			{Type: "xray", SupportsChain: true},
			{Type: "mihomo", SupportsChain: true},
			{Type: "snell", SupportsChain: false},
		},
		users: []*proto.User{
			{Name: "alice", Role: "normal", Group: "vip", InboundTags: []string{"hk", "vip"}, AuthToken: "tok-a"},
		},
	}
	rpcB := &chainTestRPCServer{}
	nodeA := startChainTestNode(t, "node-a", "id-a", token, rpcA)
	nodeB := startChainTestNode(t, "node-b", "id-b", token, rpcB)
	s, ts := setupChainHTTPServer(t, token, nodeA, nodeB)
	adminHdr := map[string]string{"X-Token": s.token}

	t.Run("POST /api/inbound/fast validates chain_uri consistency and forwards fields", func(t *testing.T) {
		badCases := []string{
			`{"inbound_type":"direct","chain_uri":"socks5://1.2.3.4:1080"}`,
			`{"inbound_type":"chain"}`,
			`{"inbound_type":"invalid"}`,
			`{"access_tags":["bad,tag"]}`,
		}
		for _, body := range badCases {
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/inbound/fast", strings.NewReader(body))
			req.Header.Set("X-Token", s.token)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("POST /api/inbound/fast %s: %v", body, err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d", body, resp.StatusCode)
			}
		}

		okBody := `{"container":"mihomo","protocol":"vless","inbound_type":"chain","chain_uri":"socks5://u:p@1.2.3.4:1080#my-exit","access_tags":["vip","hk","vip"],"remark":"Exit"}`
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/inbound/fast", strings.NewReader(okBody))
		req.Header.Set("X-Token", s.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /api/inbound/fast ok: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		rpcA.mu.Lock()
		last := rpcA.fastAddReqs[len(rpcA.fastAddReqs)-1]
		rpcA.mu.Unlock()
		if last.GetInboundType() != "chain" || last.GetChainUri() != "socks5://u:p@1.2.3.4:1080#my-exit" || !reflect.DeepEqual(last.GetAccessTags(), []string{"hk", "vip"}) || last.GetRemark() != "Exit" {
			t.Fatalf("unexpected forwarded FastAddInboundReq: %+v", last)
		}
	})

	t.Run("GET /api/inbounds returns chain summary without raw chain_uri", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/inbounds?target=node-a", nil)
		req.Header.Set("X-Token", s.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /api/inbounds: %v", err)
		}
		defer resp.Body.Close()
		var got map[string][]map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode /api/inbounds: %v", err)
		}
		item := got["node-a"][0]
		if item["inbound_type"] != "chain" || item["chain_protocol"] != "socks5" || item["chain_target"] != "203.0.113.10:1080" || item["remark"] != "HK Exit" {
			t.Fatalf("unexpected inbound item: %+v", item)
		}
		if _, leaked := item["chain_uri"]; leaked {
			t.Fatalf("chain_uri must never be exposed in GET /api/inbounds: %+v", item)
		}
	})

	t.Run("PUT /api/inbounds/tags and MCP set_inbound_tags", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/inbounds/tags", strings.NewReader(`{"container":"mihomo","name":"hy2-chain","access_tags":[" vip ","hk"]}`))
		req.Header.Set("X-Token", s.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PUT /api/inbounds/tags: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		ctx := context.Background()
		adminSess := connectMCPClient(t, ts.URL, adminHdr)
		res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_inbound_tags",
			Arguments: map[string]any{"container": "mihomo", "name": "hy2-chain", "access_tags": []string{}},
		})
		if err != nil || res.IsError {
			t.Fatalf("MCP set_inbound_tags failed: err=%v text=%s", err, extractText(t, res))
		}
		rpcA.mu.Lock()
		if len(rpcA.setTagsReqs) != 2 || !reflect.DeepEqual(rpcA.setTagsReqs[0].GetAccessTags(), []string{"hk", "vip"}) || len(rpcA.setTagsReqs[1].GetAccessTags()) != 0 {
			rpcA.mu.Unlock()
			t.Fatalf("unexpected setTagsReqs: %+v", rpcA.setTagsReqs)
		}
		rpcA.mu.Unlock()

		// Normal JWT user forbidden on HTTP and MCP.
		userJWT := makeJWT(t, s, "alice", "normal")
		req403, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/inbounds/tags", strings.NewReader(`{"container":"mihomo","name":"hy2-chain","access_tags":["hk"]}`))
		req403.Header.Set("Authorization", "Bearer "+userJWT)
		req403.Header.Set("Content-Type", "application/json")
		resp403, err := http.DefaultClient.Do(req403)
		if err != nil {
			t.Fatalf("PUT /api/inbounds/tags normal user: %v", err)
		}
		resp403.Body.Close()
		if resp403.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 for normal user, got %d", resp403.StatusCode)
		}
	})

	t.Run("GET /api/containers exposes details alongside nodes", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/containers?target=node-a", nil)
		req.Header.Set("X-Token", s.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /api/containers: %v", err)
		}
		defer resp.Body.Close()
		var parsed ContainersResp
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			t.Fatalf("decode /api/containers: %v", err)
		}
		if !reflect.DeepEqual(parsed.Nodes["node-a"], []string{"xray", "mihomo", "snell"}) {
			t.Fatalf("unexpected nodes: %+v", parsed.Nodes)
		}
		if len(parsed.Details["node-a"]) != 3 || !parsed.Details["node-a"][1].SupportsChain || parsed.Details["node-a"][2].SupportsChain {
			t.Fatalf("unexpected details: %+v", parsed.Details)
		}
	})

	t.Run("User CRUD + CopyUserBetweenNodes handles inbound_tags", func(t *testing.T) {
		// POST /api/user with inbound_tags
		reqAdd, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/user", strings.NewReader(`{"user":"bob","pwd":"pw","inbound_tags":[" vip ","hk"]}`))
		reqAdd.Header.Set("X-Token", s.token)
		reqAdd.Header.Set("Content-Type", "application/json")
		respAdd, err := http.DefaultClient.Do(reqAdd)
		if err != nil || respAdd.StatusCode != http.StatusOK {
			t.Fatalf("POST /api/user failed: %v %v", err, respAdd)
		}
		respAdd.Body.Close()

		// PUT /api/user omitted inbound_tags -> UpdateInboundTags=false
		reqUpdKeep, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/user", strings.NewReader(`{"user":"bob","role":"normal"}`))
		reqUpdKeep.Header.Set("X-Token", s.token)
		reqUpdKeep.Header.Set("Content-Type", "application/json")
		respKeep, _ := http.DefaultClient.Do(reqUpdKeep)
		respKeep.Body.Close()

		// PUT /api/user explicit empty [] -> UpdateInboundTags=true, InboundTags=nil
		reqUpdClear, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/user", strings.NewReader(`{"user":"bob","inbound_tags":[]}`))
		reqUpdClear.Header.Set("X-Token", s.token)
		reqUpdClear.Header.Set("Content-Type", "application/json")
		respClear, _ := http.DefaultClient.Do(reqUpdClear)
		respClear.Body.Close()

		// POST /api/copyUserBetweenNodes preserves inbound_tags
		reqCopy, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/copyUserBetweenNodes", strings.NewReader(`{"src_node":"node-a","dst_node":"node-b"}`))
		reqCopy.Header.Set("X-Token", s.token)
		reqCopy.Header.Set("Content-Type", "application/json")
		respCopy, err := http.DefaultClient.Do(reqCopy)
		if err != nil || respCopy.StatusCode != http.StatusOK {
			t.Fatalf("copyUserBetweenNodes failed: %v", err)
		}
		respCopy.Body.Close()

		rpcA.mu.Lock()
		addedBob := rpcA.addUserReqs[0].GetUsers()[0]
		keptBob := rpcA.updateUserReqs[0].GetUsers()[0]
		clearedBob := rpcA.updateUserReqs[1].GetUsers()[0]
		rpcA.mu.Unlock()
		if !reflect.DeepEqual(addedBob.GetInboundTags(), []string{"hk", "vip"}) {
			t.Fatalf("AddUsers inbound_tags=%v, want [hk vip]", addedBob.GetInboundTags())
		}
		if keptBob.GetUpdateInboundTags() {
			t.Fatal("omitted inbound_tags must leave UpdateInboundTags=false")
		}
		if !clearedBob.GetUpdateInboundTags() || len(clearedBob.GetInboundTags()) != 0 {
			t.Fatalf("explicit [] inbound_tags must set UpdateInboundTags=true & empty tags, got %+v", clearedBob)
		}

		rpcB.mu.Lock()
		copiedAlice := rpcB.addUserReqs[0].GetUsers()[0]
		rpcB.mu.Unlock()
		if !reflect.DeepEqual(copiedAlice.GetInboundTags(), []string{"hk", "vip"}) {
			t.Fatalf("copied user lost inbound_tags: %+v", copiedAlice.GetInboundTags())
		}

		// Admin GET /api/user returns inbound_tags
		reqList, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/user?target=node-a", nil)
		reqList.Header.Set("X-Token", s.token)
		respList, _ := http.DefaultClient.Do(reqList)
		defer respList.Body.Close()
		var usersMap map[string][]map[string]any
		if err := json.NewDecoder(respList.Body).Decode(&usersMap); err != nil {
			t.Fatalf("decode /api/user: %v", err)
		}
		if !reflect.DeepEqual(usersMap["node-a"][0]["inbound_tags"], []any{"hk", "vip"}) {
			t.Fatalf("expected inbound_tags in GET /api/user, got %+v", usersMap["node-a"][0])
		}
	})
}

type chainHysteriaStubContainer struct {
	container.Container
	meta    contracts.InboundMeta
	hasMeta bool
}

func (c *chainHysteriaStubContainer) Type() container.ContainerType {
	return contracts.ContainerHysteria
}
func (c *chainHysteriaStubContainer) ListInboundConfigs() []inbound.Inbound { return nil }
func (c *chainHysteriaStubContainer) GetInboundMeta(tag string) (contracts.InboundMeta, bool) {
	if tag != "hysteria-default" || !c.hasMeta {
		return contracts.InboundMeta{}, false
	}
	return c.meta, true
}

type memNodeGroupsStub struct {
	groups []string
}

func (m *memNodeGroupsStub) List() ([]string, error) { return append([]string(nil), m.groups...), nil }
func (m *memNodeGroupsStub) Set(g []string) error    { m.groups = append([]string(nil), g...); return nil }
func (m *memNodeGroupsStub) Close() error            { return nil }

func TestAuthHysteria2_AccessIsolationGating(t *testing.T) {
	um := usermanager.NewUserManager(nil, "node-a")
	um.EnableClusterSync("default", &memNodeGroupsStub{groups: []string{"default"}})

	_ = um.AddUser(usermanager.AddUserRequest{Username: "allowed", Password: "pw", TargetGroup: "default", InboundTags: []string{"hy-vip"}})
	_ = um.AddUser(usermanager.AddUserRequest{Username: "wrongtag", Password: "pw", TargetGroup: "default", InboundTags: []string{"other"}})
	_ = um.AddUser(usermanager.AddUserRequest{Username: "wronggroup", Password: "pw", TargetGroup: "isolated", InboundTags: []string{"hy-vip"}})

	uAllowed, _ := um.GetUser("allowed")
	uWrongTag, _ := um.GetUser("wrongtag")
	uWrongGroup, _ := um.GetUser("wronggroup")

	cm := container.NewContainerMgr(nil, container.BuildOptions{})
	cm.SetForTest(contracts.ContainerHysteria, &chainHysteriaStubContainer{
		hasMeta: true,
		meta: contracts.InboundMeta{
			InboundType: contracts.InboundTypeDirect,
			AccessTags:  []string{"hy-vip"},
		},
	})

	s := newTestHttpServer(um)
	s.SetContainerMgr(cm)
	h := &AuthHysteria2{}
	s.RegisterHandler(h, http.MethodPost)

	postAuth := func(token string) int {
		w := httptest.NewRecorder()
		body, _ := json.Marshal(AuthHysteria2Data{Addr: "1.2.3.4:1234", Auth: token})
		req := httptest.NewRequest(http.MethodPost, "/api/authHysteria2", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		s.RestfulServer.ServeHTTP(w, req)
		return w.Code
	}

	if code := postAuth(uAllowed.AuthToken); code != http.StatusOK {
		t.Fatalf("expected visible+tag-matched user allowed (200), got %d", code)
	}
	if code := postAuth(uWrongTag.AuthToken); code != http.StatusForbidden {
		t.Fatalf("expected tag-mismatched user rejected (403), got %d", code)
	}
	if code := postAuth(uWrongGroup.AuthToken); code != http.StatusForbidden {
		t.Fatalf("expected Layer-1 hidden user rejected (403), got %d", code)
	}

	// Disabled hysteria-default (registered container with hasMeta=false and empty ListInboundConfigs)
	// must Fail-Closed with 403 even for an otherwise-valid visible user.
	cmDisabled := container.NewContainerMgr(nil, container.BuildOptions{})
	cmDisabled.SetForTest(contracts.ContainerHysteria, &chainHysteriaStubContainer{
		hasMeta: false,
	})
	s.SetContainerMgr(cmDisabled)
	if code := postAuth(uAllowed.AuthToken); code != http.StatusForbidden {
		t.Fatalf("expected disabled hysteria-default to Fail-Closed (403), got %d", code)
	}

	// Empty auth token must always be rejected (403).
	s.SetContainerMgr(cm)
	if code := postAuth(""); code != http.StatusForbidden {
		t.Fatalf("expected empty auth token rejected (403), got %d", code)
	}

	// Restricted hysteria-default (non-empty AccessTags) with a UserLister that
	// does NOT implement InboundAccessChecker must Fail-Closed (403).
	basicLister := &plainUserLister{users: map[string]string{"allowed": uAllowed.AuthToken}}
	sNoChecker := newTestHttpServer(basicLister)
	sNoChecker.SetContainerMgr(cm)
	sNoChecker.RegisterHandler(&AuthHysteria2{}, http.MethodPost)
	w := httptest.NewRecorder()
	body, _ := json.Marshal(AuthHysteria2Data{Addr: "1.2.3.4:1234", Auth: uAllowed.AuthToken})
	req := httptest.NewRequest(http.MethodPost, "/api/authHysteria2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	sNoChecker.RestfulServer.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected restricted hysteria inbound without InboundAccessChecker to Fail-Closed (403), got %d", w.Code)
	}
}

type plainUserLister struct {
	users map[string]string
}

func (p *plainUserLister) ListUsersWithPasswd() map[string]string { return p.users }
func (p *plainUserLister) GetUser(string) (*contracts.User, error) {
	return nil, errors.New("not found")
}

func TestUserUpdate_HTTPAndMCPPropagateNonZeroRPCCode(t *testing.T) {
	const token = "update-err-cluster-token"
	rpcA := &chainTestRPCServer{
		updateUsersErrCode: 202,
		updateUsersErrMsg:  "user not found: ghost",
	}
	nodeA := startChainTestNode(t, "node-a", "id-a", token, rpcA)
	s, ts := setupChainHTTPServer(t, token, nodeA)

	// HTTP PUT /api/user must return non-200 error status when UpdateUsers returns non-zero Code.
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/user", strings.NewReader(`{"user":"ghost","role":"admin"}`))
	req.Header.Set("X-Token", s.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/user: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("PUT /api/user returned HTTP 200 on non-zero RPC Code")
	}

	// MCP update_user must also surface IsError=true with the error message.
	adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})
	mcpRes, err := adminSess.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "update_user",
		Arguments: map[string]any{"user": "ghost", "role": "admin"},
	})
	if err != nil {
		t.Fatalf("MCP CallTool error: %v", err)
	}
	if !mcpRes.IsError {
		t.Fatalf("expected MCP update_user IsError=true, got %+v", mcpRes)
	}
	if txt := extractText(t, mcpRes); !strings.Contains(txt, "user not found: ghost") {
		t.Fatalf("expected MCP error text to include RPC error message, got %q", txt)
	}
}
