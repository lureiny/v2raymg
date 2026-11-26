package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
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

// stubInboundsWriteRPCServer is an in-process EndNodeAccess gRPC server for
// testing POST /inbound, DELETE /inbounds, and POST /inbound/fast end-to-end.
type stubInboundsWriteRPCServer struct {
	proto.UnimplementedEndNodeAccessServer

	mu           sync.Mutex
	addReqs      []*proto.InboundOpReq
	deleteReqs   []*proto.DeleteInboundByNameReq
	fastAddReqs  []*proto.FastAddInboundReq
	addInboundFn func(ctx context.Context, req *proto.InboundOpReq) (*proto.InboundOpRsp, error)
	deleteByNmFn func(ctx context.Context, req *proto.DeleteInboundByNameReq) (*proto.InboundOpRsp, error)
	fastAddInbFn func(ctx context.Context, req *proto.FastAddInboundReq) (*proto.FastAddInboundRsp, error)
}

func (s *stubInboundsWriteRPCServer) AddInbound(ctx context.Context, req *proto.InboundOpReq) (*proto.InboundOpRsp, error) {
	s.mu.Lock()
	s.addReqs = append(s.addReqs, req)
	fn := s.addInboundFn
	s.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return &proto.InboundOpRsp{Code: 0}, nil
}

func (s *stubInboundsWriteRPCServer) DeleteInboundByName(ctx context.Context, req *proto.DeleteInboundByNameReq) (*proto.InboundOpRsp, error) {
	s.mu.Lock()
	s.deleteReqs = append(s.deleteReqs, req)
	fn := s.deleteByNmFn
	s.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return &proto.InboundOpRsp{Code: 0}, nil
}

func (s *stubInboundsWriteRPCServer) FastAddInbound(ctx context.Context, req *proto.FastAddInboundReq) (*proto.FastAddInboundRsp, error) {
	s.mu.Lock()
	s.fastAddReqs = append(s.fastAddReqs, req)
	fn := s.fastAddInbFn
	s.mu.Unlock()
	if fn != nil {
		return fn(ctx, req)
	}
	return &proto.FastAddInboundRsp{Code: 0}, nil
}

func (s *stubInboundsWriteRPCServer) counts() (addCount, delCount, fastCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.addReqs), len(s.deleteReqs), len(s.fastAddReqs)
}

func (s *stubInboundsWriteRPCServer) lastAdd() *proto.InboundOpReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.addReqs) == 0 {
		return nil
	}
	return s.addReqs[len(s.addReqs)-1]
}

func (s *stubInboundsWriteRPCServer) lastDelete() *proto.DeleteInboundByNameReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.deleteReqs) == 0 {
		return nil
	}
	return s.deleteReqs[len(s.deleteReqs)-1]
}

func (s *stubInboundsWriteRPCServer) lastFastAdd() *proto.FastAddInboundReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.fastAddReqs) == 0 {
		return nil
	}
	return s.fastAddReqs[len(s.fastAddReqs)-1]
}

