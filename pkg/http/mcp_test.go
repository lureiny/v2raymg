package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for k, v := range h.headers {
		clone.Header.Set(k, v)
	}
	base := h.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func connectMCPClient(t *testing.T, serverURL string, headers map[string]string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: serverURL + "/mcp",
		HTTPClient: &http.Client{
			Transport: &headerRoundTripper{headers: headers},
		},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("mcp client connect failed: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func extractText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatalf("expected non-empty CallToolResult.Content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

type mcpTestServerFixture struct {
	server           *HttpServer
	ts               *httptest.Server
	userReadCalls    atomic.Int32
	adminWriteCalls  atomic.Int32
	httpOnlyCalls    atomic.Int32
	lastSeenCaller   atomic.Value // Caller
	lastSeenUpload   atomic.Value // *int64 wrapped in struct
	lastSeenDownload atomic.Value // *int64 wrapped in struct
}

type optionalPtrHolder struct {
	val *int64
}

func setupMCPTestServer(t *testing.T, readOnly bool) *mcpTestServerFixture {
	t.Helper()
	s := newTestHttpServer(nil)
	s.MCPReadOnly = readOnly
	fix := &mcpTestServerFixture{server: s}

	pub := s.RestfulServer
	userGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret))
	adminGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret), auth.AdminOnly())

	// Public endpoint with MCPName set — must NEVER be exposed on /mcp.
	type emptyReq struct{}
	registerEndpoint(s, pub, Endpoint[emptyReq, map[string]string]{
		Method:         http.MethodGet,
		Path:           "/public-info",
		Scope:          ScopePublic,
		MCPName:        "public_should_not_appear",
		MCPDescription: "Should not appear in MCP",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req emptyReq) (map[string]string, error) {
			return map[string]string{"pub": "yes"}, nil
		},
	})

	// ScopeUser ReadOnly endpoint returning struct with sensitive field + MCPSanitize.
	type profileReq struct {
		Target string `json:"target,omitempty" query:"target" jsonschema:"Target node name"`
	}
	type profileResp struct {
		Username  string `json:"username"`
		Role      string `json:"role"`
		Target    string `json:"target"`
		AuthToken string `json:"auth_token"`
	}
	registerEndpoint(s, userGroup, Endpoint[profileReq, profileResp]{
		Method:         http.MethodGet,
		Path:           "/profile-test",
		Scope:          ScopeUser,
		MCPName:        "get_profile",
		MCPDescription: "Get caller profile",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req profileReq) (profileResp, error) {
			fix.userReadCalls.Add(1)
			fix.lastSeenCaller.Store(caller)
			return profileResp{
				Username:  caller.Username,
				Role:      caller.Role,
				Target:    req.Target,
				AuthToken: "secret-auth-token-123",
			}, nil
		},
		MCPSanitize: func(resp profileResp) any {
			return map[string]any{
				"username": resp.Username,
				"role":     resp.Role,
				"target":   resp.Target,
			}
		},
	})

	// ScopeUser Write endpoint returning legacy HTTP 200 + code=300 business error on trigger.
	type rotateReq struct {
		Target   string `json:"target,omitempty" jsonschema:"Target node"`
		FailMode string `json:"fail_mode,omitempty" jsonschema:"Simulate error mode"`
	}
	type rotateResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	registerEndpoint(s, userGroup, Endpoint[rotateReq, rotateResp]{
		Method:         http.MethodPost,
		Path:           "/rotate-test",
		Scope:          ScopeUser,
		OptionalBody:   true,
		MCPName:        "rotate_ports",
		MCPDescription: "Rotate user ports",
		ReadOnly:       false,
		Destructive:    false,
		Execute: func(ctx context.Context, caller Caller, req rotateReq) (rotateResp, error) {
			fix.lastSeenCaller.Store(caller)
			if req.FailMode == "biz300" {
				return rotateResp{}, NewBusinessErr(http.StatusOK, 300, "no available node")
			}
			if req.FailMode == "ep400" {
				return rotateResp{}, NewEndpointErr(http.StatusBadRequest, "bad rotate param")
			}
			return rotateResp{Code: 0, Msg: "rotated"}, nil
		},
	})

	// ScopeAdmin ReadOnly endpoint returning a JSON slice (tests non-object MCP output!).
	type nodeItem struct {
		Name string `json:"name"`
	}
	registerEndpoint(s, adminGroup, Endpoint[emptyReq, []nodeItem]{
		Method:         http.MethodGet,
		Path:           "/nodes-test",
		Scope:          ScopeAdmin,
		MCPName:        "list_nodes",
		MCPDescription: "List cluster nodes",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req emptyReq) ([]nodeItem, error) {
			fix.lastSeenCaller.Store(caller)
			return []nodeItem{{Name: "node-a"}, {Name: "node-b"}}, nil
		},
	})

	// ScopeAdmin Destructive write endpoint with *int64 pointers.
	type bwReq struct {
		Name        string `json:"name" path:"name" jsonschema:"Username"`
		UploadBps   *int64 `json:"upload_bps,omitempty" jsonschema:"Upload limit in bytes/sec (0 removes limit)"`
		DownloadBps *int64 `json:"download_bps,omitempty" jsonschema:"Download limit in bytes/sec"`
	}
	registerEndpoint(s, adminGroup, Endpoint[bwReq, map[string]any]{
		Method:         http.MethodPut,
		Path:           "/user/:name/bw-test",
		Scope:          ScopeAdmin,
		MCPName:        "set_user_bandwidth",
		MCPDescription: "Set or remove user bandwidth limits",
		ReadOnly:       false,
		Destructive:    true,
		Execute: func(ctx context.Context, caller Caller, req bwReq) (map[string]any, error) {
			fix.adminWriteCalls.Add(1)
			fix.lastSeenCaller.Store(caller)
			fix.lastSeenUpload.Store(optionalPtrHolder{val: req.UploadBps})
			fix.lastSeenDownload.Store(optionalPtrHolder{val: req.DownloadBps})
			return map[string]any{"code": 0, "user": req.Name}, nil
		},
	})

	// Endpoint without MCPName — HTTP only.
	registerEndpoint(s, adminGroup, Endpoint[emptyReq, map[string]any]{
		Method: http.MethodGet,
		Path:   "/http-only-test",
		Scope:  ScopeAdmin,
		Execute: func(ctx context.Context, caller Caller, req emptyReq) (map[string]any, error) {
			fix.httpOnlyCalls.Add(1)
			return map[string]any{"ok": true}, nil
		},
	})

	RegisterMCP(s)
	fix.ts = httptest.NewServer(s.RestfulServer)
	t.Cleanup(fix.ts.Close)
	return fix
}

