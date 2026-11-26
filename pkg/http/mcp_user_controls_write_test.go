package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	commonrpc "github.com/lureiny/v2raymg/pkg/common/rpc"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
)

const userControlsWriteClusterToken = "user-controls-write-cluster-token-123456"

type userControlsFakeRPCServer struct {
	proto.UnimplementedEndNodeAccessServer

	mu                 sync.Mutex
	nodeName           string
	updateUsersErr     string
	updateUsersRPCFail error
	updatedUsers       []*proto.User
	rotateInboundRsp   *proto.RotateInboundPortRsp
	rotateInboundRPC   error
	rotateInboundReqs  []*proto.RotateInboundPortReq
	rotateAllRsp       *proto.RotateAllPortsRsp
	rotateAllRPC       error
	rotateAllReqs      []*proto.RotateAllPortsReq
}

func (f *userControlsFakeRPCServer) UpdateUsers(_ context.Context, req *proto.UserOpReq) (*proto.UserOpRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range req.GetUsers() {
		cloned := &proto.User{
			Name:                  u.GetName(),
			Passwd:                u.GetPasswd(),
			Role:                  u.GetRole(),
			UploadBps:             u.GetUploadBps(),
			DownloadBps:           u.GetDownloadBps(),
			MaxClients:            u.GetMaxClients(),
			ClientRecycleDelaySec: u.GetClientRecycleDelaySec(),
			ClientDrainSec:        u.GetClientDrainSec(),
		}
		f.updatedUsers = append(f.updatedUsers, cloned)
	}
	if f.updateUsersRPCFail != nil {
		return nil, f.updateUsersRPCFail
	}
	if f.updateUsersErr != "" {
		return &proto.UserOpRsp{Code: 500, Msg: f.updateUsersErr}, nil
	}
	return &proto.UserOpRsp{Code: 0, Msg: "ok"}, nil
}

func (f *userControlsFakeRPCServer) RotateInboundPort(_ context.Context, req *proto.RotateInboundPortReq) (*proto.RotateInboundPortRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rotateInboundReqs = append(f.rotateInboundReqs, &proto.RotateInboundPortReq{
		Username:  req.GetUsername(),
		Container: req.GetContainer(),
		Inbound:   req.GetInbound(),
		Port:      req.GetPort(),
	})
	if f.rotateInboundRPC != nil {
		return nil, f.rotateInboundRPC
	}
	if f.rotateInboundRsp != nil {
		return f.rotateInboundRsp, nil
	}
	return &proto.RotateInboundPortRsp{Code: 0, Port: 24680, Msg: "ok"}, nil
}

func (f *userControlsFakeRPCServer) RotateAllPorts(_ context.Context, req *proto.RotateAllPortsReq) (*proto.RotateAllPortsRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rotateAllReqs = append(f.rotateAllReqs, &proto.RotateAllPortsReq{
		Username: req.GetUsername(),
	})
	if f.rotateAllRPC != nil {
		return nil, f.rotateAllRPC
	}
	if f.rotateAllRsp != nil {
		return f.rotateAllRsp, nil
	}
	return &proto.RotateAllPortsRsp{
		Code: 0,
		Ports: map[string]uint32{
			"xray:vless-in":   25001,
			"hysteria:hy2-in": 25002,
		},
		Msg: "ok",
	}, nil
}

func (f *userControlsFakeRPCServer) lastUpdatedUser() *proto.User {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.updatedUsers) == 0 {
		return nil
	}
	return f.updatedUsers[len(f.updatedUsers)-1]
}

func (f *userControlsFakeRPCServer) updatedUserCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.updatedUsers)
}

func (f *userControlsFakeRPCServer) lastRotateInboundReq() *proto.RotateInboundPortReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rotateInboundReqs) == 0 {
		return nil
	}
	return f.rotateInboundReqs[len(f.rotateInboundReqs)-1]
}

func (f *userControlsFakeRPCServer) lastRotateAllReq() *proto.RotateAllPortsReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.rotateAllReqs) == 0 {
		return nil
	}
	return f.rotateAllReqs[len(f.rotateAllReqs)-1]
}

type userControlsClusterNodes struct {
	nodes []*cluster.Node
	token string
}

func (c *userControlsClusterNodes) GetNodesWithFilter(f cluster.NodeFilter) []*cluster.Node {
	var out []*cluster.Node
	for _, n := range c.nodes {
		if f(n) {
			out = append(out, n)
		}
	}
	return out
}

func (c *userControlsClusterNodes) GetClusterToken() string { return c.token }

func startUserControlsFakeNode(t *testing.T, name string, srv *userControlsFakeRPCServer) *cluster.Node {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake grpc node %s: %v", name, err)
	}
	gs := grpc.NewServer(grpc.ForceServerCodec(commonrpc.NewEncryptMessageCodec(userControlsWriteClusterToken)))
	proto.RegisterEndNodeAccessServer(gs, srv)
	go func() {
		_ = gs.Serve(lis)
	}()

	addr := lis.Addr().(*net.TCPAddr)
	now := time.Now().Unix()
	node := &cluster.Node{
		Node: &proto.Node{
			Name:   name,
			Host:   "127.0.0.1",
			Port:   int32(addr.Port),
			NodeId: name + "-id",
		},
		CreateTime: now,
	}
	node.SetOutToken("test-out-token")
	node.SetReportHeartBeatTime(now)
	node.SetRecvHeartBeatTime(now)

	t.Cleanup(func() {
		node.CloseConn()
		gs.Stop()
		_ = lis.Close()
	})
	return node
}

type userControlsWriteFixture struct {
	server *HttpServer
	ts     *httptest.Server
	rpcA   *userControlsFakeRPCServer
	rpcB   *userControlsFakeRPCServer
}

