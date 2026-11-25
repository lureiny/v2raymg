package http

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	commonrpc "github.com/lureiny/v2raymg/pkg/common/rpc"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
)

// stubEndNodeRPCServer is a minimal in-process EndNodeAccess gRPC server used
// to exercise the full HTTP/MCP -> Endpoint -> EndNodeClient -> gRPC roundtrip.
type stubEndNodeRPCServer struct {
	proto.UnimplementedEndNodeAccessServer

	mu            sync.Mutex
	lastGetInbTag string
	listInboundFn func(ctx context.Context, req *proto.ListInboundReq) (*proto.ListInboundRsp, error)
	getInboundFn  func(ctx context.Context, req *proto.GetInboundReq) (*proto.GetInboundRsp, error)
	getCertsFn    func(ctx context.Context, req *proto.GetCertsReq) (*proto.GetCertsRsp, error)
}

func (s *stubEndNodeRPCServer) ListInbound(ctx context.Context, req *proto.ListInboundReq) (*proto.ListInboundRsp, error) {
	if s.listInboundFn != nil {
		return s.listInboundFn(ctx, req)
	}
	return &proto.ListInboundRsp{Code: 0}, nil
}

func (s *stubEndNodeRPCServer) GetInbound(ctx context.Context, req *proto.GetInboundReq) (*proto.GetInboundRsp, error) {
	s.mu.Lock()
	s.lastGetInbTag = req.GetTag()
	s.mu.Unlock()
	if s.getInboundFn != nil {
		return s.getInboundFn(ctx, req)
	}
	return &proto.GetInboundRsp{Code: 0}, nil
}

func (s *stubEndNodeRPCServer) GetCerts(ctx context.Context, req *proto.GetCertsReq) (*proto.GetCertsRsp, error) {
	if s.getCertsFn != nil {
		return s.getCertsFn(ctx, req)
	}
	return &proto.GetCertsRsp{Code: 0}, nil
}

func (s *stubEndNodeRPCServer) getLastInboundTag() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastGetInbTag
}

// startTestEndNode launches an encrypted in-process gRPC EndNodeAccessServer
// and returns a registered *cluster.Node pointing to it.
func startTestEndNode(t *testing.T, name, nodeID, clusterToken string, stub *stubEndNodeRPCServer) *cluster.Node {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	grpcSrv := grpc.NewServer(grpc.ForceServerCodec(commonrpc.NewEncryptMessageCodec(clusterToken)))
	proto.RegisterEndNodeAccessServer(grpcSrv, stub)
	go func() {
		_ = grpcSrv.Serve(lis)
	}()
	t.Cleanup(func() {
		grpcSrv.Stop()
		_ = lis.Close()
	})

	addr := lis.Addr().(*net.TCPAddr)
	now := time.Now().Unix()
	node := &cluster.Node{
		Node: &proto.Node{
			Name:   name,
			Host:   "127.0.0.1",
			Port:   int32(addr.Port),
			NodeId: nodeID,
		},
		CreateTime: now,
	}
	node.SetOutToken("peer-out-token")
	node.SetReportHeartBeatTime(now)
	t.Cleanup(node.CloseConn)
	return node
}

type testClusterNodesList struct {
	nodes []*cluster.Node
	token string
}

func (c *testClusterNodesList) GetNodesWithFilter(f cluster.NodeFilter) []*cluster.Node {
	var out []*cluster.Node
	for _, n := range c.nodes {
		if f(n) {
			out = append(out, n)
		}
	}
	return out
}

func (c *testClusterNodesList) GetClusterToken() string { return c.token }

func newInboundsTestServer(t *testing.T, clusterToken string, nodes ...*cluster.Node) *HttpServer {
	t.Helper()
	s := newTestHttpServer(nil)
	s.Name = "node-a"
	s.localNode = &cluster.LocalNode{
		Node: proto.Node{
			Name:   "node-a",
			NodeId: "local-id-a",
			Host:   "127.0.0.1",
			Port:   19000,
		},
	}
	s.clusterNodes = &testClusterNodesList{
		nodes: nodes,
		token: clusterToken,
	}
	return s
}