func startInboundsWriteEndNode(t *testing.T, name, nodeID, clusterToken string, stub *stubInboundsWriteRPCServer) *cluster.Node {
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

// ---------------------------------------------------------------------------
// HTTP Backwards Compatibility Tests
// ---------------------------------------------------------------------------

func TestInboundAddEndpoint_HTTP(t *testing.T) {
	const clusterToken = "inbounds-write-add-token"

	t.Run("validation errors and 502 via legacy InboundAddHandler", func(t *testing.T) {
		s := newInboundsTestServer(t, clusterToken)
		h := &InboundAddHandler{}
		h.setHttpServer(s)
		r := handlerEngine(http.MethodPost, "/inbound", h.handlerFunc)

		cases := []struct {
			name       string
			body       string
			wantStatus int
			wantSubstr string
		}{
			{"invalid json returns 400", "{bad json", http.StatusBadRequest, "invalid request body"},
			{"empty body returns 400", "", http.StatusBadRequest, "invalid request body"},
			{"snell container rejected 400", `{"container":"snell","bound_raw_string":"e30="}`, http.StatusBadRequest, `container "snell" not supported via POST /inbound; use POST /inbound/fast instead`},
			{"hysteria container rejected 400", `{"container":"hysteria","bound_raw_string":"e30="}`, http.StatusBadRequest, `container "hysteria" not supported via POST /inbound; use POST /inbound/fast instead`},
			{"mihomo container rejected 400", `{"container":"mihomo","bound_raw_string":"e30="}`, http.StatusBadRequest, `container "mihomo" not supported via POST /inbound; use POST /inbound/fast instead`},
			{"omitted container reaches node check 502", `{"bound_raw_string":"e30="}`, http.StatusBadGateway, "no available node"},
			{"explicit xray container reaches node check 502", `{"container":"xray","bound_raw_string":"e30="}`, http.StatusBadGateway, "no available node"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/inbound", strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				if w.Code != tc.wantStatus {
					t.Fatalf("expected %d, got %d body=%s", tc.wantStatus, w.Code, w.Body.String())
				}
				var env struct {
					Code int    `json:"code"`
					Msg  string `json:"msg"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
					t.Fatalf("unmarshal body %q: %v", w.Body.String(), err)
				}
				if env.Code != tc.wantStatus || !strings.Contains(env.Msg, tc.wantSubstr) {
					t.Fatalf("unexpected envelope: %+v (want code=%d substr=%q)", env, tc.wantStatus, tc.wantSubstr)
				}
			})
		}
	})

	t.Run("success defaults target to local node, returns ok, and preserves help text", func(t *testing.T) {
		stubA := &stubInboundsWriteRPCServer{}
		stubB := &stubInboundsWriteRPCServer{}
		nodeA := startInboundsWriteEndNode(t, "node-a", "id-a", clusterToken, stubA)
		nodeB := startInboundsWriteEndNode(t, "node-b", "id-b", clusterToken, stubB)

		s := newInboundsTestServer(t, clusterToken, nodeA, nodeB)
		s.registerOn(s.RestfulServer, &HelpHandler{}, http.MethodGet)
		registerEndpoint(s, s.RestfulServer.Group("/api"), s.inboundAddEndpoint())

		// Empty target defaults to s.Name ("node-a") via resolveTarget.
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/inbound", strings.NewReader(`{"bound_raw_string":"eyJ0YWciOiJ2bGVzcyJ9"}`))
		req.Header.Set("Content-Type", "application/json")
		s.RestfulServer.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		var okBody struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &okBody); err != nil {
			t.Fatalf("unmarshal ok body: %v", err)
		}
		if okBody.Code != 0 || okBody.Msg != "ok" {
			t.Fatalf("expected {code:0 msg:'ok'}, got %+v", okBody)
		}
		if got := stubA.lastAdd(); got == nil || got.GetInboundInfo() != "eyJ0YWciOiJ2bGVzcyJ9" {
			t.Fatalf("expected node-a to receive bound_raw_string, got %+v", got)
		}
		if addB, _, _ := stubB.counts(); addB != 0 {
			t.Fatalf("expected node-b not called when target omitted, got %d calls", addB)
		}

		// Explicit target="node-b" routes to node-b.
		wB := httptest.NewRecorder()
		reqB := httptest.NewRequest(http.MethodPost, "/api/inbound", strings.NewReader(`{"target":"node-b","container":"xray","bound_raw_string":"e30="}`))
		reqB.Header.Set("Content-Type", "application/json")
		s.RestfulServer.ServeHTTP(wB, reqB)
		if wB.Code != http.StatusOK {
			t.Fatalf("expected 200 for node-b, got %d body=%s", wB.Code, wB.Body.String())
		}
		if got := stubB.lastAdd(); got == nil || got.GetInboundInfo() != "e30=" {
			t.Fatalf("expected node-b to receive bound_raw_string, got %+v", got)
		}

		// /help/inbound matches legacy help text byte-for-byte.
		wantHelp := `POST /api/inbound
	添加inbound（仅支持 xray container）
	body: {"target": "", "bound_raw_string": "", "container": "xray"}
	bound_raw_string: json中inbound配置base64编码后的字符串
	container: 可选，目前仅支持 "xray"（默认）；其他container请使用 POST /api/inbound/fast`
		if (&InboundAddHandler{}).help() != wantHelp {
			t.Fatalf("legacy InboundAddHandler.help() drifted")
		}
		wHelp := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(wHelp, httptest.NewRequest(http.MethodGet, "/help/inbound", nil))
		if wHelp.Code != http.StatusOK || wHelp.Body.String() != wantHelp {
			t.Fatalf("unexpected /help/inbound: code=%d body=%q", wHelp.Code, wHelp.Body.String())
		}
	})

	t.Run("RPC failure returns 500 with failed node message", func(t *testing.T) {
		stubErr := &stubInboundsWriteRPCServer{
			addInboundFn: func(ctx context.Context, req *proto.InboundOpReq) (*proto.InboundOpRsp, error) {
				return &proto.InboundOpRsp{Code: 601, Msg: "duplicate tag"}, nil
			},
		}
		nodeA := startInboundsWriteEndNode(t, "node-a", "id-a", clusterToken, stubErr)
		s := newInboundsTestServer(t, clusterToken, nodeA)
		registerEndpoint(s, s.RestfulServer.Group("/api"), s.inboundAddEndpoint())

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/inbound", strings.NewReader(`{"bound_raw_string":"e30="}`))
		req.Header.Set("Content-Type", "application/json")
		s.RestfulServer.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "duplicate tag") {
			t.Fatalf("expected body to mention 'duplicate tag', got %s", w.Body.String())
		}
	})
}

func TestInboundDeleteByNameEndpoint_HTTP(t *testing.T) {
	const clusterToken = "inbounds-write-del-token"

	t.Run("validation errors and 502 via legacy InboundDeleteByNameHandler", func(t *testing.T) {
		s := newInboundsTestServer(t, clusterToken)
		h := &InboundDeleteByNameHandler{}
		h.setHttpServer(s)
		r := handlerEngine(http.MethodDelete, "/inbounds", h.handlerFunc)

		cases := []struct {
			name       string
			body       string
			wantStatus int
			wantSubstr string
		}{
			{"invalid json returns 400", "{bad", http.StatusBadRequest, "invalid request body"},
			{"empty body returns 400", "", http.StatusBadRequest, "invalid request body"},
			{"missing container returns 400", `{"name":"vless-in"}`, http.StatusBadRequest, "container is required"},
			{"missing name returns 400", `{"container":"xray"}`, http.StatusBadRequest, "name is required"},
			{"valid fields with no node returns 502", `{"container":"mihomo","name":"hy2-in"}`, http.StatusBadGateway, "no available node"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodDelete, "/inbounds", strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				if w.Code != tc.wantStatus {
					t.Fatalf("expected %d, got %d body=%s", tc.wantStatus, w.Code, w.Body.String())
				}
				var env struct {
					Code int    `json:"code"`
					Msg  string `json:"msg"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
					t.Fatalf("unmarshal body %q: %v", w.Body.String(), err)
				}
				if env.Code != tc.wantStatus || !strings.Contains(env.Msg, tc.wantSubstr) {
					t.Fatalf("unexpected envelope: %+v (want code=%d substr=%q)", env, tc.wantStatus, tc.wantSubstr)
				}
			})
		}
	})

	t.Run("success forwards container+name, returns ok, and preserves help text", func(t *testing.T) {
		stubA := &stubInboundsWriteRPCServer{}
		nodeA := startInboundsWriteEndNode(t, "node-a", "id-a", clusterToken, stubA)
		s := newInboundsTestServer(t, clusterToken, nodeA)
		s.registerOn(s.RestfulServer, &HelpHandler{}, http.MethodGet)
		registerEndpoint(s, s.RestfulServer.Group("/api"), s.inboundDeleteByNameEndpoint())

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/inbounds", strings.NewReader(`{"container":"mihomo","name":"hy2-443"}`))
		req.Header.Set("Content-Type", "application/json")
		s.RestfulServer.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
		}
		var okBody struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &okBody); err != nil {
			t.Fatalf("unmarshal ok body: %v", err)
		}
		if okBody.Code != 0 || okBody.Msg != "ok" {
			t.Fatalf("expected {code:0 msg:'ok'}, got %+v", okBody)
		}
		got := stubA.lastDelete()
		if got == nil || got.GetContainer() != "mihomo" || got.GetName() != "hy2-443" {
			t.Fatalf("unexpected RPC DeleteInboundByNameReq: %+v", got)
		}

		wantHelp := `DELETE /api/inbounds
	按 container 类型 + name 删除 inbound
	body: {"target": "", "container": "xray|snell|hysteria|mihomo", "name": "<inbound tag>"}`
		if (&InboundDeleteByNameHandler{}).help() != wantHelp {
			t.Fatalf("legacy InboundDeleteByNameHandler.help() drifted")
		}
		wHelp := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(wHelp, httptest.NewRequest(http.MethodGet, "/help/inbounds", nil))
		if wHelp.Code != http.StatusOK || wHelp.Body.String() != wantHelp {
			t.Fatalf("unexpected /help/inbounds: code=%d body=%q", wHelp.Code, wHelp.Body.String())
		}
	})

	t.Run("RPC failure returns 500", func(t *testing.T) {
		stubErr := &stubInboundsWriteRPCServer{
			deleteByNmFn: func(ctx context.Context, req *proto.DeleteInboundByNameReq) (*proto.InboundOpRsp, error) {
				return &proto.InboundOpRsp{Code: 300, Msg: "inbound not found"}, nil
			},
		}
		nodeA := startInboundsWriteEndNode(t, "node-a", "id-a", clusterToken, stubErr)
		s := newInboundsTestServer(t, clusterToken, nodeA)
		registerEndpoint(s, s.RestfulServer.Group("/api"), s.inboundDeleteByNameEndpoint())

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/inbounds", strings.NewReader(`{"container":"xray","name":"ghost"}`))
		req.Header.Set("Content-Type", "application/json")
		s.RestfulServer.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "inbound not found") {
			t.Fatalf("expected body to mention 'inbound not found', got %s", w.Body.String())
		}
	})
}