func setupUserControlsWriteFixture(t *testing.T, readOnly bool) *userControlsWriteFixture {
	t.Helper()

	aliceHash, err := auth.HashLoginPassword("alice-old-pass")
	if err != nil {
		t.Fatalf("hash alice password: %v", err)
	}
	bossHash, err := auth.HashLoginPassword("boss-old-pass")
	if err != nil {
		t.Fatalf("hash boss password: %v", err)
	}

	ul := newMockUserLister(
		&contracts.User{Username: "alice", Role: "normal", LoginPassword: aliceHash},
		&contracts.User{Username: "bob", Role: "normal"},
		&contracts.User{Username: "boss", Role: "admin", LoginPassword: bossHash},
	)

	rpcA := &userControlsFakeRPCServer{nodeName: "node-a"}
	rpcB := &userControlsFakeRPCServer{nodeName: "node-b"}
	nodeA := startUserControlsFakeNode(t, "node-a", rpcA)
	nodeB := startUserControlsFakeNode(t, "node-b", rpcB)

	s := newTestHttpServer(ul)
	s.Name = "node-a"
	s.MCPReadOnly = readOnly
	s.localNode = &cluster.LocalNode{
		Node: proto.Node{
			Name:   "node-a",
			Host:   "127.0.0.1",
			Port:   19999,
			NodeId: "node-a-id",
		},
	}
	s.clusterNodes = &userControlsClusterNodes{
		nodes: []*cluster.Node{nodeA, nodeB},
		token: userControlsWriteClusterToken,
	}

	userGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret))
	registerEndpoint(s, userGroup, s.rotateInboundPortEndpoint())
	registerEndpoint(s, userGroup, s.rotateAllPortsEndpoint())
	registerEndpoint(s, userGroup, s.changePasswordEndpoint())

	adminGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret), auth.AdminOnly())
	registerEndpoint(s, adminGroup, s.setUserRoleEndpoint())
	registerEndpoint(s, adminGroup, s.setUserBandwidthEndpoint())
	registerEndpoint(s, adminGroup, s.setUserClientLimitEndpoint())

	RegisterMCP(s)

	ts := httptest.NewServer(s.RestfulServer)
	t.Cleanup(ts.Close)

	return &userControlsWriteFixture{
		server: s,
		ts:     ts,
		rpcA:   rpcA,
		rpcB:   rpcB,
	}
}

func doUserControlsHTTP(t *testing.T, client *http.Client, method, fullURL string, body io.Reader, contentType string, headers map[string]string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, fullURL, body)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, fullURL, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, fullURL, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp.StatusCode, raw
}

func TestUserControlsWrite_MCPVisibilityAndAnnotations(t *testing.T) {
	ctx := context.Background()

	t.Run("MCPReadOnly=true hides all 6 write tools from both admin and normal user", func(t *testing.T) {
		fix := setupUserControlsWriteFixture(t, true)
		normalJWT := makeJWT(t, fix.server, "alice", "normal")
		adminJWT := makeJWT(t, fix.server, "boss", "admin")

		for _, tc := range []struct {
			name    string
			headers map[string]string
		}{
			{"X-Token", map[string]string{"X-Token": fix.server.token}},
			{"admin JWT", map[string]string{"Authorization": "Bearer " + adminJWT}},
			{"normal JWT", map[string]string{"Authorization": "Bearer " + normalJWT}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				sess := connectMCPClient(t, fix.ts.URL, tc.headers)
				res, err := sess.ListTools(ctx, &mcp.ListToolsParams{})
				if err != nil {
					t.Fatalf("ListTools failed: %v", err)
				}
				if len(res.Tools) != 0 {
					t.Fatalf("expected 0 tools when MCPReadOnly=true, got %v", toolNames(res.Tools))
				}
			})
		}
	})

	t.Run("MCPReadOnly=false exposes all 6 to admin and only 3 ScopeUser tools to normal JWT with exact annotations", func(t *testing.T) {
		fix := setupUserControlsWriteFixture(t, false)
		normalJWT := makeJWT(t, fix.server, "alice", "normal")
		adminJWT := makeJWT(t, fix.server, "boss", "admin")

		wantDestructive := map[string]bool{
			"set_user_role":         false,
			"set_user_bandwidth":    false,
			"set_user_client_limit": false,
			"rotate_inbound_port":   false,
			"rotate_all_ports":      true,
			"change_password":       true,
		}

		// Both X-Token and admin JWT see all 6 tools.
		for _, tc := range []struct {
			name    string
			headers map[string]string
		}{
			{"X-Token", map[string]string{"X-Token": fix.server.token}},
			{"admin JWT", map[string]string{"Authorization": "Bearer " + adminJWT}},
		} {
			sess := connectMCPClient(t, fix.ts.URL, tc.headers)
			res, err := sess.ListTools(ctx, &mcp.ListToolsParams{})
			if err != nil {
				t.Fatalf("%s ListTools failed: %v", tc.name, err)
			}
			wantAdmin := "change_password,rotate_all_ports,rotate_inbound_port,set_user_bandwidth,set_user_client_limit,set_user_role"
			if got := strings.Join(toolNames(res.Tools), ","); got != wantAdmin {
				t.Fatalf("%s tools mismatch: got %q, want %q", tc.name, got, wantAdmin)
			}
			for _, tl := range res.Tools {
				if tl.Annotations == nil {
					t.Fatalf("tool %q missing annotations", tl.Name)
				}
				if tl.Annotations.ReadOnlyHint {
					t.Errorf("tool %q must have ReadOnlyHint=false", tl.Name)
				}
				if tl.Annotations.DestructiveHint == nil {
					t.Fatalf("tool %q missing DestructiveHint", tl.Name)
				}
				if *tl.Annotations.DestructiveHint != wantDestructive[tl.Name] {
					t.Errorf("tool %q DestructiveHint=%v, want %v", tl.Name, *tl.Annotations.DestructiveHint, wantDestructive[tl.Name])
				}
			}
		}

		// Normal JWT sees ONLY the 3 ScopeUser tools.
		userSess := connectMCPClient(t, fix.ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
		userRes, err := userSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("normal user ListTools failed: %v", err)
		}
		wantUser := "change_password,rotate_all_ports,rotate_inbound_port"
		if got := strings.Join(toolNames(userRes.Tools), ","); got != wantUser {
			t.Fatalf("normal user tools mismatch: got %q, want %q", got, wantUser)
		}
		for _, tl := range userRes.Tools {
			if tl.Annotations == nil || tl.Annotations.ReadOnlyHint {
				t.Errorf("user tool %q expected ReadOnlyHint=false, got %+v", tl.Name, tl.Annotations)
			}
			if tl.Annotations == nil || tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint != wantDestructive[tl.Name] {
				t.Errorf("user tool %q DestructiveHint mismatch: got %+v, want %v", tl.Name, tl.Annotations, wantDestructive[tl.Name])
			}
		}

		// Normal user calling a ScopeAdmin tool must fail.
		callRes, callErr := userSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_user_role",
			Arguments: map[string]any{"name": "alice", "role": "admin"},
		})
		if callErr == nil && !callRes.IsError {
			t.Fatalf("expected error when normal user calls admin tool set_user_role, got success: %+v", callRes)
		}
	})
}