// ---------------------------------------------------------------------------
// HTTP Backwards Compatibility Tests
// ---------------------------------------------------------------------------

func TestInboundListEndpoint_HTTP(t *testing.T) {
	const clusterToken = "test-cluster-token-123456"

	t.Run("no available node returns 502 via legacy handler and registerEndpoint", func(t *testing.T) {
		s := newInboundsTestServer(t, clusterToken)
		handler := &InboundListHandler{}
		handler.setHttpServer(s)

		r := handlerEngine(http.MethodGet, "/inbounds", handler.handlerFunc)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/inbounds", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d body=%s", w.Code, w.Body.String())
		}
		var body struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal body: %v", err)
		}
		if body.Code != 502 || body.Msg != "no available node" {
			t.Fatalf("unexpected error envelope: %+v", body)
		}
	})

	t.Run("default target=all queries all nodes and returns succList", func(t *testing.T) {
		stubA := &stubEndNodeRPCServer{
			listInboundFn: func(ctx context.Context, req *proto.ListInboundReq) (*proto.ListInboundRsp, error) {
				return &proto.ListInboundRsp{
					Code: 0,
					Inbounds: []*proto.InboundInfo{
						{Container: "xray", Name: "vless-in"},
						{Container: "hysteria", Name: "hy2-in"},
					},
				}, nil
			},
		}
		stubB := &stubEndNodeRPCServer{
			listInboundFn: func(ctx context.Context, req *proto.ListInboundReq) (*proto.ListInboundRsp, error) {
				return &proto.ListInboundRsp{
					Code: 0,
					Inbounds: []*proto.InboundInfo{
						{Container: "snell", Name: "snell-in"},
					},
				}, nil
			},
		}
		nodeA := startTestEndNode(t, "node-a", "id-a", clusterToken, stubA)
		nodeB := startTestEndNode(t, "node-b", "id-b", clusterToken, stubB)
		s := newInboundsTestServer(t, clusterToken, nodeA, nodeB)
		s.registerOn(s.RestfulServer, &HelpHandler{}, http.MethodGet)
		api := s.RestfulServer.Group("/api")
		registerEndpoint(s, api, s.inboundListEndpoint())

		for _, rawURL := range []string{"/api/inbounds", "/api/inbounds?target="} {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, rawURL, nil)
			s.RestfulServer.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("%s: expected 200, got %d body=%s", rawURL, w.Code, w.Body.String())
			}
			var got map[string][]map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("%s: unmarshal response: %v (body=%s)", rawURL, err, w.Body.String())
			}
			if len(got["node-a"]) != 2 || len(got["node-b"]) != 1 {
				t.Fatalf("%s: expected 2 inbounds on node-a and 1 on node-b, got %+v", rawURL, got)
			}
		}

		// Target single node.
		w := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/inbounds?target=node-b", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		var single map[string][]map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &single); err != nil {
			t.Fatalf("unmarshal single: %v", err)
		}
		if len(single) != 1 || len(single["node-b"]) != 1 {
			t.Fatalf("expected only node-b, got %+v", single)
		}

		// Verify /help/inbounds matches legacy help text exactly.
		wHelp := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(wHelp, httptest.NewRequest(http.MethodGet, "/help/inbounds", nil))
		if wHelp.Code != http.StatusOK || wHelp.Body.String() != (&InboundListHandler{}).help() {
			t.Fatalf("unexpected /help/inbounds: code=%d body=%q", wHelp.Code, wHelp.Body.String())
		}
	})

	t.Run("RPC failure on all nodes returns 500", func(t *testing.T) {
		stubErr := &stubEndNodeRPCServer{
			listInboundFn: func(ctx context.Context, req *proto.ListInboundReq) (*proto.ListInboundRsp, error) {
				return &proto.ListInboundRsp{Code: 300, Msg: "container down"}, nil
			},
		}
		nodeA := startTestEndNode(t, "node-a", "id-a", clusterToken, stubErr)
		s := newInboundsTestServer(t, clusterToken, nodeA)
		api := s.RestfulServer.Group("/api")
		registerEndpoint(s, api, s.inboundListEndpoint())

		w := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/inbounds", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "container down") {
			t.Fatalf("expected body to mention 'container down', got %s", w.Body.String())
		}
	})

	t.Run("unregistered remote node yields empty array []", func(t *testing.T) {
		// Node is valid (fresh CreateTime) so GetTargetNodes returns it,
		// but RegisteredRemote() is false (empty outToken), so succList and failedList are empty.
		unregNode := &cluster.Node{
			Node:       &proto.Node{Name: "node-unreg", Host: "127.0.0.1", Port: 19999, NodeId: "id-u"},
			CreateTime: time.Now().Unix(),
		}
		s := newInboundsTestServer(t, clusterToken, unregNode)
		api := s.RestfulServer.Group("/api")
		registerEndpoint(s, api, s.inboundListEndpoint())

		w := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/inbounds", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		if strings.TrimSpace(w.Body.String()) != "[]" {
			t.Fatalf("expected empty JSON array '[]', got %q", w.Body.String())
		}
	})
}