func TestFastAddInboundEndpoint_HTTP(t *testing.T) {
	const clusterToken = "inbounds-write-fastadd-token"

	t.Run("validation errors and 502 via legacy FastAddInboundHandler", func(t *testing.T) {
		s := newInboundsTestServer(t, clusterToken)
		h := &FastAddInboundHandler{}
		h.setHttpServer(s)
		r := handlerEngine(http.MethodPost, "/inbound/fast", h.handlerFunc)

		cases := []struct {
			name       string
			body       string
			wantStatus int
			wantSubstr string
		}{
			{"invalid json returns 400", "{invalid", http.StatusBadRequest, "invalid request body"},
			{"empty body returns 400", "", http.StatusBadRequest, "invalid request body"},
			{"unsupported protocol returns 400", `{"protocol":"socks"}`, http.StatusBadRequest, "unsupported protocol: socks"},
			{"h3 transport returns specific 400 hint", `{"protocol":"vless","transport":"h3"}`, http.StatusBadRequest, `transport "h3" is not supported; use xhttp or splithttp instead`},
			{"unknown transport returns 400", `{"protocol":"vless","transport":"kcp"}`, http.StatusBadRequest, "unsupported transport: kcp"},
			{"default protocol vless+tcp reaches 502", `{}`, http.StatusBadGateway, "no available node"},
			{"hysteria2 without transport reaches 502", `{"protocol":"hysteria2","container":"mihomo"}`, http.StatusBadGateway, "no available node"},
			{"tuic with quic transport alias reaches 502", `{"protocol":"tuic","transport":"quic","container":"mihomo"}`, http.StatusBadGateway, "no available node"},
			{"anytls with tcp transport alias reaches 502", `{"protocol":"anytls","transport":"tcp","container":"mihomo"}`, http.StatusBadGateway, "no available node"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPost, "/inbound/fast", strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				if w.Code != tc.wantStatus {
					t.Fatalf("expected %d, got %d body=%s", tc.wantStatus, w.Code, w.Body.String())
				}
				var env struct {
					Code int    `json:"code"`
					Msg  string `json:"msg"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
					t.Fatalf("unmarshal body %q: %v", w.Body.String(), err)
				}
				if env.Code != tc.wantStatus || !strings.Contains(env.Msg, tc.wantSubstr) {
					t.Fatalf("unexpected envelope: %+v (want code=%d substr=%q)", env, tc.wantStatus, tc.wantSubstr)
				}
			})
		}
	})

	t.Run("protocol builder mapping, transport normalization, and convenience param merges", func(t *testing.T) {
		stubA := &stubInboundsWriteRPCServer{}
		nodeA := startInboundsWriteEndNode(t, "node-a", "id-a", clusterToken, stubA)
		s := newInboundsTestServer(t, clusterToken, nodeA)
		s.registerOn(s.RestfulServer, &HelpHandler{}, http.MethodGet)
		registerEndpoint(s, s.RestfulServer.Group("/api"), s.fastAddInboundEndpoint())

		postFast := func(t *testing.T, payload map[string]any) *proto.FastAddInboundReq {
			t.Helper()
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/inbound/fast", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			s.RestfulServer.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
			}
			var okBody struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &okBody); err != nil {
				t.Fatalf("unmarshal ok body: %v", err)
			}
			if okBody.Code != 0 || okBody.Msg != "ok" {
				t.Fatalf("expected {code:0 msg:'ok'}, got %+v", okBody)
			}
			return stubA.lastFastAdd()
		}

		// 1. VLESS + legacy stream="http" -> transport="h2", Reality, convenience fields & explicit extra_params override.
		gotVless := postFast(t, map[string]any{
			"tag":                  "vless-reality",
			"protocol":             "vless",
			"stream":               "http",
			"port":                 443,
			"container":            "xray",
			"security":             "reality",
			"reality_target":       "www.microsoft.com:443",
			"reality_server_names": []string{"www.microsoft.com"},
			"reality_short_ids":    []string{"abcd1234"},
			"flow":                 "xtls-rprx-vision",
			"http_path":            "/h2",
			"http_host":            "a.example.com,b.example.com",
			"alpn":                 "h2,http/1.1",
			"sniffing_enabled":     true,
			"self_signed":          true,
			"skip_cert_verify":     true,
			"extra_params": map[string]string{
				"http_path": "/explicit-override",
				"custom":    "val",
			},
		})
		if gotVless.GetInboundBuilderType() != proto.BuilderType_VLESSSettingBuilderType {
			t.Fatalf("expected VLESS builder type, got %v", gotVless.GetInboundBuilderType())
		}
		if gotVless.GetTransport() != "h2" {
			t.Fatalf("expected legacy stream=http normalized to transport=h2, got %q", gotVless.GetTransport())
		}
		if gotVless.GetPort() != 443 || gotVless.GetSecurity() != "reality" || gotVless.GetRealityTarget() != "www.microsoft.com:443" {
			t.Fatalf("unexpected vless fields: %+v", gotVless)
		}
		extra := gotVless.GetExtraParams()
		if extra["http_path"] != "/explicit-override" || extra["custom"] != "val" || extra["flow"] != "xtls-rprx-vision" ||
			extra["alpn"] != "h2,http/1.1" || extra["sniffing_enabled"] != "true" || extra["self_signed"] != "true" || extra["skip_cert_verify"] != "true" {
			t.Fatalf("unexpected vless extra_params: %+v", extra)
		}

		// 2. Shadowsocks + plugin convenience fields.
		gotSS := postFast(t, map[string]any{
			"tag":             "ss-obfs",
			"protocol":        "ss",
			"plugin":          "shadow-tls",
			"plugin_mode":     "tls",
			"plugin_host":     "edge.example.com",
			"plugin_path":     "/p",
			"plugin_tls":      true,
			"plugin_password": "secret",
			"plugin_version":  "3",
		})
		if gotSS.GetInboundBuilderType() != proto.BuilderType_SSSettingBuilderType {
			t.Fatalf("expected SS builder type, got %v", gotSS.GetInboundBuilderType())
		}
		if gotSS.GetTransport() != "tcp" {
			t.Fatalf("expected default tcp transport for ss, got %q", gotSS.GetTransport())
		}
		ssExtra := gotSS.GetExtraParams()
		if ssExtra["plugin"] != "shadow-tls" || ssExtra["plugin_tls"] != "true" || ssExtra["plugin_version"] != "3" || ssExtra["plugin_password"] != "secret" {
			t.Fatalf("unexpected ss extra_params: %+v", ssExtra)
		}

		// 3. Hysteria2 + QUIC-native transport + hy2 convenience fields.
		gotHy2 := postFast(t, map[string]any{
			"tag":                     "hy2-main",
			"protocol":                "hysteria2",
			"transport":               "quic",
			"container":               "mihomo",
			"obfs":                    "salamander",
			"obfs_password":           "obfs-pass",
			"up":                      "50 Mbps",
			"down":                    "100 Mbps",
			"masquerade":              "https://example.com",
			"ignore_client_bandwidth": true,
		})
		if gotHy2.GetInboundBuilderType() != proto.BuilderType_Hysteria2SettingBuilderType {
			t.Fatalf("expected Hysteria2 builder type, got %v", gotHy2.GetInboundBuilderType())
		}
		if gotHy2.GetTransport() != "" {
			t.Fatalf("expected empty transport for hysteria2 quic alias, got %q", gotHy2.GetTransport())
		}
		hy2Extra := gotHy2.GetExtraParams()
		if hy2Extra["obfs"] != "salamander" || hy2Extra["obfs_password"] != "obfs-pass" || hy2Extra["up"] != "50 Mbps" ||
			hy2Extra["down"] != "100 Mbps" || hy2Extra["masquerade"] != "https://example.com" || hy2Extra["ignore_client_bandwidth"] != "true" {
			t.Fatalf("unexpected hy2 extra_params: %+v", hy2Extra)
		}

		// 4. TUIC + convenience fields.
		gotTUIC := postFast(t, map[string]any{
			"tag":                   "tuic-main",
			"protocol":              "tuic",
			"container":             "mihomo",
			"congestion_controller": "bbr",
			"udp_relay_mode":        "native",
			"zero_rtt_handshake":    true,
			"heartbeat_interval":    "10s",
			"disable_sni":           true,
		})
		if gotTUIC.GetInboundBuilderType() != proto.BuilderType_TUICSettingBuilderType || gotTUIC.GetTransport() != "" {
			t.Fatalf("unexpected tuic builder/transport: %v / %q", gotTUIC.GetInboundBuilderType(), gotTUIC.GetTransport())
		}
		tuicExtra := gotTUIC.GetExtraParams()
		if tuicExtra["congestion_controller"] != "bbr" || tuicExtra["udp_relay_mode"] != "native" ||
			tuicExtra["zero_rtt_handshake"] != "true" || tuicExtra["heartbeat_interval"] != "10s" || tuicExtra["disable_sni"] != "true" {
			t.Fatalf("unexpected tuic extra_params: %+v", tuicExtra)
		}

		// 5. AnyTLS + tcp alias -> empty transport + int convenience fields (zero skipped, positive kept).
		gotAny := postFast(t, map[string]any{
			"tag":                                 "anytls-main",
			"protocol":                            "anytls",
			"transport":                           "tcp",
			"container":                           "mihomo",
			"padding_scheme":                      "stop=8",
			"idle_session_check_interval_seconds": 30,
			"idle_session_timeout_seconds":        0,
			"min_idle_session":                    2,
		})
		if gotAny.GetInboundBuilderType() != proto.BuilderType_AnyTLSSettingBuilderType || gotAny.GetTransport() != "" {
			t.Fatalf("unexpected anytls builder/transport: %v / %q", gotAny.GetInboundBuilderType(), gotAny.GetTransport())
		}
		anyExtra := gotAny.GetExtraParams()
		if anyExtra["padding_scheme"] != "stop=8" || anyExtra["idle_session_check_interval_seconds"] != "30" || anyExtra["min_idle_session"] != "2" {
			t.Fatalf("unexpected anytls extra_params: %+v", anyExtra)
		}
		if _, exists := anyExtra["idle_session_timeout_seconds"]; exists {
			t.Fatalf("expected zero idle_session_timeout_seconds to be skipped, got %q", anyExtra["idle_session_timeout_seconds"])
		}

		// /help/inbound/fast matches legacy help text byte-for-byte.
		wantHelp := (&FastAddInboundHandler{}).help()
		if wantHelp != s.fastAddInboundEndpoint().Help {
			t.Fatalf("FastAddInboundHandler.help() != fastAddInboundEndpoint().Help")
		}
		wHelp := httptest.NewRecorder()
		s.RestfulServer.ServeHTTP(wHelp, httptest.NewRequest(http.MethodGet, "/help/inbound/fast", nil))
		if wHelp.Code != http.StatusOK || wHelp.Body.String() != wantHelp {
			t.Fatalf("unexpected /help/inbound/fast: code=%d body=%q", wHelp.Code, wHelp.Body.String())
		}
	})

	t.Run("RPC failure returns 500", func(t *testing.T) {
		stubErr := &stubInboundsWriteRPCServer{
			fastAddInbFn: func(ctx context.Context, req *proto.FastAddInboundReq) (*proto.FastAddInboundRsp, error) {
				return &proto.FastAddInboundRsp{Code: 1020, Msg: "port already in use"}, nil
			},
		}
		nodeA := startInboundsWriteEndNode(t, "node-a", "id-a", clusterToken, stubErr)
		s := newInboundsTestServer(t, clusterToken, nodeA)
		registerEndpoint(s, s.RestfulServer.Group("/api"), s.fastAddInboundEndpoint())

		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/inbound/fast", strings.NewReader(`{"protocol":"vless","tag":"dup"}`))
		req.Header.Set("Content-Type", "application/json")
		s.RestfulServer.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "port already in use") {
			t.Fatalf("expected body to mention 'port already in use', got %s", w.Body.String())
		}
	})
}

// ---------------------------------------------------------------------------
// MCP Integration Tests (ReadOnly filtering, RBAC, Annotations, Schema, Exec)
// ---------------------------------------------------------------------------

func TestMCP_InboundsWriteEndpoints(t *testing.T) {
	const clusterToken = "mcp-inbounds-write-token"

	setupServer := func(t *testing.T, readOnly bool, stubA, stubB *stubInboundsWriteRPCServer) (*HttpServer, *httptest.Server) {
		t.Helper()
		var nodes []*cluster.Node
		if stubA != nil {
			nodes = append(nodes, startInboundsWriteEndNode(t, "node-a", "id-a", clusterToken, stubA))
		}
		if stubB != nil {
			nodes = append(nodes, startInboundsWriteEndNode(t, "node-b", "id-b", clusterToken, stubB))
		}
		s := newInboundsTestServer(t, clusterToken, nodes...)
		s.MCPReadOnly = readOnly
		adminGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret), auth.AdminOnly())
		registerEndpoint(s, adminGroup, s.inboundAddEndpoint())
		registerEndpoint(s, adminGroup, s.inboundDeleteByNameEndpoint())
		registerEndpoint(s, adminGroup, s.fastAddInboundEndpoint())
		RegisterMCP(s)

		ts := httptest.NewServer(s.RestfulServer)
		t.Cleanup(ts.Close)
		return s, ts
	}

	ctx := context.Background()

	t.Run("MCPReadOnly=true hides all 3 write tools from admin and normal user", func(t *testing.T) {
		s, ts := setupServer(t, true, &stubInboundsWriteRPCServer{}, nil)

		adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})
		adminList, err := adminSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("admin ListTools failed: %v", err)
		}
		if len(adminList.Tools) != 0 {
			t.Fatalf("expected 0 write tools when MCPReadOnly=true, got %v", toolNames(adminList.Tools))
		}

		normalJWT := makeJWT(t, s, "alice", "normal")
		userSess := connectMCPClient(t, ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
		userList, err := userSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("normal user ListTools failed: %v", err)
		}
		if len(userList.Tools) != 0 {
			t.Fatalf("expected 0 tools for normal user when MCPReadOnly=true, got %v", toolNames(userList.Tools))
		}
	})

	t.Run("MCPReadOnly=false exposes all 3 to admin, hides from normal JWT, checks annotations and schemas", func(t *testing.T) {
		stubA := &stubInboundsWriteRPCServer{}
		s, ts := setupServer(t, false, stubA, nil)

		adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})
		adminList, err := adminSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("admin ListTools failed: %v", err)
		}
		got := toolNames(adminList.Tools)
		want := []string{"add_inbound", "delete_inbound", "fast_add_inbound"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("admin tools mismatch: got %v want %v", got, want)
		}

		byName := make(map[string]*mcp.Tool, len(adminList.Tools))
		for _, tl := range adminList.Tools {
			byName[tl.Name] = tl
			if tl.Description == "" {
				t.Errorf("tool %s missing description", tl.Name)
			}
			if tl.Annotations == nil {
				t.Fatalf("tool %s missing annotations", tl.Name)
			}
			if tl.Annotations.ReadOnlyHint {
				t.Errorf("tool %s expected ReadOnlyHint=false, got true", tl.Name)
			}
			if tl.Annotations.DestructiveHint == nil {
				t.Fatalf("tool %s missing DestructiveHint", tl.Name)
			}
		}

		// DestructiveHint assertions.
		if !*byName["delete_inbound"].Annotations.DestructiveHint {
			t.Errorf("delete_inbound expected DestructiveHint=true")
		}
		if *byName["add_inbound"].Annotations.DestructiveHint {
			t.Errorf("add_inbound expected DestructiveHint=false")
		}
		if *byName["fast_add_inbound"].Annotations.DestructiveHint {
			t.Errorf("fast_add_inbound expected DestructiveHint=false")
		}

		// Inspect generated JSON schemas via go-sdk.
		type parsedSchema struct {
			Type       string `json:"type"`
			Properties map[string]struct {
				Type        any    `json:"type"`
				Description string `json:"description"`
			} `json:"properties"`
			Required []string `json:"required"`
		}
		parseToolSchema := func(t *testing.T, tl *mcp.Tool) parsedSchema {
			t.Helper()
			raw, err := json.Marshal(tl.InputSchema)
			if err != nil {
				t.Fatalf("marshal InputSchema for %s: %v", tl.Name, err)
			}
			var ps parsedSchema
			if err := json.Unmarshal(raw, &ps); err != nil {
				t.Fatalf("unmarshal InputSchema for %s: %v (raw=%s)", tl.Name, err, string(raw))
			}
			if ps.Type != "object" {
				t.Fatalf("tool %s schema type=%q, want 'object'", tl.Name, ps.Type)
			}
			return ps
		}

		// add_inbound schema
		addSchema := parseToolSchema(t, byName["add_inbound"])
		for _, field := range []string{"target", "bound_raw_string", "container"} {
			if p, ok := addSchema.Properties[field]; !ok || p.Description == "" {
				t.Errorf("add_inbound schema missing property or description for %q", field)
			}
		}
		if strings.Join(addSchema.Required, ",") != "bound_raw_string" {
			t.Errorf("add_inbound required=%v, want [bound_raw_string]", addSchema.Required)
		}

		// delete_inbound schema
		delSchema := parseToolSchema(t, byName["delete_inbound"])
		for _, field := range []string{"target", "container", "name"} {
			if p, ok := delSchema.Properties[field]; !ok || p.Description == "" {
				t.Errorf("delete_inbound schema missing property or description for %q", field)
			}
		}
		delReq := append([]string(nil), delSchema.Required...)
		sort.Strings(delReq)
		if strings.Join(delReq, ",") != "container,name" {
			t.Errorf("delete_inbound required=%v, want [container name]", delReq)
		}

		// fast_add_inbound schema — all 50 properties present with descriptions, none required.
		fastSchema := parseToolSchema(t, byName["fast_add_inbound"])
		expectedFastFields := []string{
			"target", "tag", "protocol", "stream", "transport", "domain", "is_xtls", "port",
			"self_signed", "container", "security", "reality_target", "reality_server_names",
			"reality_short_ids", "extra_params",
			"ws_path", "grpc_service_name", "http_path", "http_host",
			"httpupgrade_path", "httpupgrade_host", "xhttp_path", "xhttp_mode", "xhttp_host",
			"alpn", "flow", "sniffing_enabled", "skip_cert_verify",
			"plugin", "plugin_mode", "plugin_host", "plugin_path", "plugin_tls", "plugin_password", "plugin_version",
			"obfs", "obfs_password", "up", "down", "masquerade", "ignore_client_bandwidth",
			"congestion_controller", "udp_relay_mode", "zero_rtt_handshake", "heartbeat_interval", "disable_sni",
			"padding_scheme", "idle_session_check_interval_seconds", "idle_session_timeout_seconds", "min_idle_session",
			"inbound_type", "chain_uri", "access_tags", "remark",
		}
		if len(fastSchema.Properties) != len(expectedFastFields) {
			t.Fatalf("fast_add_inbound property count=%d, want %d", len(fastSchema.Properties), len(expectedFastFields))
		}
		for _, field := range expectedFastFields {
			p, ok := fastSchema.Properties[field]
			if !ok {
				t.Errorf("fast_add_inbound schema missing property %q", field)
				continue
			}
			if p.Description == "" {
				t.Errorf("fast_add_inbound schema property %q missing description", field)
			}
		}
		if len(fastSchema.Required) != 0 {
			t.Errorf("fast_add_inbound expected all fields optional, got required=%v", fastSchema.Required)
		}

		// Normal JWT user sees 0 tools and cannot invoke any of the 3.
		normalJWT := makeJWT(t, s, "alice", "normal")
		userSess := connectMCPClient(t, ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
		userList, err := userSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("normal user ListTools failed: %v", err)
		}
		if len(userList.Tools) != 0 {
			t.Fatalf("normal user expected 0 admin write tools, got %v", toolNames(userList.Tools))
		}
		for _, toolName := range want {
			if _, err := userSess.CallTool(ctx, &mcp.CallToolParams{Name: toolName}); err == nil {
				t.Fatalf("expected error when normal user calls admin tool %s", toolName)
			}
		}
		addC, delC, fastC := stubA.counts()
		if addC != 0 || delC != 0 || fastC != 0 {
			t.Fatalf("expected 0 RPC calls from blocked normal user, got add=%d del=%d fast=%d", addC, delC, fastC)
		}

		// Admin JWT also sees all 3 write tools.
		adminJWT := makeJWT(t, s, "boss", "admin")
		adminJWTSess := connectMCPClient(t, ts.URL, map[string]string{"Authorization": "Bearer " + adminJWT})
		adminJWTList, err := adminJWTSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("admin JWT ListTools failed: %v", err)
		}
		if strings.Join(toolNames(adminJWTList.Tools), ",") != strings.Join(want, ",") {
			t.Fatalf("admin JWT tools mismatch: got %v want %v", toolNames(adminJWTList.Tools), want)
		}
	})

	t.Run("MCP tool execution: success, validation IsError, no-node IsError, RPC IsError", func(t *testing.T) {
		stubA := &stubInboundsWriteRPCServer{}
		stubB := &stubInboundsWriteRPCServer{
			addInboundFn: func(ctx context.Context, req *proto.InboundOpReq) (*proto.InboundOpRsp, error) {
				return &proto.InboundOpRsp{Code: 601, Msg: "add failed on node-b"}, nil
			},
			deleteByNmFn: func(ctx context.Context, req *proto.DeleteInboundByNameReq) (*proto.InboundOpRsp, error) {
				return &proto.InboundOpRsp{Code: 300, Msg: "delete failed on node-b"}, nil
			},
			fastAddInbFn: func(ctx context.Context, req *proto.FastAddInboundReq) (*proto.FastAddInboundRsp, error) {
				return &proto.FastAddInboundRsp{Code: 1020, Msg: "fastadd failed on node-b"}, nil
			},
		}
		s, ts := setupServer(t, false, stubA, stubB)
		adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})

		// --- add_inbound ---
		resAdd, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_inbound",
			Arguments: map[string]any{"bound_raw_string": "eyJ0YWciOiJtY3AtYWRkIn0="},
		})
		if err != nil || resAdd.IsError {
			t.Fatalf("add_inbound failed: err=%v res=%+v", err, resAdd)
		}
		if !strings.Contains(extractText(t, resAdd), `"msg":"ok"`) {
			t.Fatalf("unexpected add_inbound output: %s", extractText(t, resAdd))
		}
		if got := stubA.lastAdd(); got == nil || got.GetInboundInfo() != "eyJ0YWciOiJtY3AtYWRkIn0=" {
			t.Fatalf("unexpected add_inbound RPC payload: %+v", got)
		}

		// add_inbound validation error -> IsError: true
		resAddBadContainer, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_inbound",
			Arguments: map[string]any{"bound_raw_string": "e30=", "container": "snell"},
		})
		if err != nil {
			t.Fatalf("expected tool error IsError=true, not protocol error: %v", err)
		}
		if !resAddBadContainer.IsError || !strings.Contains(extractText(t, resAddBadContainer), `container "snell" not supported`) {
			t.Fatalf("expected IsError with unsupported container, got isError=%v text=%q", resAddBadContainer.IsError, extractText(t, resAddBadContainer))
		}

		// add_inbound no node -> IsError: true
		resAddNoNode, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_inbound",
			Arguments: map[string]any{"target": "nonexistent", "bound_raw_string": "e30="},
		})
		if err != nil || !resAddNoNode.IsError || !strings.Contains(extractText(t, resAddNoNode), "no available node") {
			t.Fatalf("expected IsError 'no available node', err=%v res=%+v", err, resAddNoNode)
		}

		// add_inbound RPC error -> IsError: true
		resAddRPCErr, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_inbound",
			Arguments: map[string]any{"target": "node-b", "bound_raw_string": "e30="},
		})
		if err != nil || !resAddRPCErr.IsError || !strings.Contains(extractText(t, resAddRPCErr), "add failed on node-b") {
			t.Fatalf("expected IsError 'add failed on node-b', err=%v res=%+v", err, resAddRPCErr)
		}

		// --- delete_inbound ---
		resDel, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "delete_inbound",
			Arguments: map[string]any{"container": "xray", "name": "mcp-del"},
		})
		if err != nil || resDel.IsError {
			t.Fatalf("delete_inbound failed: err=%v res=%+v", err, resDel)
		}
		if got := stubA.lastDelete(); got == nil || got.GetContainer() != "xray" || got.GetName() != "mcp-del" {
			t.Fatalf("unexpected delete_inbound RPC payload: %+v", got)
		}

		// delete_inbound empty container -> IsError: true
		resDelNoContainer, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "delete_inbound",
			Arguments: map[string]any{"container": "", "name": "mcp-del"},
		})
		if err != nil || !resDelNoContainer.IsError || !strings.Contains(extractText(t, resDelNoContainer), "container is required") {
			t.Fatalf("expected IsError 'container is required', err=%v res=%+v", err, resDelNoContainer)
		}

		// delete_inbound empty name -> IsError: true
		resDelNoName, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "delete_inbound",
			Arguments: map[string]any{"container": "xray", "name": ""},
		})
		if err != nil || !resDelNoName.IsError || !strings.Contains(extractText(t, resDelNoName), "name is required") {
			t.Fatalf("expected IsError 'name is required', err=%v res=%+v", err, resDelNoName)
		}

		// delete_inbound no node -> IsError: true
		resDelNoNode, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "delete_inbound",
			Arguments: map[string]any{"target": "nonexistent", "container": "xray", "name": "mcp-del"},
		})
		if err != nil || !resDelNoNode.IsError || !strings.Contains(extractText(t, resDelNoNode), "no available node") {
			t.Fatalf("expected IsError 'no available node', err=%v res=%+v", err, resDelNoNode)
		}

		// delete_inbound RPC error -> IsError: true
		resDelRPCErr, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "delete_inbound",
			Arguments: map[string]any{"target": "node-b", "container": "xray", "name": "mcp-del"},
		})
		if err != nil || !resDelRPCErr.IsError || !strings.Contains(extractText(t, resDelRPCErr), "delete failed on node-b") {
			t.Fatalf("expected IsError 'delete failed on node-b', err=%v res=%+v", err, resDelRPCErr)
		}

		// --- fast_add_inbound ---
		// Minimal arguments succeed (all fields optional).
		resFastMin, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "fast_add_inbound",
			Arguments: map[string]any{"tag": "mcp-vless-min"},
		})
		if err != nil || resFastMin.IsError {
			t.Fatalf("fast_add_inbound minimal failed: err=%v res=%+v", err, resFastMin)
		}
		if got := stubA.lastFastAdd(); got == nil || got.GetTag() != "mcp-vless-min" ||
			got.GetInboundBuilderType() != proto.BuilderType_VLESSSettingBuilderType || got.GetTransport() != "tcp" {
			t.Fatalf("unexpected fast_add_inbound minimal RPC payload: %+v", got)
		}

		// Hysteria2 convenience args via MCP.
		resFastHy2, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name: "fast_add_inbound",
			Arguments: map[string]any{
				"tag":                     "mcp-hy2",
				"protocol":                "hysteria2",
				"container":               "mihomo",
				"obfs":                    "salamander",
				"obfs_password":           "pw123",
				"up":                      "20 Mbps",
				"down":                    "50 Mbps",
				"ignore_client_bandwidth": true,
			},
		})
		if err != nil || resFastHy2.IsError {
			t.Fatalf("fast_add_inbound hysteria2 failed: err=%v res=%+v", err, resFastHy2)
		}
		if got := stubA.lastFastAdd(); got == nil || got.GetInboundBuilderType() != proto.BuilderType_Hysteria2SettingBuilderType ||
			got.GetTransport() != "" || got.GetExtraParams()["obfs"] != "salamander" || got.GetExtraParams()["ignore_client_bandwidth"] != "true" {
			t.Fatalf("unexpected fast_add_inbound hy2 RPC payload: %+v", got)
		}

		// fast_add_inbound unsupported protocol -> IsError: true
		resFastBadProto, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "fast_add_inbound",
			Arguments: map[string]any{"protocol": "invalidproto"},
		})
		if err != nil || !resFastBadProto.IsError || !strings.Contains(extractText(t, resFastBadProto), "unsupported protocol: invalidproto") {
			t.Fatalf("expected IsError 'unsupported protocol: invalidproto', err=%v res=%+v", err, resFastBadProto)
		}

		// fast_add_inbound unsupported transport h3 -> IsError: true
		resFastH3, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "fast_add_inbound",
			Arguments: map[string]any{"protocol": "vless", "transport": "h3"},
		})
		if err != nil || !resFastH3.IsError || !strings.Contains(extractText(t, resFastH3), `transport "h3" is not supported`) {
			t.Fatalf("expected IsError for transport h3, err=%v res=%+v", err, resFastH3)
		}

		// fast_add_inbound no node -> IsError: true
		resFastNoNode, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "fast_add_inbound",
			Arguments: map[string]any{"target": "nonexistent", "protocol": "vless"},
		})
		if err != nil || !resFastNoNode.IsError || !strings.Contains(extractText(t, resFastNoNode), "no available node") {
			t.Fatalf("expected IsError 'no available node', err=%v res=%+v", err, resFastNoNode)
		}

		// fast_add_inbound RPC error -> IsError: true
		resFastRPCErr, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "fast_add_inbound",
			Arguments: map[string]any{"target": "node-b", "protocol": "vless"},
		})
		if err != nil || !resFastRPCErr.IsError || !strings.Contains(extractText(t, resFastRPCErr), "fastadd failed on node-b") {
			t.Fatalf("expected IsError 'fastadd failed on node-b', err=%v res=%+v", err, resFastRPCErr)
		}
	})
}