func TestSetUserRoleEndpoint_HTTPAndMCP(t *testing.T) {
	fix := setupUserControlsWriteFixture(t, false)
	ctx := context.Background()
	adminHeaders := map[string]string{"X-Token": fix.server.token}
	adminSess := connectMCPClient(t, fix.ts.URL, adminHeaders)

	t.Run("HTTP PUT success, path param overrides body name, default target is local node", func(t *testing.T) {
		code, raw := doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/user/alice/role",
			bytes.NewBufferString(`{"name":"ignored","role":"admin"}`), "application/json", adminHeaders)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", code, string(raw))
		}
		var body struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if body.Code != 0 || body.Msg != "ok" {
			t.Fatalf("expected {code:0 msg:ok}, got %+v", body)
		}
		u := fix.rpcA.lastUpdatedUser()
		if u == nil || u.GetName() != "alice" || u.GetRole() != "admin" {
			t.Fatalf("expected node-a to receive User{Name:alice, Role:admin}, got %+v", u)
		}
		if fix.rpcB.updatedUserCount() != 0 {
			t.Fatalf("expected node-b not called for default local target, got %d calls", fix.rpcB.updatedUserCount())
		}
	})

	t.Run("HTTP validation errors and legacy HTTP 200 code=404 no available node", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			body     string
			wantHTTP int
			wantCode int
			wantSub  string
		}{
			{"invalid JSON", `{"role":`, http.StatusBadRequest, 400, "invalid request body"},
			{"missing role", `{}`, http.StatusBadRequest, 400, "role required"},
			{"invalid role", `{"role":"superuser"}`, http.StatusBadRequest, 400, "role must be 'admin' or 'normal'"},
			{"no available node", `{"role":"normal","target":"nonexistent"}`, http.StatusOK, 404, "no available node"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				code, raw := doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/user/alice/role",
					bytes.NewBufferString(tc.body), "application/json", adminHeaders)
				if code != tc.wantHTTP {
					t.Fatalf("expected HTTP %d, got %d body=%s", tc.wantHTTP, code, string(raw))
				}
				var resp struct {
					Code int    `json:"code"`
					Msg  string `json:"msg"`
				}
				if err := json.Unmarshal(raw, &resp); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if resp.Code != tc.wantCode || !strings.Contains(resp.Msg, tc.wantSub) {
					t.Fatalf("expected code=%d msg containing %q, got %+v", tc.wantCode, tc.wantSub, resp)
				}
			})
		}
	})

	t.Run("HTTP RPC failure returns 500", func(t *testing.T) {
		fix.rpcB.mu.Lock()
		fix.rpcB.updateUsersErr = "db locked"
		fix.rpcB.mu.Unlock()
		defer func() {
			fix.rpcB.mu.Lock()
			fix.rpcB.updateUsersErr = ""
			fix.rpcB.mu.Unlock()
		}()

		code, raw := doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/user/alice/role",
			bytes.NewBufferString(`{"role":"normal","target":"node-b"}`), "application/json", adminHeaders)
		if code != http.StatusInternalServerError || !strings.Contains(string(raw), "db locked") {
			t.Fatalf("expected 500 with 'db locked', got %d body=%s", code, string(raw))
		}
	})

	t.Run("MCP set_user_role success and NewBusinessErr maps to IsError=true", func(t *testing.T) {
		res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_user_role",
			Arguments: map[string]any{"name": "bob", "role": "normal", "target": "node-b"},
		})
		if err != nil {
			t.Fatalf("MCP set_user_role failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected IsError: %s", extractText(t, res))
		}
		u := fix.rpcB.lastUpdatedUser()
		if u == nil || u.GetName() != "bob" || u.GetRole() != "normal" {
			t.Fatalf("expected node-b User{Name:bob, Role:normal}, got %+v", u)
		}

		// Legacy HTTP 200 + code=404 MUST become IsError=true in MCP.
		noNodeRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_user_role",
			Arguments: map[string]any{"name": "bob", "role": "admin", "target": "missing-node"},
		})
		if err != nil {
			t.Fatalf("expected MCP tool error, got protocol error: %v", err)
		}
		if !noNodeRes.IsError || !strings.Contains(extractText(t, noNodeRes), "no available node") {
			t.Fatalf("expected IsError=true with 'no available node', got %+v text=%q", noNodeRes, extractText(t, noNodeRes))
		}

		badRoleRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_user_role",
			Arguments: map[string]any{"name": "bob", "role": "owner"},
		})
		if err != nil {
			t.Fatalf("expected MCP tool error, got protocol error: %v", err)
		}
		if !badRoleRes.IsError || !strings.Contains(extractText(t, badRoleRes), "role must be") {
			t.Fatalf("expected IsError=true for invalid role, got %+v", badRoleRes)
		}
	})
}

func TestSetUserBandwidthEndpoint_HTTPAndMCP(t *testing.T) {
	fix := setupUserControlsWriteFixture(t, false)
	ctx := context.Background()
	adminHeaders := map[string]string{"X-Token": fix.server.token}
	adminSess := connectMCPClient(t, fix.ts.URL, adminHeaders)

	t.Run("HTTP pointer zero maps to proto -1 while omitted maps to proto 0", func(t *testing.T) {
		// Explicit upload_bps=0, download_bps omitted -> UploadBps=-1, DownloadBps=0.
		code, raw := doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/user/alice/bandwidth",
			bytes.NewBufferString(`{"upload_bps":0}`), "application/json", adminHeaders)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", code, string(raw))
		}
		u := fix.rpcA.lastUpdatedUser()
		if u == nil || u.GetName() != "alice" || u.GetUploadBps() != -1 || u.GetDownloadBps() != 0 {
			t.Fatalf("expected UploadBps=-1 DownloadBps=0, got %+v", u)
		}

		// Explicit positive upload & zero download -> UploadBps=1000000, DownloadBps=-1.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/user/alice/bandwidth",
			bytes.NewBufferString(`{"upload_bps":1000000,"download_bps":0}`), "application/json", adminHeaders)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", code, string(raw))
		}
		u = fix.rpcA.lastUpdatedUser()
		if u == nil || u.GetUploadBps() != 1000000 || u.GetDownloadBps() != -1 {
			t.Fatalf("expected UploadBps=1000000 DownloadBps=-1, got %+v", u)
		}
	})

	t.Run("HTTP validation errors and legacy HTTP 200 code=404", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			body     string
			wantHTTP int
			wantCode int
			wantSub  string
		}{
			{"neither field", `{}`, http.StatusBadRequest, 400, "at least one"},
			{"negative upload", `{"upload_bps":-1}`, http.StatusBadRequest, 400, "upload_bps must be >= 0"},
			{"negative download", `{"download_bps":-5}`, http.StatusBadRequest, 400, "download_bps must be >= 0"},
			{"no available node", `{"upload_bps":100,"target":"missing"}`, http.StatusOK, 404, "no available node"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				code, raw := doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/user/alice/bandwidth",
					bytes.NewBufferString(tc.body), "application/json", adminHeaders)
				if code != tc.wantHTTP {
					t.Fatalf("expected HTTP %d, got %d body=%s", tc.wantHTTP, code, string(raw))
				}
				var resp struct {
					Code int    `json:"code"`
					Msg  string `json:"msg"`
				}
				if err := json.Unmarshal(raw, &resp); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if resp.Code != tc.wantCode || !strings.Contains(resp.Msg, tc.wantSub) {
					t.Fatalf("expected code=%d msg containing %q, got %+v", tc.wantCode, tc.wantSub, resp)
				}
			})
		}
	})

	t.Run("MCP set_user_bandwidth pointer 0 vs omitted and IsError on business error", func(t *testing.T) {
		res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_user_bandwidth",
			Arguments: map[string]any{"name": "alice", "download_bps": 0},
		})
		if err != nil {
			t.Fatalf("MCP set_user_bandwidth failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected IsError: %s", extractText(t, res))
		}
		u := fix.rpcA.lastUpdatedUser()
		if u == nil || u.GetUploadBps() != 0 || u.GetDownloadBps() != -1 {
			t.Fatalf("expected MCP UploadBps=0 DownloadBps=-1, got %+v", u)
		}

		noNodeRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_user_bandwidth",
			Arguments: map[string]any{"name": "alice", "upload_bps": 500000, "target": "no-such-node"},
		})
		if err != nil {
			t.Fatalf("expected MCP tool error, got protocol error: %v", err)
		}
		if !noNodeRes.IsError || !strings.Contains(extractText(t, noNodeRes), "no available node") {
			t.Fatalf("expected IsError=true with 'no available node', got %+v", noNodeRes)
		}
	})
}