func TestMCP_UnauthenticatedRejected401(t *testing.T) {
	fix := setupMCPTestServer(t, false)
	client := fix.ts.Client()

	// Revoked JWT
	revokedJWT := makeJWT(t, fix.server, "revoked-user", "admin")
	claims, err := auth.ParseJWT(fix.server.jwtSecret, revokedJWT)
	if err != nil {
		t.Fatalf("parse jwt: %v", err)
	}
	auth.BlacklistAdd(claims.JTI)

	cases := []struct {
		name    string
		method  string
		headers map[string]string
	}{
		{"POST no auth", http.MethodPost, nil},
		{"GET no auth", http.MethodGet, map[string]string{"Accept": "text/event-stream"}},
		{"DELETE no auth", http.MethodDelete, nil},
		{"POST wrong X-Token", http.MethodPost, map[string]string{"X-Token": "wrong-token"}},
		{"POST invalid JWT", http.MethodPost, map[string]string{"Authorization": "Bearer not-a-jwt"}},
		{"POST revoked JWT", http.MethodPost, map[string]string{"Authorization": "Bearer " + revokedJWT}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, fix.ts.URL+"/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("do request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", resp.StatusCode)
			}
		})
	}
}

func TestMCP_RoleIsolation_AdminVsNormalUser(t *testing.T) {
	fix := setupMCPTestServer(t, false)
	ctx := context.Background()

	// 1. X-Token (admin) sees ScopeUser + ScopeAdmin tools, not ScopePublic or MCPName=="".
	xtokenSess := connectMCPClient(t, fix.ts.URL, map[string]string{"X-Token": fix.server.token})
	xtokenTools, err := xtokenSess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("x-token ListTools failed: %v", err)
	}
	gotAdminNames := toolNames(xtokenTools.Tools)
	wantAdminNames := []string{"get_profile", "list_nodes", "rotate_ports", "set_user_bandwidth"}
	if strings.Join(gotAdminNames, ",") != strings.Join(wantAdminNames, ",") {
		t.Fatalf("admin tools mismatch: got %v want %v", gotAdminNames, wantAdminNames)
	}

	// Verify ToolAnnotations on listed tools.
	byName := map[string]*mcp.Tool{}
	for _, tl := range xtokenTools.Tools {
		byName[tl.Name] = tl
	}
	if byName["list_nodes"].Annotations == nil || !byName["list_nodes"].Annotations.ReadOnlyHint {
		t.Fatalf("expected list_nodes ReadOnlyHint=true, got %+v", byName["list_nodes"].Annotations)
	}
	if byName["set_user_bandwidth"].Annotations == nil || byName["set_user_bandwidth"].Annotations.ReadOnlyHint {
		t.Fatalf("expected set_user_bandwidth ReadOnlyHint=false, got %+v", byName["set_user_bandwidth"].Annotations)
	}
	if byName["set_user_bandwidth"].Annotations.DestructiveHint == nil || !*byName["set_user_bandwidth"].Annotations.DestructiveHint {
		t.Fatalf("expected set_user_bandwidth DestructiveHint=true, got %+v", byName["set_user_bandwidth"].Annotations)
	}
	if byName["rotate_ports"].Annotations.DestructiveHint == nil || *byName["rotate_ports"].Annotations.DestructiveHint {
		t.Fatalf("expected rotate_ports DestructiveHint=false, got %+v", byName["rotate_ports"].Annotations)
	}

	// 2. Admin JWT sees all admin + user tools and propagates Caller{Username, Role}.
	adminJWT := makeJWT(t, fix.server, "boss", "admin")
	adminJWTSess := connectMCPClient(t, fix.ts.URL, map[string]string{"Authorization": "Bearer " + adminJWT})
	res, err := adminJWTSess.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_nodes",
	})
	if err != nil {
		t.Fatalf("admin JWT CallTool list_nodes failed: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected IsError on list_nodes: %s", extractText(t, res))
	}
	// Slice output must marshal cleanly to JSON array text content!
	var nodes []map[string]string
	if err := json.Unmarshal([]byte(extractText(t, res)), &nodes); err != nil {
		t.Fatalf("expected JSON slice output from list_nodes, got %q: %v", extractText(t, res), err)
	}
	if len(nodes) != 2 || nodes[0]["name"] != "node-a" || nodes[1]["name"] != "node-b" {
		t.Fatalf("unexpected list_nodes output: %+v", nodes)
	}
	if c := fix.lastSeenCaller.Load().(Caller); c.Username != "boss" || c.Role != "admin" {
		t.Fatalf("expected Caller{boss, admin}, got %+v", c)
	}

	// 3. Normal JWT sees ONLY ScopeUser tools.
	normalJWT := makeJWT(t, fix.server, "alice", "normal")
	normalSess := connectMCPClient(t, fix.ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
	normalTools, err := normalSess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("normal user ListTools failed: %v", err)
	}
	gotUserNames := toolNames(normalTools.Tools)
	wantUserNames := []string{"get_profile", "rotate_ports"}
	if strings.Join(gotUserNames, ",") != strings.Join(wantUserNames, ",") {
		t.Fatalf("normal user tools mismatch: got %v want %v", gotUserNames, wantUserNames)
	}

	// 4. Normal JWT calling an admin tool by name MUST be rejected and NEVER invoke Execute.
	beforeAdminCalls := fix.adminWriteCalls.Load()
	_, err = normalSess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "set_user_bandwidth",
		Arguments: map[string]any{"name": "alice", "upload_bps": 100},
	})
	if err == nil {
		t.Fatalf("expected error when normal user calls admin tool set_user_bandwidth")
	}
	if fix.adminWriteCalls.Load() != beforeAdminCalls {
		t.Fatalf("admin Execute must not run for normal user")
	}
}