func TestInboundGetEndpoint_HTTP(t *testing.T) {
	const clusterToken = "test-cluster-token-123456"

	t.Run("container validation rejects non-xray with 400 and defaults to xray", func(t *testing.T) {
		s := newInboundsTestServer(t, clusterToken)
		handler := &InboundGetHandler{}
		handler.setHttpServer(s)
		r := handlerEngine(http.MethodGet, "/inbound", handler.handlerFunc)

		for _, tc := range []struct {
			name       string
			query      string
			wantStatus int
			wantSubstr string
		}{
			{"snell rejected 400", "container=snell&src_tag=t1", http.StatusBadRequest, `container "snell" not supported via GET /inbound`},
			{"hysteria rejected 400", "container=hysteria", http.StatusBadRequest, `container "hysteria" not supported via GET /inbound`},
			{"unknown rejected 400", "container=mihomo", http.StatusBadRequest, `container "mihomo" not supported via GET /inbound`},
			{"default container=xray proceeds to node check (502)", "src_tag=t1", http.StatusBadGateway, "no available node"},
			{"empty container= falls back to xray (502)", "container=", http.StatusBadGateway, "no available node"},
			{"explicit container=xray proceeds to node check (502)", "container=xray", http.StatusBadGateway, "no available node"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				url := "/inbound"
				if tc.query != "" {
					url += "?" + tc.query
				}
				r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
				if w.Code != tc.wantStatus {
					t.Fatalf("expected %d, got %d body=%s", tc.wantStatus, w.Code, w.Body.String())
				}
				var envelope struct {
					Code int    `json:"code"`
					Msg  string `json:"msg"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
					t.Fatalf("unmarshal error body %q: %v", w.Body.String(), err)
				}
				if envelope.Code != tc.wantStatus {
					t.Fatalf("expected code %d, got %+v", tc.wantStatus, envelope)
				}
				if !strings.Contains(envelope.Msg, tc.wantSubstr) {
					t.Fatalf("expected msg %q to contain %q", envelope.Msg, tc.wantSubstr)
				}
			})
		}
	})

	t.Run("success forwards src_tag and returns succList; empty returns code=0 ok", func(t *testing.T) {
		stubA := &stubEndNodeRPCServer{
			getInboundFn: func(ctx context.Context, req *proto.GetInboundReq) (*proto.GetInboundRsp, error) {
				return &proto.GetInboundRsp{Code: 0, Data: `{"tag":"` + req.GetTag() + `","protocol":"vless"}`}, nil
			},
		}
		nodeA := startTestEndNode(t, "node-a", "id-a", clusterToken, stubA)
		s := newInboundsTestServer(t, clusterToken, nodeA)
		s.registerOn(s.RestfulServer, &HelpHandler{}, http.MethodGet)
		api := s.RestfulServer.Group("/api")
		registerEndpoint(s, api, s.inboundGetEndpoint())

		w := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/inbound?src_tag=my-vless", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		if stubA.getLastInboundTag() != "my-vless" {
			t.Fatalf("expected src_tag 'my-vless' forwarded to RPC, got %q", stubA.getLastInboundTag())
		}
		var got map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal response: %v (body=%s)", err, w.Body.String())
		}
		if !strings.Contains(got["node-a"], `"tag":"my-vless"`) {
			t.Fatalf("unexpected node-a response: %+v", got)
		}

		// Verify /help/inbound matches legacy help text exactly.
		wHelp := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(wHelp, httptest.NewRequest(http.MethodGet, "/help/inbound", nil))
		if wHelp.Code != http.StatusOK || wHelp.Body.String() != (&InboundGetHandler{}).help() {
			t.Fatalf("unexpected /help/inbound: code=%d body=%q", wHelp.Code, wHelp.Body.String())
		}
	})

	t.Run("RPC failure returns 500 and empty succ/failed returns legacy code=0 ok", func(t *testing.T) {
		stubErr := &stubEndNodeRPCServer{
			getInboundFn: func(ctx context.Context, req *proto.GetInboundReq) (*proto.GetInboundRsp, error) {
				return &proto.GetInboundRsp{Code: 300, Msg: "inbound not found"}, nil
			},
		}
		nodeErr := startTestEndNode(t, "node-err", "id-err", clusterToken, stubErr)
		sErr := newInboundsTestServer(t, clusterToken, nodeErr)
		registerEndpoint(sErr, sErr.RestfulServer.Group("/api"), sErr.inboundGetEndpoint())

		w := httptest.NewRecorder()
		sErr.RestfulServer.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/inbound?src_tag=missing", nil))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "inbound not found") {
			t.Fatalf("expected 'inbound not found' in body, got %s", w.Body.String())
		}

		// Unregistered remote node -> empty succList & failedList -> legacy {"code":0,"msg":"ok"}.
		unregNode := &cluster.Node{
			Node:       &proto.Node{Name: "node-unreg", Host: "127.0.0.1", Port: 19999, NodeId: "id-u"},
			CreateTime: time.Now().Unix(),
		}
		sEmpty := newInboundsTestServer(t, clusterToken, unregNode)
		registerEndpoint(sEmpty, sEmpty.RestfulServer.Group("/api"), sEmpty.inboundGetEndpoint())

		w2 := httptest.NewRecorder()
		sEmpty.RestfulServer.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/inbound", nil))
		if w2.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w2.Code, w2.Body.String())
		}
		var okResp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(w2.Body.Bytes(), &okResp); err != nil {
			t.Fatalf("unmarshal ok resp: %v", err)
		}
		if okResp.Code != 0 || okResp.Msg != "ok" {
			t.Fatalf("expected {code:0 msg:'ok'}, got %+v", okResp)
		}
	})
}

func TestCertsListEndpoint_HTTP(t *testing.T) {
	const clusterToken = "test-cluster-token-123456"

	t.Run("no available node returns 502 via GetCertsHandler", func(t *testing.T) {
		s := newInboundsTestServer(t, clusterToken)
		handler := &GetCertsHandler{}
		handler.setHttpServer(s)

		r := handlerEngine(http.MethodGet, "/getCerts", handler.handlerFunc)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/getCerts", nil))

		if w.Code != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "no available node") {
			t.Fatalf("expected 'no available node' in body, got %s", w.Body.String())
		}
	})

	t.Run("default target=all returns certs across nodes and preserves partial success", func(t *testing.T) {
		stubA := &stubEndNodeRPCServer{
			getCertsFn: func(ctx context.Context, req *proto.GetCertsReq) (*proto.GetCertsRsp, error) {
				return &proto.GetCertsRsp{
					Code: 0,
					Certs: []*proto.Cert{
						{Domain: "a.example.com", ExpireTime: "2027-01-01"},
					},
				}, nil
			},
		}
		stubB := &stubEndNodeRPCServer{
			getCertsFn: func(ctx context.Context, req *proto.GetCertsReq) (*proto.GetCertsRsp, error) {
				return &proto.GetCertsRsp{
					Code: 0,
					Certs: []*proto.Cert{
						{Domain: "b.example.com", ExpireTime: "2027-06-01"},
					},
				}, nil
			},
		}
		nodeA := startTestEndNode(t, "node-a", "id-a", clusterToken, stubA)
		nodeB := startTestEndNode(t, "node-b", "id-b", clusterToken, stubB)
		s := newInboundsTestServer(t, clusterToken, nodeA, nodeB)
		s.registerOn(s.RestfulServer, &HelpHandler{}, http.MethodGet)
		registerEndpoint(s, s.RestfulServer.Group("/api"), s.certsListEndpoint())

		w := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/getCerts", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		var certs map[string][]map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &certs); err != nil {
			t.Fatalf("unmarshal certs: %v (body=%s)", err, w.Body.String())
		}
		if len(certs["node-a"]) != 1 || certs["node-a"][0]["domain"] != "a.example.com" {
			t.Fatalf("unexpected node-a certs: %+v", certs["node-a"])
		}
		if len(certs["node-b"]) != 1 || certs["node-b"][0]["domain"] != "b.example.com" {
			t.Fatalf("unexpected node-b certs: %+v", certs["node-b"])
		}

		// Verify /help/getCerts matches legacy help text exactly.
		wHelp := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(wHelp, httptest.NewRequest(http.MethodGet, "/help/getCerts", nil))
		if wHelp.Code != http.StatusOK || wHelp.Body.String() != (&GetCertsHandler{}).help() {
			t.Fatalf("unexpected /help/getCerts: code=%d body=%q", wHelp.Code, wHelp.Body.String())
		}
	})

	t.Run("partial RPC failure still returns 200 with successful nodes", func(t *testing.T) {
		stubOK := &stubEndNodeRPCServer{
			getCertsFn: func(ctx context.Context, req *proto.GetCertsReq) (*proto.GetCertsRsp, error) {
				return &proto.GetCertsRsp{
					Code:  0,
					Certs: []*proto.Cert{{Domain: "ok.example.com", ExpireTime: "2027-03-01"}},
				}, nil
			},
		}
		nodeOK := startTestEndNode(t, "node-ok", "id-ok", clusterToken, stubOK)
		// Unreachable port causes RPC failure on node-fail while node-ok succeeds.
		badLis, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		deadPort := badLis.Addr().(*net.TCPAddr).Port
		_ = badLis.Close()
		now := time.Now().Unix()
		nodeFail := &cluster.Node{
			Node:       &proto.Node{Name: "node-fail", Host: "127.0.0.1", Port: int32(deadPort), NodeId: "id-fail"},
			CreateTime: now,
		}
		nodeFail.SetOutToken("peer-out-token")
		nodeFail.SetReportHeartBeatTime(now)
		t.Cleanup(nodeFail.CloseConn)

		s := newInboundsTestServer(t, clusterToken, nodeOK, nodeFail)
		registerEndpoint(s, s.RestfulServer.Group("/api"), s.certsListEndpoint())

		w := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/getCerts", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 on partial failure, got %d body=%s", w.Code, w.Body.String())
		}
		var certs map[string][]map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &certs); err != nil {
			t.Fatalf("unmarshal partial certs: %v", err)
		}
		if len(certs["node-ok"]) != 1 || certs["node-ok"][0]["domain"] != "ok.example.com" {
			t.Fatalf("expected node-ok certs preserved, got %+v", certs)
		}
		if _, exists := certs["node-fail"]; exists {
			t.Fatalf("did not expect failed node in succList, got %+v", certs)
		}
	})
}

// ---------------------------------------------------------------------------
// MCP Integration Tests (Real MCP Client + StreamableClientTransport)
// ---------------------------------------------------------------------------

func TestMCP_InboundsAndCertsEndpoints(t *testing.T) {
	const clusterToken = "mcp-inbounds-cluster-token"

	stubA := &stubEndNodeRPCServer{
		listInboundFn: func(ctx context.Context, req *proto.ListInboundReq) (*proto.ListInboundRsp, error) {
			return &proto.ListInboundRsp{
				Code: 0,
				Inbounds: []*proto.InboundInfo{
					{Container: "xray", Name: "xray-vless"},
					{Container: "mihomo", Name: "mihomo-hy2"},
				},
			}, nil
		},
		getInboundFn: func(ctx context.Context, req *proto.GetInboundReq) (*proto.GetInboundRsp, error) {
			return &proto.GetInboundRsp{
				Code: 0,
				Data: `{"node":"node-a","tag":"` + req.GetTag() + `"}`,
			}, nil
		},
		getCertsFn: func(ctx context.Context, req *proto.GetCertsReq) (*proto.GetCertsRsp, error) {
			return &proto.GetCertsRsp{
				Code: 0,
				Certs: []*proto.Cert{
					{Domain: "alpha.example.com", ExpireTime: "2027-02-01"},
				},
			}, nil
		},
	}
	stubB := &stubEndNodeRPCServer{
		listInboundFn: func(ctx context.Context, req *proto.ListInboundReq) (*proto.ListInboundRsp, error) {
			return &proto.ListInboundRsp{
				Code: 0,
				Inbounds: []*proto.InboundInfo{
					{Container: "snell", Name: "snell-main"},
				},
			}, nil
		},
		getInboundFn: func(ctx context.Context, req *proto.GetInboundReq) (*proto.GetInboundRsp, error) {
			return &proto.GetInboundRsp{
				Code: 0,
				Data: `{"node":"node-b","tag":"` + req.GetTag() + `"}`,
			}, nil
		},
		getCertsFn: func(ctx context.Context, req *proto.GetCertsReq) (*proto.GetCertsRsp, error) {
			return &proto.GetCertsRsp{
				Code: 0,
				Certs: []*proto.Cert{
					{Domain: "beta.example.com", ExpireTime: "2027-08-01"},
				},
			}, nil
		},
	}

	nodeA := startTestEndNode(t, "node-a", "id-a", clusterToken, stubA)
	nodeB := startTestEndNode(t, "node-b", "id-b", clusterToken, stubB)

	s := newInboundsTestServer(t, clusterToken, nodeA, nodeB)
	s.MCPReadOnly = true // all 3 endpoints are ReadOnly: true and must remain visible in read-only mode
	adminGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret), auth.AdminOnly())
	registerEndpoint(s, adminGroup, s.inboundListEndpoint())
	registerEndpoint(s, adminGroup, s.inboundGetEndpoint())
	registerEndpoint(s, adminGroup, s.certsListEndpoint())
	RegisterMCP(s)

	ts := httptest.NewServer(s.RestfulServer)
	defer ts.Close()

	ctx := context.Background()

	t.Run("admin sees all 3 read-only tools and normal JWT user sees none", func(t *testing.T) {
		adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})
		adminList, err := adminSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("admin ListTools failed: %v", err)
		}
		got := toolNames(adminList.Tools)
		want := []string{"get_inbound", "list_certs", "list_inbounds"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("admin tools mismatch: got %v want %v", got, want)
		}
		for _, tl := range adminList.Tools {
			if tl.Description == "" {
				t.Errorf("tool %s missing description", tl.Name)
			}
			if tl.Annotations == nil || !tl.Annotations.ReadOnlyHint {
				t.Errorf("tool %s expected ReadOnlyHint=true, got %+v", tl.Name, tl.Annotations)
			}
			if tl.Annotations == nil || tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint {
				t.Errorf("tool %s expected DestructiveHint=false, got %+v", tl.Name, tl.Annotations)
			}
			schemaBytes, err := json.Marshal(tl.InputSchema)
			if err != nil {
				t.Fatalf("marshal schema for %s: %v", tl.Name, err)
			}
			var schemaMap struct {
				Properties map[string]struct {
					Description string `json:"description"`
				} `json:"properties"`
				Required []string `json:"required"`
			}
			if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
				t.Fatalf("unmarshal schema for %s: %v", tl.Name, err)
			}
			if len(schemaMap.Required) != 0 {
				t.Errorf("tool %s should have all optional params, got required=%v", tl.Name, schemaMap.Required)
			}
			if prop, ok := schemaMap.Properties["target"]; !ok || prop.Description == "" {
				t.Errorf("tool %s missing target property description in schema: %s", tl.Name, string(schemaBytes))
			}
			if tl.Name == "get_inbound" {
				for _, field := range []string{"src_tag", "container"} {
					if prop, ok := schemaMap.Properties[field]; !ok || prop.Description == "" {
						t.Errorf("get_inbound missing %s property description in schema: %s", field, string(schemaBytes))
					}
				}
			}
		}

		// Normal JWT user MUST NOT see any of the 3 admin-only tools.
		normalJWT := makeJWT(t, s, "alice", "normal")
		userSess := connectMCPClient(t, ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
		userList, err := userSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("normal user ListTools failed: %v", err)
		}
		if len(userList.Tools) != 0 {
			t.Fatalf("expected normal user to see 0 admin tools, got %v", toolNames(userList.Tools))
		}

		// Calling any admin tool directly as normal user must fail.
		for _, name := range want {
			if _, err := userSess.CallTool(ctx, &mcp.CallToolParams{Name: name}); err == nil {
				t.Fatalf("expected error when normal user calls admin tool %s", name)
			}
		}
	})

	t.Run("list_inbounds default target=all and single target", func(t *testing.T) {
		adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})

		res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_inbounds"})
		if err != nil {
			t.Fatalf("CallTool list_inbounds: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected IsError: %s", extractText(t, res))
		}
		var all map[string][]map[string]any
		if err := json.Unmarshal([]byte(extractText(t, res)), &all); err != nil {
			t.Fatalf("unmarshal list_inbounds: %v", err)
		}
		if len(all["node-a"]) != 2 || len(all["node-b"]) != 1 {
			t.Fatalf("expected both node-a (2) and node-b (1) when target omitted, got %+v", all)
		}

		resSingle, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_inbounds",
			Arguments: map[string]any{"target": "node-b"},
		})
		if err != nil {
			t.Fatalf("CallTool list_inbounds single: %v", err)
		}
		if resSingle.IsError {
			t.Fatalf("unexpected IsError: %s", extractText(t, resSingle))
		}
		var onlyB map[string][]map[string]any
		if err := json.Unmarshal([]byte(extractText(t, resSingle)), &onlyB); err != nil {
			t.Fatalf("unmarshal single: %v", err)
		}
		if len(onlyB) != 1 || len(onlyB["node-b"]) != 1 {
			t.Fatalf("expected only node-b, got %+v", onlyB)
		}
	})

	t.Run("get_inbound defaults parity (target=all, container=xray) and validation errors", func(t *testing.T) {
		adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})

		// Omitted args -> target="all", container="xray"
		res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "get_inbound"})
		if err != nil {
			t.Fatalf("CallTool get_inbound: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected IsError: %s", extractText(t, res))
		}
		var gotAll map[string]string
		if err := json.Unmarshal([]byte(extractText(t, res)), &gotAll); err != nil {
			t.Fatalf("unmarshal get_inbound: %v", err)
		}
		if _, okA := gotAll["node-a"]; !okA {
			t.Fatalf("expected node-a in default get_inbound response, got %+v", gotAll)
		}
		if _, okB := gotAll["node-b"]; !okB {
			t.Fatalf("expected node-b in default get_inbound response, got %+v", gotAll)
		}

		// With src_tag & empty container (should default to xray) & single target
		resTag, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_inbound",
			Arguments: map[string]any{"target": "node-a", "src_tag": "vless-443", "container": ""},
		})
		if err != nil {
			t.Fatalf("CallTool get_inbound with src_tag: %v", err)
		}
		if resTag.IsError {
			t.Fatalf("unexpected IsError: %s", extractText(t, resTag))
		}
		var gotTag map[string]string
		if err := json.Unmarshal([]byte(extractText(t, resTag)), &gotTag); err != nil {
			t.Fatalf("unmarshal get_inbound tag: %v", err)
		}
		if len(gotTag) != 1 || !strings.Contains(gotTag["node-a"], `"tag":"vless-443"`) {
			t.Fatalf("unexpected get_inbound response: %+v", gotTag)
		}

		// Non-xray container -> MCP IsError: true
		resBad, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_inbound",
			Arguments: map[string]any{"container": "snell"},
		})
		if err != nil {
			t.Fatalf("expected tool error (IsError=true), not protocol error: %v", err)
		}
		if !resBad.IsError {
			t.Fatalf("expected IsError=true for container=snell")
		}
		if txt := extractText(t, resBad); !strings.Contains(txt, `container "snell" not supported`) {
			t.Fatalf("unexpected error text: %q", txt)
		}
	})

	t.Run("list_certs default target=all and single target", func(t *testing.T) {
		adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})

		res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_certs"})
		if err != nil {
			t.Fatalf("CallTool list_certs: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected IsError: %s", extractText(t, res))
		}
		var all map[string][]map[string]any
		if err := json.Unmarshal([]byte(extractText(t, res)), &all); err != nil {
			t.Fatalf("unmarshal list_certs: %v", err)
		}
		if len(all["node-a"]) != 1 || len(all["node-b"]) != 1 {
			t.Fatalf("expected certs from both nodes when target omitted, got %+v", all)
		}

		resSingle, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_certs",
			Arguments: map[string]any{"target": "node-a"},
		})
		if err != nil {
			t.Fatalf("CallTool list_certs single: %v", err)
		}
		if resSingle.IsError {
			t.Fatalf("unexpected IsError: %s", extractText(t, resSingle))
		}
		var onlyA map[string][]map[string]any
		if err := json.Unmarshal([]byte(extractText(t, resSingle)), &onlyA); err != nil {
			t.Fatalf("unmarshal single certs: %v", err)
		}
		if len(onlyA) != 1 || onlyA["node-a"][0]["domain"] != "alpha.example.com" {
			t.Fatalf("expected only node-a cert, got %+v", onlyA)
		}
	})

	t.Run("all 3 tools return IsError=true on no available node", func(t *testing.T) {
		adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})

		for _, toolName := range []string{"list_inbounds", "get_inbound", "list_certs"} {
			res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
				Name:      toolName,
				Arguments: map[string]any{"target": "nonexistent-node"},
			})
			if err != nil {
				t.Fatalf("%s: expected tool error (IsError=true), not protocol error: %v", toolName, err)
			}
			if !res.IsError {
				t.Fatalf("%s: expected IsError=true when target has no available node", toolName)
			}
			if txt := extractText(t, res); !strings.Contains(txt, "no available node") {
				t.Fatalf("%s: expected 'no available node' in error text, got %q", toolName, txt)
			}
		}
	})
}