func TestSetUserClientLimitEndpoint_HTTPAndMCP(t *testing.T) {
	fix := setupUserControlsWriteFixture(t, false)
	ctx := context.Background()
	adminHeaders := map[string]string{"X-Token": fix.server.token}
	adminSess := connectMCPClient(t, fix.ts.URL, adminHeaders)

	t.Run("HTTP max_clients=0 maps to -1 and optional fields stay 0 when omitted", func(t *testing.T) {
		code, raw := doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/user/alice/client-limit",
			bytes.NewBufferString(`{"max_clients":0}`), "application/json", adminHeaders)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", code, string(raw))
		}
		u := fix.rpcA.lastUpdatedUser()
		if u == nil || u.GetName() != "alice" || u.GetMaxClients() != -1 || u.GetClientRecycleDelaySec() != 0 || u.GetClientDrainSec() != 0 {
			t.Fatalf("expected MaxClients=-1 Recycle=0 Drain=0, got %+v", u)
		}

		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/user/alice/client-limit",
			bytes.NewBufferString(`{"max_clients":3,"recycle_delay_sec":60,"drain_sec":2}`), "application/json", adminHeaders)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", code, string(raw))
		}
		u = fix.rpcA.lastUpdatedUser()
		if u == nil || u.GetMaxClients() != 3 || u.GetClientRecycleDelaySec() != 60 || u.GetClientDrainSec() != 2 {
			t.Fatalf("expected MaxClients=3 Recycle=60 Drain=2, got %+v", u)
		}
	})

	t.Run("HTTP validation errors and legacy HTTP 200 code=404", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			body     string
			wantHTTP int
			wantCode int
			wantSub  string
		}{
			{"missing max_clients", `{"recycle_delay_sec":30}`, http.StatusBadRequest, 400, "max_clients required"},
			{"negative max_clients", `{"max_clients":-1}`, http.StatusBadRequest, 400, "max_clients must be >= 0"},
			{"negative recycle_delay", `{"max_clients":2,"recycle_delay_sec":-1}`, http.StatusBadRequest, 400, "recycle_delay_sec must be >= 0"},
			{"negative drain", `{"max_clients":2,"drain_sec":-1}`, http.StatusBadRequest, 400, "drain_sec must be >= 0"},
			{"no available node", `{"max_clients":2,"target":"missing"}`, http.StatusOK, 404, "no available node"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				code, raw := doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/user/alice/client-limit",
					bytes.NewBufferString(tc.body), "application/json", adminHeaders)
				if code != tc.wantHTTP {
					t.Fatalf("expected HTTP %d, got %d body=%s", tc.wantHTTP, code, string(raw))
				}
				var resp struct {
					Code int    `json:"code"`
					Msg  string `json:"msg"`
				}
				if err := json.Unmarshal(raw, &resp); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if resp.Code != tc.wantCode || !strings.Contains(resp.Msg, tc.wantSub) {
					t.Fatalf("expected code=%d msg containing %q, got %+v", tc.wantCode, tc.wantSub, resp)
				}
			})
		}
	})

	t.Run("MCP set_user_client_limit explicit 0 vs positive and IsError paths", func(t *testing.T) {
		res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_user_client_limit",
			Arguments: map[string]any{"name": "alice", "max_clients": 0},
		})
		if err != nil {
			t.Fatalf("MCP set_user_client_limit(0) failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected IsError: %s", extractText(t, res))
		}
		u := fix.rpcA.lastUpdatedUser()
		if u == nil || u.GetMaxClients() != -1 || u.GetClientRecycleDelaySec() != 0 || u.GetClientDrainSec() != 0 {
			t.Fatalf("expected MCP MaxClients=-1 Recycle=0 Drain=0, got %+v", u)
		}

		res2, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name: "set_user_client_limit",
			Arguments: map[string]any{
				"name":              "alice",
				"max_clients":       4,
				"recycle_delay_sec": 45,
				"drain_sec":         5,
			},
		})
		if err != nil || res2.IsError {
			t.Fatalf("MCP set_user_client_limit(4) failed: err=%v res=%+v", err, res2)
		}
		u = fix.rpcA.lastUpdatedUser()
		if u == nil || u.GetMaxClients() != 4 || u.GetClientRecycleDelaySec() != 45 || u.GetClientDrainSec() != 5 {
			t.Fatalf("expected MCP MaxClients=4 Recycle=45 Drain=5, got %+v", u)
		}

		noNodeRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "set_user_client_limit",
			Arguments: map[string]any{"name": "alice", "max_clients": 2, "target": "missing"},
		})
		if err != nil || !noNodeRes.IsError || !strings.Contains(extractText(t, noNodeRes), "no available node") {
			t.Fatalf("expected MCP IsError 'no available node', err=%v res=%+v", err, noNodeRes)
		}
	})
}