func TestMCP_DefenseInDepthScopeCheck(t *testing.T) {
	// Even if an admin tool is somehow mounted on a server reachable by a non-admin
	// caller context, the tool handler itself enforces ep.Scope == ScopeAdmin.
	type reqT struct{}
	called := false
	reg := newMCPToolRegistrar(Endpoint[reqT, map[string]string]{
		Method:         http.MethodPost,
		Path:           "/secret-admin",
		Scope:          ScopeAdmin,
		MCPName:        "secret_admin_op",
		MCPDescription: "Admin only op",
		Execute: func(ctx context.Context, caller Caller, req reqT) (map[string]string, error) {
			called = true
			return map[string]string{"ok": "yes"}, nil
		},
	})

	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	reg.register(srv)

	// Connect in-memory client/server with a non-admin caller injected on server context.
	t1, t2 := mcp.NewInMemoryTransports()
	ctx := withMCPCaller(context.Background(), Caller{Username: "alice", Role: "normal"})
	ss, err := srv.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "secret_admin_op"})
	if err != nil {
		t.Fatalf("expected tool error result (IsError=true), got protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true for non-admin calling ScopeAdmin tool")
	}
	if called {
		t.Fatalf("Execute must not be called when defense-in-depth rejects caller")
	}
	if !strings.Contains(extractText(t, res), "admin role required") {
		t.Fatalf("unexpected error text: %q", extractText(t, res))
	}
}