func TestRotateInboundPortEndpoint_HTTPAndMCP(t *testing.T) {
	fix := setupUserControlsWriteFixture(t, false)
	ctx := context.Background()
	normalJWT := makeJWT(t, fix.server, "alice", "normal")
	adminJWT := makeJWT(t, fix.server, "boss", "admin")

	normalHeaders := map[string]string{"Authorization": "Bearer " + normalJWT}
	adminJWTHeaders := map[string]string{"Authorization": "Bearer " + adminJWT}
	xTokenHeaders := map[string]string{"X-Token": fix.server.token}

	normalSess := connectMCPClient(t, fix.ts.URL, normalHeaders)
	adminJWTSess := connectMCPClient(t, fix.ts.URL, adminJWTHeaders)
	xTokenSess := connectMCPClient(t, fix.ts.URL, xTokenHeaders)

	t.Run("HTTP JSON and form binding preserve caller auth rules and response shape", func(t *testing.T) {
		// Normal JWT rotating own inbound via JSON.
		code, raw := doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateInboundPort",
			bytes.NewBufferString(`{"container":"xray","inbound":"vless-in","port":0}`), "application/json", normalHeaders)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", code, string(raw))
		}
		var rsp RotateInboundPortResp
		if err := json.Unmarshal(raw, &rsp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if rsp.Code != 0 || rsp.Port != 24680 || rsp.Msg != "ok" {
			t.Fatalf("unexpected rotate response: %+v", rsp)
		}
		rpcReq := fix.rpcA.lastRotateInboundReq()
		if rpcReq == nil || rpcReq.GetUsername() != "alice" || rpcReq.GetContainer() != "xray" || rpcReq.GetInbound() != "vless-in" || rpcReq.GetPort() != 0 {
			t.Fatalf("unexpected RPC req: %+v", rpcReq)
		}

		// Form-urlencoded binding with explicit port and target.
		form := url.Values{}
		form.Set("target", "node-b")
		form.Set("username", "alice")
		form.Set("container", "hysteria")
		form.Set("inbound", "hy2-in")
		form.Set("port", "31234")
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateInboundPort",
			strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", xTokenHeaders)
		if code != http.StatusOK {
			t.Fatalf("form binding expected 200, got %d body=%s", code, string(raw))
		}
		rpcReqB := fix.rpcB.lastRotateInboundReq()
		if rpcReqB == nil || rpcReqB.GetUsername() != "alice" || rpcReqB.GetContainer() != "hysteria" || rpcReqB.GetInbound() != "hy2-in" || rpcReqB.GetPort() != 31234 {
			t.Fatalf("unexpected form RPC req on node-b: %+v", rpcReqB)
		}

		// Admin JWT optional username defaults to self ("boss").
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateInboundPort",
			bytes.NewBufferString(`{"container":"xray","inbound":"vless-in"}`), "application/json", adminJWTHeaders)
		if code != http.StatusOK {
			t.Fatalf("admin JWT self expected 200, got %d body=%s", code, string(raw))
		}
		if got := fix.rpcA.lastRotateInboundReq().GetUsername(); got != "boss" {
			t.Fatalf("expected admin JWT default target user 'boss', got %q", got)
		}

		// Normal JWT targeting another user -> 403.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateInboundPort",
			bytes.NewBufferString(`{"username":"bob","container":"xray","inbound":"vless-in"}`), "application/json", normalHeaders)
		if code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%s", code, string(raw))
		}

		// X-Token missing username -> 400.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateInboundPort",
			bytes.NewBufferString(`{"container":"xray","inbound":"vless-in"}`), "application/json", xTokenHeaders)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400 for X-Token without username, got %d body=%s", code, string(raw))
		}

		// No available node -> 502.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateInboundPort",
			bytes.NewBufferString(`{"target":"missing","container":"xray","inbound":"vless-in"}`), "application/json", normalHeaders)
		if code != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d body=%s", code, string(raw))
		}

		// RPC transport error -> 500.
		fix.rpcB.mu.Lock()
		fix.rpcB.rotateInboundRPC = errors.New("connection reset")
		fix.rpcB.mu.Unlock()
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateInboundPort",
			bytes.NewBufferString(`{"target":"node-b","container":"xray","inbound":"vless-in"}`), "application/json", normalHeaders)
		fix.rpcB.mu.Lock()
		fix.rpcB.rotateInboundRPC = nil
		fix.rpcB.mu.Unlock()
		if code != http.StatusInternalServerError || !strings.Contains(string(raw), "connection reset") {
			t.Fatalf("expected 500 with transport error, got %d body=%s", code, string(raw))
		}

		// Structured non-zero business response from node -> HTTP 200 preserving code/port/msg.
		fix.rpcA.mu.Lock()
		fix.rpcA.rotateInboundRsp = &proto.RotateInboundPortRsp{Code: 300, Port: 0, Msg: "port in use"}
		fix.rpcA.mu.Unlock()
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateInboundPort",
			bytes.NewBufferString(`{"container":"xray","inbound":"vless-in","port":80}`), "application/json", normalHeaders)
		fix.rpcA.mu.Lock()
		fix.rpcA.rotateInboundRsp = nil
		fix.rpcA.mu.Unlock()
		if code != http.StatusOK {
			t.Fatalf("expected 200 with structured node code, got %d body=%s", code, string(raw))
		}
		var bizRsp RotateInboundPortResp
		if err := json.Unmarshal(raw, &bizRsp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if bizRsp.Code != 300 || bizRsp.Port != 0 || bizRsp.Msg != "port in use" {
			t.Fatalf("unexpected structured rsp: %+v", bizRsp)
		}
	})

	t.Run("MCP rotate_inbound_port auth rules and normal JWT cross-user rejection", func(t *testing.T) {
		// Normal JWT rotating own inbound -> success.
		res, err := normalSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "rotate_inbound_port",
			Arguments: map[string]any{"container": "xray", "inbound": "vless-in"},
		})
		if err != nil || res.IsError {
			t.Fatalf("normal MCP rotate_inbound_port self failed: err=%v res=%+v", err, res)
		}
		var out RotateInboundPortResp
		if err := json.Unmarshal([]byte(extractText(t, res)), &out); err != nil {
			t.Fatalf("unmarshal MCP output: %v", err)
		}
		if out.Code != 0 || out.Port != 24680 || out.Msg != "ok" {
			t.Fatalf("unexpected MCP rotate_inbound_port output: %+v", out)
		}

		// Normal JWT trying to rotate another user's port -> IsError=true!
		denyRes, err := normalSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "rotate_inbound_port",
			Arguments: map[string]any{"username": "bob", "container": "xray", "inbound": "vless-in"},
		})
		if err != nil {
			t.Fatalf("expected MCP tool error, got protocol error: %v", err)
		}
		if !denyRes.IsError || !strings.Contains(extractText(t, denyRes), "only admin can rotate another user's inbound port") {
			t.Fatalf("expected IsError=true 403 msg, got %+v text=%q", denyRes, extractText(t, denyRes))
		}

		// Admin JWT rotating another user -> success.
		adminRes, err := adminJWTSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "rotate_inbound_port",
			Arguments: map[string]any{"username": "alice", "container": "xray", "inbound": "vless-in"},
		})
		if err != nil || adminRes.IsError {
			t.Fatalf("admin JWT rotate_inbound_port for alice failed: err=%v res=%+v", err, adminRes)
		}
		if got := fix.rpcA.lastRotateInboundReq().GetUsername(); got != "alice" {
			t.Fatalf("expected RPC username 'alice', got %q", got)
		}

		// X-Token missing username -> IsError=true.
		xtRes, err := xTokenSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "rotate_inbound_port",
			Arguments: map[string]any{"container": "xray", "inbound": "vless-in"},
		})
		if err != nil || !xtRes.IsError || !strings.Contains(extractText(t, xtRes), "username required for X-Token requests") {
			t.Fatalf("expected X-Token missing username IsError, err=%v res=%+v", err, xtRes)
		}
	})
}