func TestMCP_ReadOnlyModeFiltering(t *testing.T) {
	fix := setupMCPTestServer(t, true)
	ctx := context.Background()

	// Admin in read-only mode sees only read-only tools across user + admin scopes.
	adminSess := connectMCPClient(t, fix.ts.URL, map[string]string{"X-Token": fix.server.token})
	adminTools, err := adminSess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("admin ListTools failed: %v", err)
	}
	if got := strings.Join(toolNames(adminTools.Tools), ","); got != "get_profile,list_nodes" {
		t.Fatalf("expected only read-only tools [get_profile list_nodes], got %q", got)
	}

	// Normal user in read-only mode sees only ScopeUser read-only tools.
	normalJWT := makeJWT(t, fix.server, "alice", "normal")
	userSess := connectMCPClient(t, fix.ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
	userTools, err := userSess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("user ListTools failed: %v", err)
	}
	if got := strings.Join(toolNames(userTools.Tools), ","); got != "get_profile" {
		t.Fatalf("expected only [get_profile], got %q", got)
	}

	// Edge case: server with zero tools visible to normal user still answers tools/list cleanly.
	sEmpty := newTestHttpServer(nil)
	sEmpty.MCPReadOnly = true
	registerEndpoint(sEmpty, sEmpty.RestfulServer.Group("/api"), Endpoint[struct{}, map[string]string]{
		Method:      http.MethodPost,
		Path:        "/write-only",
		Scope:       ScopeUser,
		MCPName:     "write_only",
		ReadOnly:    false,
		Destructive: false,
		Execute: func(ctx context.Context, caller Caller, req struct{}) (map[string]string, error) {
			return map[string]string{"ok": "1"}, nil
		},
	})
	RegisterMCP(sEmpty)
	tsEmpty := httptest.NewServer(sEmpty.RestfulServer)
	defer tsEmpty.Close()

	emptySess := connectMCPClient(t, tsEmpty.URL, map[string]string{"X-Token": sEmpty.token})
	emptyTools, err := emptySess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools on zero-tool server should succeed, got: %v", err)
	}
	if len(emptyTools.Tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(emptyTools.Tools))
	}
}