func TestRotateAllPortsEndpoint_HTTPAndMCP(t *testing.T) {
	fix := setupUserControlsWriteFixture(t, false)
	ctx := context.Background()
	normalJWT := makeJWT(t, fix.server, "alice", "normal")
	adminJWT := makeJWT(t, fix.server, "boss", "admin")

	normalHeaders := map[string]string{"Authorization": "Bearer " + normalJWT}
	adminJWTHeaders := map[string]string{"Authorization": "Bearer " + adminJWT}
	xTokenHeaders := map[string]string{"X-Token": fix.server.token}

	normalSess := connectMCPClient(t, fix.ts.URL, normalHeaders)
	adminJWTSess := connectMCPClient(t, fix.ts.URL, adminJWTHeaders)
	xTokenSess := connectMCPClient(t, fix.ts.URL, xTokenHeaders)

	t.Run("HTTP optional empty body, form binding, partial success, and auth rules", func(t *testing.T) {
		// Normal JWT with nil body (OptionalBody=true) -> 200 with ports map.
		code, raw := doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateAllPorts",
			nil, "", normalHeaders)
		if code != http.StatusOK {
			t.Fatalf("empty body expected 200, got %d body=%s", code, string(raw))
		}
		var rsp RotateAllPortsResp
		if err := json.Unmarshal(raw, &rsp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if rsp.Code != 0 || rsp.Msg != "ok" || rsp.Ports["xray:vless-in"] != 25001 || rsp.Ports["hysteria:hy2-in"] != 25002 {
			t.Fatalf("unexpected RotateAllPortsResp: %+v", rsp)
		}
		if got := fix.rpcA.lastRotateAllReq().GetUsername(); got != "alice" {
			t.Fatalf("expected RPC username 'alice', got %q", got)
		}

		// Form-urlencoded binding with X-Token.
		form := url.Values{}
		form.Set("target", "node-b")
		form.Set("username", "bob")
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateAllPorts",
			strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", xTokenHeaders)
		if code != http.StatusOK {
			t.Fatalf("form binding expected 200, got %d body=%s", code, string(raw))
		}
		if got := fix.rpcB.lastRotateAllReq().GetUsername(); got != "bob" {
			t.Fatalf("expected node-b RPC username 'bob', got %q", got)
		}

		// Partial success (code=301) preserves ports map.
		fix.rpcA.mu.Lock()
		fix.rpcA.rotateAllRsp = &proto.RotateAllPortsRsp{
			Code:  301,
			Ports: map[string]uint32{"xray:vless-in": 26001},
			Msg:   "hysteria:hy2-in failed",
		}
		fix.rpcA.mu.Unlock()
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateAllPorts",
			bytes.NewBufferString(`{}`), "application/json", adminJWTHeaders)
		fix.rpcA.mu.Lock()
		fix.rpcA.rotateAllRsp = nil
		fix.rpcA.mu.Unlock()
		if code != http.StatusOK {
			t.Fatalf("partial success expected 200, got %d body=%s", code, string(raw))
		}
		var partial RotateAllPortsResp
		if err := json.Unmarshal(raw, &partial); err != nil {
			t.Fatalf("unmarshal partial: %v", err)
		}
		if partial.Code != 301 || partial.Ports["xray:vless-in"] != 26001 || partial.Msg != "hysteria:hy2-in failed" {
			t.Fatalf("unexpected partial response: %+v", partial)
		}

		// Normal JWT targeting another user -> 403.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateAllPorts",
			bytes.NewBufferString(`{"username":"bob"}`), "application/json", normalHeaders)
		if code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%s", code, string(raw))
		}

		// X-Token with nil body -> 400.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPost, fix.ts.URL+"/api/rotateAllPorts",
			nil, "", xTokenHeaders)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400 for X-Token nil body, got %d body=%s", code, string(raw))
		}
	})

	t.Run("MCP rotate_all_ports execution and cross-user denial", func(t *testing.T) {
		res, err := normalSess.CallTool(ctx, &mcp.CallToolParams{Name: "rotate_all_ports"})
		if err != nil || res.IsError {
			t.Fatalf("normal MCP rotate_all_ports failed: err=%v res=%+v", err, res)
		}
		var out RotateAllPortsResp
		if err := json.Unmarshal([]byte(extractText(t, res)), &out); err != nil {
			t.Fatalf("unmarshal MCP output: %v", err)
		}
		if out.Code != 0 || out.Ports["xray:vless-in"] != 25001 {
			t.Fatalf("unexpected MCP rotate_all_ports output: %+v", out)
		}

		denyRes, err := normalSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "rotate_all_ports",
			Arguments: map[string]any{"username": "bob"},
		})
		if err != nil || !denyRes.IsError || !strings.Contains(extractText(t, denyRes), "only admin can rotate another user's ports") {
			t.Fatalf("expected MCP cross-user denial IsError=true, err=%v res=%+v", err, denyRes)
		}

		adminRes, err := adminJWTSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "rotate_all_ports",
			Arguments: map[string]any{"username": "alice"},
		})
		if err != nil || adminRes.IsError {
			t.Fatalf("admin JWT rotate_all_ports failed: err=%v res=%+v", err, adminRes)
		}

		xtRes, err := xTokenSess.CallTool(ctx, &mcp.CallToolParams{Name: "rotate_all_ports"})
		if err != nil || !xtRes.IsError || !strings.Contains(extractText(t, xtRes), "username required for X-Token requests") {
			t.Fatalf("expected X-Token missing username IsError, err=%v res=%+v", err, xtRes)
		}
	})
}

func TestChangePasswordEndpoint_HTTPAndMCP(t *testing.T) {
	fix := setupUserControlsWriteFixture(t, false)
	ctx := context.Background()
	normalJWT := makeJWT(t, fix.server, "alice", "normal")
	ghostJWT := makeJWT(t, fix.server, "ghost", "normal")

	normalHeaders := map[string]string{"Authorization": "Bearer " + normalJWT}
	ghostHeaders := map[string]string{"Authorization": "Bearer " + ghostJWT}
	xTokenHeaders := map[string]string{"X-Token": fix.server.token}

	normalSess := connectMCPClient(t, fix.ts.URL, normalHeaders)
	xTokenSess := connectMCPClient(t, fix.ts.URL, xTokenHeaders)

	t.Run("HTTP success, validation, auth, legacy HTTP 200 code=300, and partial failure", func(t *testing.T) {
		// Happy path.
		code, raw := doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/profile/password",
			bytes.NewBufferString(`{"old_password":"alice-old-pass","new_password":"alice-new-pass"}`), "application/json", normalHeaders)
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", code, string(raw))
		}
		var okBody struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(raw, &okBody); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if okBody.Code != 0 || okBody.Msg != "ok" {
			t.Fatalf("expected {code:0 msg:ok}, got %+v", okBody)
		}
		u := fix.rpcA.lastUpdatedUser()
		if u == nil || u.GetName() != "alice" || u.GetPasswd() != "alice-new-pass" {
			t.Fatalf("expected RPC User{Name:alice, Passwd:alice-new-pass}, got %+v", u)
		}

		// X-Token -> 401 JWT required.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/profile/password",
			bytes.NewBufferString(`{"old_password":"a","new_password":"b"}`), "application/json", xTokenHeaders)
		if code != http.StatusUnauthorized || !strings.Contains(string(raw), "JWT required") {
			t.Fatalf("expected 401 JWT required, got %d body=%s", code, string(raw))
		}

		// Empty old/new password -> 400.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/profile/password",
			bytes.NewBufferString(`{"old_password":"alice-old-pass"}`), "application/json", normalHeaders)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400 for missing new_password, got %d body=%s", code, string(raw))
		}

		// User not found -> 404.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/profile/password",
			bytes.NewBufferString(`{"old_password":"a","new_password":"b"}`), "application/json", ghostHeaders)
		if code != http.StatusNotFound || !strings.Contains(string(raw), "user not found") {
			t.Fatalf("expected 404 user not found, got %d body=%s", code, string(raw))
		}

		// Wrong old password -> 403.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/profile/password",
			bytes.NewBufferString(`{"old_password":"wrong-pass","new_password":"new-pass"}`), "application/json", normalHeaders)
		if code != http.StatusForbidden || !strings.Contains(string(raw), "old password is incorrect") {
			t.Fatalf("expected 403 old password is incorrect, got %d body=%s", code, string(raw))
		}

		// No available node -> legacy HTTP 200 with code=300.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/profile/password",
			bytes.NewBufferString(`{"target":"missing","old_password":"alice-old-pass","new_password":"new-pass"}`), "application/json", normalHeaders)
		if code != http.StatusOK {
			t.Fatalf("expected legacy HTTP 200, got %d body=%s", code, string(raw))
		}
		var biz300 struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(raw, &biz300); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if biz300.Code != 300 || biz300.Msg != "no available node" {
			t.Fatalf("expected {code:300 msg:'no available node'}, got %+v", biz300)
		}

		// All target nodes failed -> legacy HTTP 200 with code=300.
		fix.rpcB.mu.Lock()
		fix.rpcB.updateUsersErr = "write refused"
		fix.rpcB.mu.Unlock()
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/profile/password",
			bytes.NewBufferString(`{"target":"node-b","old_password":"alice-old-pass","new_password":"new-pass"}`), "application/json", normalHeaders)
		if code != http.StatusOK {
			t.Fatalf("expected legacy HTTP 200 on all-failed, got %d body=%s", code, string(raw))
		}
		if err := json.Unmarshal(raw, &biz300); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if biz300.Code != 300 || !strings.Contains(biz300.Msg, "write refused") {
			t.Fatalf("expected code=300 with 'write refused', got %+v", biz300)
		}

		// Partial failure (target=all: node-a succeeds, node-b fails) -> HTTP 200 with code=0 and msg listing failed node.
		code, raw = doUserControlsHTTP(t, fix.ts.Client(), http.MethodPut, fix.ts.URL+"/api/profile/password",
			bytes.NewBufferString(`{"target":"all","old_password":"alice-old-pass","new_password":"new-pass"}`), "application/json", normalHeaders)
		fix.rpcB.mu.Lock()
		fix.rpcB.updateUsersErr = ""
		fix.rpcB.mu.Unlock()
		if code != http.StatusOK {
			t.Fatalf("expected HTTP 200 on partial success, got %d body=%s", code, string(raw))
		}
		var partial struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(raw, &partial); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if partial.Code != 0 || !strings.Contains(partial.Msg, "write refused") {
			t.Fatalf("expected code=0 with partial error msg, got %+v", partial)
		}
	})

	t.Run("MCP change_password execution and NewBusinessErr maps to IsError=true", func(t *testing.T) {
		res, err := normalSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "change_password",
			Arguments: map[string]any{"old_password": "alice-old-pass", "new_password": "alice-mcp-pass"},
		})
		if err != nil || res.IsError {
			t.Fatalf("MCP change_password failed: err=%v res=%+v", err, res)
		}
		u := fix.rpcA.lastUpdatedUser()
		if u == nil || u.GetName() != "alice" || u.GetPasswd() != "alice-mcp-pass" {
			t.Fatalf("expected RPC User{Name:alice, Passwd:alice-mcp-pass}, got %+v", u)
		}

		// X-Token -> IsError=true ("JWT required").
		xtRes, err := xTokenSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "change_password",
			Arguments: map[string]any{"old_password": "a", "new_password": "b"},
		})
		if err != nil || !xtRes.IsError || !strings.Contains(extractText(t, xtRes), "JWT required") {
			t.Fatalf("expected MCP X-Token IsError 'JWT required', err=%v res=%+v", err, xtRes)
		}

		// Wrong old password -> IsError=true.
		wrongRes, err := normalSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "change_password",
			Arguments: map[string]any{"old_password": "nope", "new_password": "b"},
		})
		if err != nil || !wrongRes.IsError || !strings.Contains(extractText(t, wrongRes), "old password is incorrect") {
			t.Fatalf("expected MCP wrong password IsError, err=%v res=%+v", err, wrongRes)
		}

		// No available node (HTTP 200 code=300) -> MCP IsError=true!
		noNodeRes, err := normalSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "change_password",
			Arguments: map[string]any{"target": "missing", "old_password": "alice-old-pass", "new_password": "b"},
		})
		if err != nil || !noNodeRes.IsError || !strings.Contains(extractText(t, noNodeRes), "no available node") {
			t.Fatalf("expected MCP no available node IsError, err=%v res=%+v", err, noNodeRes)
		}
	})
}