func TestMCP_DirectExecute_Sanitize_ErrorSemantics_AndPointers(t *testing.T) {
	fix := setupMCPTestServer(t, false)
	ctx := context.Background()
	normalJWT := makeJWT(t, fix.server, "alice", "normal")

	// 1. Compare HTTP vs MCP output for MCPSanitize (auth_token present on HTTP, stripped on MCP).
	httpReq, _ := http.NewRequest(http.MethodGet, fix.ts.URL+"/api/profile-test?target=node1", nil)
	httpReq.Header.Set("Authorization", "Bearer "+normalJWT)
	httpResp, err := fix.ts.Client().Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP GET profile-test: %v", err)
	}
	defer httpResp.Body.Close()
	var httpProfile map[string]any
	if err := json.NewDecoder(httpResp.Body).Decode(&httpProfile); err != nil {
		t.Fatalf("decode HTTP profile: %v", err)
	}
	if httpProfile["auth_token"] != "secret-auth-token-123" {
		t.Fatalf("expected HTTP response to retain auth_token, got %+v", httpProfile)
	}

	userSess := connectMCPClient(t, fix.ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
	beforeCalls := fix.userReadCalls.Load()
	mcpRes, err := userSess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_profile",
		Arguments: map[string]any{"target": "node1"},
	})
	if err != nil {
		t.Fatalf("MCP CallTool get_profile failed: %v", err)
	}
	if mcpRes.IsError {
		t.Fatalf("unexpected IsError: %s", extractText(t, mcpRes))
	}
	if fix.userReadCalls.Load() != beforeCalls+1 {
		t.Fatalf("expected Execute to be called directly once")
	}
	var mcpProfile map[string]any
	if err := json.Unmarshal([]byte(extractText(t, mcpRes)), &mcpProfile); err != nil {
		t.Fatalf("unmarshal MCP profile text: %v", err)
	}
	if _, leaked := mcpProfile["auth_token"]; leaked {
		t.Fatalf("MCPSanitize failed: auth_token leaked in MCP response: %+v", mcpProfile)
	}
	if mcpProfile["username"] != "alice" || mcpProfile["role"] != "normal" || mcpProfile["target"] != "node1" {
		t.Fatalf("unexpected MCP profile payload: %+v", mcpProfile)
	}

	// 2. Legacy HTTP 200 + code=300 business error vs MCP IsError=true.
	bizHTTPReq, _ := http.NewRequest(http.MethodPost, fix.ts.URL+"/api/rotate-test", bytes.NewBufferString(`{"fail_mode":"biz300"}`))
	bizHTTPReq.Header.Set("Content-Type", "application/json")
	bizHTTPReq.Header.Set("Authorization", "Bearer "+normalJWT)
	bizHTTPResp, err := fix.ts.Client().Do(bizHTTPReq)
	if err != nil {
		t.Fatalf("HTTP POST rotate-test: %v", err)
	}
	defer bizHTTPResp.Body.Close()
	if bizHTTPResp.StatusCode != http.StatusOK {
		t.Fatalf("expected legacy HTTP 200 for business error, got %d", bizHTTPResp.StatusCode)
	}
	var bizBody struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.NewDecoder(bizHTTPResp.Body).Decode(&bizBody); err != nil {
		t.Fatalf("decode biz body: %v", err)
	}
	if bizBody.Code != 300 || bizBody.Msg != "no available node" {
		t.Fatalf("expected {code:300 msg:'no available node'}, got %+v", bizBody)
	}

	bizMCPRes, err := userSess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "rotate_ports",
		Arguments: map[string]any{"fail_mode": "biz300"},
	})
	if err != nil {
		t.Fatalf("expected MCP tool error (IsError=true), not protocol error: %v", err)
	}
	if !bizMCPRes.IsError {
		t.Fatalf("expected IsError=true for business error in MCP")
	}
	if got := extractText(t, bizMCPRes); got != "no available node" {
		t.Fatalf("expected MCP error text 'no available node', got %q", got)
	}

	// 3. Pointer semantics (*int64 explicit 0 vs omitted nil) through MCP CallTool.
	adminSess := connectMCPClient(t, fix.ts.URL, map[string]string{"X-Token": fix.server.token})
	bwRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "set_user_bandwidth",
		Arguments: map[string]any{"name": "alice", "upload_bps": 0},
	})
	if err != nil {
		t.Fatalf("CallTool set_user_bandwidth failed: %v", err)
	}
	if bwRes.IsError {
		t.Fatalf("unexpected IsError: %s", extractText(t, bwRes))
	}
	up := fix.lastSeenUpload.Load().(optionalPtrHolder).val
	down := fix.lastSeenDownload.Load().(optionalPtrHolder).val
	if up == nil || *up != 0 {
		t.Fatalf("expected upload_bps=&0 in MCP Execute, got %v", up)
	}
	if down != nil {
		t.Fatalf("expected download_bps=nil when omitted in MCP Execute, got %v", *down)
	}
}