func TestUserControlsWrite_LegacyHandlersDelegationAndHelpText(t *testing.T) {
	fix := setupUserControlsWriteFixture(t, false)
	s := fix.server

	roleH := &SetUserRoleHandler{}
	roleH.setHttpServer(s)
	bwH := &SetUserBandwidthHandler{}
	bwH.setHttpServer(s)
	clH := &SetUserClientLimitHandler{}
	clH.setHttpServer(s)
	ripH := &RotateInboundPortHandler{}
	ripH.setHttpServer(s)
	rapH := &RotateAllPortsHandler{}
	rapH.setHttpServer(s)
	cpH := &ChangePasswordHandler{}
	cpH.setHttpServer(s)

	cases := []struct {
		name     string
		handler  HttpHandlerInterface
		epHelp   string
		wantPath string
		wantHelp string
	}{
		{
			name:     "SetUserRoleHandler",
			handler:  roleH,
			epHelp:   s.setUserRoleEndpoint().Help,
			wantPath: "/user/:name/role",
			wantHelp: `/api/user/:name/role
	PUT /api/user/:name/role
	Header: X-Token: <admin-token>  OR  Authorization: Bearer <admin-jwt>
	Body: {"role":"admin"|"normal", "target":""}
	Sets the frontend login role for the specified user.
	X-Token is the break-glass path (no admin account needed); admin JWT also accepted.`,
		},
		{
			name:     "SetUserBandwidthHandler",
			handler:  bwH,
			epHelp:   s.setUserBandwidthEndpoint().Help,
			wantPath: "/user/:name/bandwidth",
			wantHelp: `PUT /api/user/:name/bandwidth
	Header: X-Token or Authorization: Bearer <admin-jwt>
	Body: {"upload_bps": 1000000, "download_bps": 500000, "target": ""}
	Sets bandwidth limits (bytes/sec). 0 removes the limit (unlimited).`,
		},
		{
			name:     "SetUserClientLimitHandler",
			handler:  clH,
			epHelp:   s.setUserClientLimitEndpoint().Help,
			wantPath: "/user/:name/client-limit",
			wantHelp: `PUT /api/user/:name/client-limit
	Header: X-Token or Authorization: Bearer <admin-jwt>
	Body: {"max_clients": 3, "recycle_delay_sec": 60, "drain_sec": 2, "target": ""}
	Sets client connection limits. max_clients is required; 0 removes the limit.
	recycle_delay_sec and drain_sec are optional (unchanged if omitted).`,
		},
		{
			name:     "RotateInboundPortHandler",
			handler:  ripH,
			epHelp:   s.rotateInboundPortEndpoint().Help,
			wantPath: "/rotateInboundPort",
			wantHelp: `/api/rotateInboundPort
	用户自助更换指定 container+inbound 的前置转发端口（JWT 鉴权，用户只能操作自己的 inbound）
	POST /api/rotateInboundPort
	Header: Authorization: Bearer <jwt>  OR  X-Token: <admin-token>
	Body: {"target":"node1","container":"xray","inbound":"<tag>","port":0}
	  target: 目标节点名，空则默认本机
	  X-Token: body 必须提供 username 字段
	  admin JWT: 可选 username 字段；不填默认操作自身
	  普通用户 JWT: 不允许指定他人 username（→ 403）
	port=0 随机分配，port>0 指定新端口
	Response: {"code":0,"port":12345,"msg":"ok"}`,
		},
		{
			name:     "RotateAllPortsHandler",
			handler:  rapH,
			epHelp:   s.rotateAllPortsEndpoint().Help,
			wantPath: "/rotateAllPorts",
			wantHelp: `/api/rotateAllPorts
	用户自助重置自身所有 inbound 的前置转发端口（JWT 鉴权，用户只能操作自己的 inbound）
	POST /api/rotateAllPorts
	Header: Authorization: Bearer <jwt>  OR  X-Token: <admin-token>
	Body (optional): {"target":"node1","username": "target-user"}
	  target: 目标节点名，空则默认本机
	  X-Token: body 必须提供 username 字段
	  admin JWT: 可选 username 字段；不填默认操作自身
	  普通用户 JWT: 不允许指定他人 username（→ 403）
	Response: {"code":0,"ports":{"xray:vless-tcp":23456,"xray:trojan-tls":34567},"msg":"ok"}
	部分成功时 code=301，ports 包含已成功轮换的 inbound，msg 包含失败原因
	全部失败时 code=300，ports 为空`,
		},
		{
			name:     "ChangePasswordHandler",
			handler:  cpH,
			epHelp:   s.changePasswordEndpoint().Help,
			wantPath: "/profile/password",
			wantHelp: `/api/profile/password
	PUT /api/profile/password
	Header: Authorization: Bearer <jwt-token>
	Body: {"target":"node1","old_password":"<current>","new_password":"<new>"}
	  target: 目标节点名，空则默认本机
	Changes the authenticated user's own password. JWT required.`,
		},
	}

	for _, tc := range cases {
		if tc.handler.getRelativePath() != tc.wantPath {
			t.Errorf("%s path=%q, want %q", tc.name, tc.handler.getRelativePath(), tc.wantPath)
		}
		if tc.handler.help() != tc.wantHelp || tc.epHelp != tc.wantHelp {
			t.Errorf("%s help mismatch:\nhandler=%q\nendpoint=%q\nwant=%q", tc.name, tc.handler.help(), tc.epHelp, tc.wantHelp)
		}
	}

	// Verify direct legacy handlerFunc delegation works end-to-end.
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	normalJWT := makeJWT(t, s, "alice", "normal")

	rBW := handlerEngine(http.MethodPut, "/user/:name/bandwidth", bwH.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/user/alice/bandwidth", bytes.NewBufferString(`{"upload_bps":0}`))
	req.Header.Set("Content-Type", "application/json")
	rBW.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("legacy SetUserBandwidthHandler delegation failed: code=%d body=%s", w.Code, w.Body.String())
	}

	rCL := handlerEngine(http.MethodPut, "/user/:name/client-limit", clH.handlerFunc)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/user/alice/client-limit", bytes.NewBufferString(`{"max_clients":2}`))
	req.Header.Set("Content-Type", "application/json")
	rCL.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("legacy SetUserClientLimitHandler delegation failed: code=%d body=%s", w.Code, w.Body.String())
	}

	rCP := handlerEngine(http.MethodPut, "/profile/password", cpH.handlerFunc, mw)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/profile/password", bytes.NewBufferString(`{"old_password":"alice-old-pass","new_password":"legacy-new"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+normalJWT)
	rCP.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"code":0`) {
		t.Fatalf("legacy ChangePasswordHandler delegation failed: code=%d body=%s", w.Code, w.Body.String())
	}
}