func TestMCP_StatelessRequestsWithoutSession(t *testing.T) {
	fix := setupMCPTestServer(t, false)
	client := fix.ts.Client()

	// Stateless server must accept tools/list directly without initialize or Mcp-Session-Id.
	post := func(payload string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, fix.ts.URL+"/mcp", bytes.NewBufferString(payload))
		if err != nil {
			t.Fatalf("new req: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("X-Token", fix.server.token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do req: %v", err)
		}
		return resp
	}

	resp1 := post(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("stateless tools/list expected 200, got %d", resp1.StatusCode)
	}
	var rpcResp1 struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&rpcResp1); err != nil {
		t.Fatalf("decode stateless tools/list response: %v", err)
	}
	if len(rpcResp1.Result.Tools) != 4 {
		t.Fatalf("expected 4 admin tools in stateless response, got %d", len(rpcResp1.Result.Tools))
	}

	// Second independent stateless tools/call with no Mcp-Session-Id header.
	resp2 := post(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_nodes","arguments":{}}}`)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("stateless tools/call expected 200, got %d", resp2.StatusCode)
	}

	// GET /mcp in stateless mode must return 405 Method Not Allowed (per MCP spec).
	getReq, _ := http.NewRequest(http.MethodGet, fix.ts.URL+"/mcp", nil)
	getReq.Header.Set("Accept", "text/event-stream")
	getReq.Header.Set("X-Token", fix.server.token)
	getResp, err := client.Do(getReq)
	if err != nil {
		t.Fatalf("GET /mcp failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed for GET /mcp in stateless mode, got %d", getResp.StatusCode)
	}
}

func TestMCP_DefaultTagsParity(t *testing.T) {
	type embeddedTarget struct {
		Target string `json:"target,omitempty" query:"target" default:"all" jsonschema:"Target node"`
	}
	type defaultsReq struct {
		embeddedTarget
		Container string   `json:"container,omitempty" query:"container" default:"xray" jsonschema:"Container type"`
		UseSNI    bool     `json:"use_sni,omitempty"   query:"use_sni"   default:"true" jsonschema:"Use SNI"`
		Limit     int      `json:"limit,omitempty"     query:"limit"     default:"10"   jsonschema:"Result limit"`
		OptInt    *int     `json:"opt_int,omitempty"   query:"opt_int"   default:"7"    jsonschema:"Optional int"`
		Tags      []string `json:"tags,omitempty"      query:"tags"      default:"a,b"  jsonschema:"Tags"`
	}
	type defaultsResp struct {
		Target    string   `json:"target"`
		Container string   `json:"container"`
		UseSNI    bool     `json:"use_sni"`
		Limit     int      `json:"limit"`
		OptInt    *int     `json:"opt_int"`
		Tags      []string `json:"tags"`
	}

	s := newTestHttpServer(nil)
	userGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret))
	registerEndpoint(s, userGroup, Endpoint[defaultsReq, defaultsResp]{
		Method:         http.MethodGet,
		Path:           "/defaults-test",
		Scope:          ScopeUser,
		MCPName:        "check_defaults",
		MCPDescription: "Check default tag parity between HTTP and MCP",
		ReadOnly:       true,
		Execute: func(ctx context.Context, caller Caller, req defaultsReq) (defaultsResp, error) {
			return defaultsResp{
				Target:    req.Target,
				Container: req.Container,
				UseSNI:    req.UseSNI,
				Limit:     req.Limit,
				OptInt:    req.OptInt,
				Tags:      req.Tags,
			}, nil
		},
	})
	RegisterMCP(s)
	ts := httptest.NewServer(s.RestfulServer)
	defer ts.Close()

	ctx := context.Background()
	sess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})

	callAndDecode := func(t *testing.T, args map[string]any) defaultsResp {
		t.Helper()
		res, err := sess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "check_defaults",
			Arguments: args,
		})
		if err != nil {
			t.Fatalf("CallTool failed: %v", err)
		}
		if res.IsError {
			t.Fatalf("unexpected IsError: %s", extractText(t, res))
		}
		var out defaultsResp
		if err := json.Unmarshal([]byte(extractText(t, res)), &out); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		return out
	}

	t.Run("omitted arguments receive defaults matching HTTP", func(t *testing.T) {
		got := callAndDecode(t, nil)
		if got.Target != "all" || got.Container != "xray" || !got.UseSNI || got.Limit != 10 {
			t.Fatalf("unexpected defaults: %+v", got)
		}
		if got.OptInt == nil || *got.OptInt != 7 {
			t.Fatalf("expected OptInt=&7, got %v", got.OptInt)
		}
		if strings.Join(got.Tags, ",") != "a,b" {
			t.Fatalf("expected Tags=[a b], got %v", got.Tags)
		}
	})

	t.Run("explicit empty string gets default", func(t *testing.T) {
		got := callAndDecode(t, map[string]any{"target": "", "container": ""})
		if got.Target != "all" || got.Container != "xray" {
			t.Fatalf("expected empty strings to fall back to defaults (all/xray), got %+v", got)
		}
	})

	t.Run("explicit false, 0, and non-empty values are NOT clobbered by defaults", func(t *testing.T) {
		got := callAndDecode(t, map[string]any{
			"target":    "node1",
			"container": "mihomo",
			"use_sni":   false,
			"limit":     0,
			"opt_int":   0,
			"tags":      []string{"only"},
		})
		if got.Target != "node1" || got.Container != "mihomo" {
			t.Fatalf("expected explicit strings preserved, got %+v", got)
		}
		if got.UseSNI {
			t.Fatalf("explicit use_sni=false was clobbered by default:'true'")
		}
		if got.Limit != 0 {
			t.Fatalf("explicit limit=0 was clobbered by default:'10', got %d", got.Limit)
		}
		if got.OptInt == nil || *got.OptInt != 0 {
			t.Fatalf("explicit opt_int=0 was clobbered by default:'7', got %v", got.OptInt)
		}
		if strings.Join(got.Tags, ",") != "only" {
			t.Fatalf("expected Tags=[only], got %v", got.Tags)
		}
	})
}
