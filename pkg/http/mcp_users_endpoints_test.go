package http

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/cluster"
	commonrpc "github.com/lureiny/v2raymg/pkg/common/rpc"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
)

const testUsersClusterToken = "test-users-cluster-secret-token-123456"

type fakeUsersRPCServer struct {
	proto.UnimplementedEndNodeAccessServer
	nodeName string
	users    []*proto.User
	profiles map[string]*proto.GetProfileRsp
}

func (f *fakeUsersRPCServer) GetUsers(_ context.Context, _ *proto.GetUsersReq) (*proto.GetUsersRsp, error) {
	return &proto.GetUsersRsp{
		Code:  0,
		Users: f.users,
	}, nil
}

func (f *fakeUsersRPCServer) GetProfile(_ context.Context, req *proto.GetProfileReq) (*proto.GetProfileRsp, error) {
	if rsp, ok := f.profiles[req.GetUsername()]; ok {
		return rsp, nil
	}
	return &proto.GetProfileRsp{Code: 0}, nil
}

type staticUsersClusterNodes struct {
	nodes []*cluster.Node
	token string
}

func (s *staticUsersClusterNodes) GetNodesWithFilter(f cluster.NodeFilter) []*cluster.Node {
	var out []*cluster.Node
	for _, n := range s.nodes {
		if f(n) {
			out = append(out, n)
		}
	}
	return out
}

func (s *staticUsersClusterNodes) GetClusterToken() string {
	return s.token
}

func startFakeUsersNode(t *testing.T, name string, users []*proto.User, profiles map[string]*proto.GetProfileRsp) *cluster.Node {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake grpc node %s: %v", name, err)
	}
	srv := grpc.NewServer(grpc.ForceServerCodec(commonrpc.NewEncryptMessageCodec(testUsersClusterToken)))
	proto.RegisterEndNodeAccessServer(srv, &fakeUsersRPCServer{
		nodeName: name,
		users:    users,
		profiles: profiles,
	})
	go func() {
		_ = srv.Serve(lis)
	}()

	addr := lis.Addr().(*net.TCPAddr)
	node := &cluster.Node{
		Node: &proto.Node{
			Name:   name,
			Host:   "127.0.0.1",
			Port:   int32(addr.Port),
			NodeId: name + "-id",
		},
		CreateTime: time.Now().Unix(),
	}
	node.SetOutToken("test-out-token")
	node.SetReportHeartBeatTime(time.Now().Unix())

	t.Cleanup(func() {
		node.CloseConn()
		srv.Stop()
		_ = lis.Close()
	})
	return node
}

type usersEndpointTestFixture struct {
	server *HttpServer
	ts     *httptest.Server
}

func setupUsersEndpointsTestServer(t *testing.T, readOnly bool) *usersEndpointTestFixture {
	t.Helper()

	nodeA := startFakeUsersNode(t, "node-a",
		[]*proto.User{
			{
				Name:                  "alice",
				AuthToken:             "alice-secret-auth-token-A",
				ExpireTime:            1893456000,
				Downlink:              2048,
				Uplink:                1024,
				Role:                  "normal",
				UploadBps:             50000,
				DownloadBps:           100000,
				MaxClients:            5,
				ClientRecycleDelaySec: 30,
				ClientDrainSec:        10,
				Group:                 "vip",
			},
			{
				Name:       "empty-tok-user",
				AuthToken:  "",
				ExpireTime: 0,
				Role:       "normal",
			},
		},
		map[string]*proto.GetProfileRsp{
			"alice": {
				Code:          0,
				Username:      "alice",
				Role:          "normal",
				ExpireTime:    1893456000,
				Downlink:      2048,
				Uplink:        1024,
				ProxyPassword: "alice-secret-proxy-password-A",
				TrafficLimit:  1048576,
				Inbounds: []*proto.ProfileInbound{
					{Tag: "vless-in", Container: "xray", Port: 443},
				},
			},
			"boss": {
				Code:          0,
				Username:      "boss",
				Role:          "admin",
				ExpireTime:    0,
				Downlink:      512,
				Uplink:        256,
				ProxyPassword: "boss-secret-proxy-password-A",
				TrafficLimit:  0,
				Inbounds: []*proto.ProfileInbound{
					{Tag: "hy2-in", Container: "hysteria", Port: 8443},
				},
			},
		},
	)

	nodeB := startFakeUsersNode(t, "node-b",
		[]*proto.User{
			{
				Name:      "bob",
				AuthToken: "bob-secret-auth-token-B",
				Role:      "normal",
				Group:     "default",
			},
		},
		map[string]*proto.GetProfileRsp{
			"alice": {
				Code:          0,
				Username:      "alice",
				Role:          "normal",
				ExpireTime:    1893456000,
				Downlink:      4096,
				Uplink:        2048,
				ProxyPassword: "",
				TrafficLimit:  2097152,
			},
		},
	)

	s := newTestHttpServer(nil)
	s.Name = "node-a"
	s.MCPReadOnly = readOnly
	s.localNode = &cluster.LocalNode{
		Node: proto.Node{
			Name:   "local-node",
			NodeId: "local-node-id",
		},
	}
	s.clusterNodes = &staticUsersClusterNodes{
		nodes: []*cluster.Node{nodeA, nodeB},
		token: testUsersClusterToken,
	}

	userGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret))
	registerEndpoint(s, userGroup, s.userListEndpoint())
	registerEndpoint(s, userGroup, s.profileEndpoint())
	RegisterMCP(s)

	ts := httptest.NewServer(s.RestfulServer)
	t.Cleanup(ts.Close)
	return &usersEndpointTestFixture{server: s, ts: ts}
}

func TestSanitizeUserEntriesForMCP_DeepCopyAndRedaction(t *testing.T) {
	t.Run("nil and empty maps", func(t *testing.T) {
		if got := sanitizeUserEntriesForMCP(nil); got != nil {
			t.Fatalf("expected nil for nil input, got %#v", got)
		}
		empty := map[string][]gin.H{}
		gotEmpty, ok := sanitizeUserEntriesForMCP(empty).(map[string][]gin.H)
		if !ok || gotEmpty == nil || len(gotEmpty) != 0 {
			t.Fatalf("expected empty non-nil map, got %#v", gotEmpty)
		}
	})

	t.Run("redacts non-empty auth_token, preserves empty string, never mutates original", func(t *testing.T) {
		origInbounds := []gin.H{
			{"node": "node-a", "tag": "vless", "container": "xray", "port": uint32(443)},
		}
		orig := map[string][]gin.H{
			"node-a": {
				{
					"name":       "alice",
					"auth_token": "super-secret-token",
					"role":       "normal",
					"inbounds":   origInbounds,
				},
				{
					"name":       "blank-tok",
					"auth_token": "",
					"role":       "normal",
				},
				{
					"name": "no-tok-key",
					"role": "normal",
				},
				nil,
			},
			"node-nil": nil,
		}

		sanitizedAny := sanitizeUserEntriesForMCP(orig)
		sanitized, ok := sanitizedAny.(map[string][]gin.H)
		if !ok {
			t.Fatalf("expected map[string][]gin.H, got %T", sanitizedAny)
		}

		// Sanitized assertions.
		if sanitized["node-a"][0]["auth_token"] != "[REDACTED]" {
			t.Fatalf("expected non-empty auth_token redacted, got %#v", sanitized["node-a"][0]["auth_token"])
		}
		if sanitized["node-a"][1]["auth_token"] != "" {
			t.Fatalf("expected empty auth_token preserved as empty string, got %#v", sanitized["node-a"][1]["auth_token"])
		}
		if _, exists := sanitized["node-a"][2]["auth_token"]; exists {
			t.Fatalf("expected absent auth_token key to remain absent")
		}
		if sanitized["node-a"][3] != nil {
			t.Fatalf("expected nil entry preserved as nil, got %#v", sanitized["node-a"][3])
		}
		if sanitized["node-nil"] != nil {
			t.Fatalf("expected nil slice preserved as nil, got %#v", sanitized["node-nil"])
		}

		// Original must be completely untouched.
		if orig["node-a"][0]["auth_token"] != "super-secret-token" {
			t.Fatalf("original auth_token was mutated in place: %#v", orig["node-a"][0]["auth_token"])
		}

		// Mutating the returned structure must NOT affect original (deep copy check).
		sanitized["node-a"][0]["name"] = "mutated"
		sanitized["node-a"][0]["extra"] = "leaked"
		sanitizedInbounds := sanitized["node-a"][0]["inbounds"].([]gin.H)
		sanitizedInbounds[0]["tag"] = "mutated-tag"
		sanitized["node-added"] = []gin.H{{"name": "ghost"}}

		if orig["node-a"][0]["name"] != "alice" {
			t.Fatalf("mutating sanitized entry affected original name: %#v", orig["node-a"][0]["name"])
		}
		if _, leaked := orig["node-a"][0]["extra"]; leaked {
			t.Fatalf("mutating sanitized entry added key to original")
		}
		if origInbounds[0]["tag"] != "vless" {
			t.Fatalf("mutating sanitized inbounds affected original inbounds: %#v", origInbounds[0]["tag"])
		}
		if _, leaked := orig["node-added"]; leaked {
			t.Fatalf("mutating sanitized map affected original map")
		}
	})
}

func TestUsersEndpoints_MCPVisibilityAndAnnotations(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		name := "read_write"
		if readOnly {
			name = "read_only"
		}
		t.Run(name, func(t *testing.T) {
			fix := setupUsersEndpointsTestServer(t, readOnly)
			ctx := context.Background()

			// Admin via X-Token sees both list_users and get_profile.
			adminSess := connectMCPClient(t, fix.ts.URL, map[string]string{"X-Token": fix.server.token})
			adminList, err := adminSess.ListTools(ctx, &mcp.ListToolsParams{})
			if err != nil {
				t.Fatalf("admin ListTools failed: %v", err)
			}
			if got := strings.Join(toolNames(adminList.Tools), ","); got != "get_profile,list_users" {
				t.Fatalf("admin tools mismatch: got %q, want %q", got, "get_profile,list_users")
			}
			for _, tl := range adminList.Tools {
				if tl.Annotations == nil || !tl.Annotations.ReadOnlyHint {
					t.Fatalf("tool %s must have ReadOnlyHint=true, got %+v", tl.Name, tl.Annotations)
				}
				if tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint {
					t.Fatalf("tool %s must have DestructiveHint=false, got %+v", tl.Name, tl.Annotations)
				}
			}

			// Normal user via Bearer JWT also sees both list_users and get_profile.
			normalJWT := makeJWT(t, fix.server, "alice", "normal")
			userSess := connectMCPClient(t, fix.ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
			userList, err := userSess.ListTools(ctx, &mcp.ListToolsParams{})
			if err != nil {
				t.Fatalf("normal user ListTools failed: %v", err)
			}
			if got := strings.Join(toolNames(userList.Tools), ","); got != "get_profile,list_users" {
				t.Fatalf("normal user tools mismatch: got %q, want %q", got, "get_profile,list_users")
			}
		})
	}
}

func TestUsersEndpoints_HTTPVerbatimVsMCPRedacted(t *testing.T) {
	fix := setupUsersEndpointsTestServer(t, false)
	ctx := context.Background()
	httpClient := fix.ts.Client()

	normalJWT := makeJWT(t, fix.server, "alice", "normal")
	adminJWT := makeJWT(t, fix.server, "boss", "admin")

	adminSess := connectMCPClient(t, fix.ts.URL, map[string]string{"X-Token": fix.server.token})
	normalSess := connectMCPClient(t, fix.ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
	adminJWTSess := connectMCPClient(t, fix.ts.URL, map[string]string{"Authorization": "Bearer " + adminJWT})

	doHTTPGet := func(t *testing.T, path string, headers map[string]string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, fix.ts.URL+path, nil)
		if err != nil {
			t.Fatalf("new request %s: %v", path, err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("do request %s: %v", path, err)
		}
		defer resp.Body.Close()
		var buf json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&buf); err != nil {
			t.Fatalf("decode JSON from %s: %v", path, err)
		}
		return resp.StatusCode, buf
	}

	t.Run("admin GET /api/user returns real auth_token verbatim; MCP list_users redacts it", func(t *testing.T) {
		// HTTP admin view (default target=all -> both node-a and node-b).
		code, rawHTTP := doHTTPGet(t, "/api/user", map[string]string{"X-Token": fix.server.token})
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, string(rawHTTP))
		}
		var httpResult map[string][]map[string]any
		if err := json.Unmarshal(rawHTTP, &httpResult); err != nil {
			t.Fatalf("unmarshal HTTP /api/user: %v", err)
		}
		if len(httpResult["node-a"]) != 2 || len(httpResult["node-b"]) != 1 {
			t.Fatalf("expected both node-a (2 users) and node-b (1 user), got %+v", httpResult)
		}
		aliceHTTP := httpResult["node-a"][0]
		expectedAdminKeys := []string{
			"name", "auth_token", "expire_time", "downlink", "uplink", "role",
			"upload_bps", "download_bps", "max_clients", "client_recycle_delay_sec",
			"client_drain_sec", "group", "inbound_tags",
		}
		for _, k := range expectedAdminKeys {
			if _, ok := aliceHTTP[k]; !ok {
				t.Errorf("HTTP /api/user missing key %q: %+v", k, aliceHTTP)
			}
		}
		if aliceHTTP["auth_token"] != "alice-secret-auth-token-A" {
			t.Fatalf("HTTP /api/user must return real auth_token verbatim, got %#v", aliceHTTP["auth_token"])
		}
		if httpResult["node-a"][1]["auth_token"] != "" {
			t.Fatalf("HTTP /api/user empty auth_token mismatch: %#v", httpResult["node-a"][1]["auth_token"])
		}
		if httpResult["node-b"][0]["auth_token"] != "bob-secret-auth-token-B" {
			t.Fatalf("HTTP /api/user node-b must return real auth_token verbatim, got %#v", httpResult["node-b"][0]["auth_token"])
		}

		// MCP list_users (admin X-Token, omitted target -> default "all").
		mcpRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_users"})
		if err != nil {
			t.Fatalf("MCP list_users failed: %v", err)
		}
		if mcpRes.IsError {
			t.Fatalf("unexpected MCP IsError: %s", extractText(t, mcpRes))
		}
		mcpText := extractText(t, mcpRes)
		if strings.Contains(mcpText, "alice-secret-auth-token-A") || strings.Contains(mcpText, "bob-secret-auth-token-B") {
			t.Fatalf("MCP list_users leaked real auth_token in payload: %s", mcpText)
		}
		var mcpResult map[string][]map[string]any
		if err := json.Unmarshal([]byte(mcpText), &mcpResult); err != nil {
			t.Fatalf("unmarshal MCP list_users: %v", err)
		}
		if len(mcpResult["node-a"]) != 2 || len(mcpResult["node-b"]) != 1 {
			t.Fatalf("expected MCP default target=all to return both nodes, got %+v", mcpResult)
		}
		if mcpResult["node-a"][0]["auth_token"] != "[REDACTED]" {
			t.Fatalf("expected MCP alice auth_token=[REDACTED], got %#v", mcpResult["node-a"][0]["auth_token"])
		}
		if mcpResult["node-a"][1]["auth_token"] != "" {
			t.Fatalf("expected MCP empty auth_token preserved as empty string, got %#v", mcpResult["node-a"][1]["auth_token"])
		}
		if mcpResult["node-b"][0]["auth_token"] != "[REDACTED]" {
			t.Fatalf("expected MCP bob auth_token=[REDACTED], got %#v", mcpResult["node-b"][0]["auth_token"])
		}

		// Explicit target filter on both HTTP and MCP.
		codeSingle, rawSingle := doHTTPGet(t, "/api/user?target=node-a", map[string]string{"X-Token": fix.server.token})
		if codeSingle != http.StatusOK {
			t.Fatalf("expected 200 for target=node-a, got %d", codeSingle)
		}
		var singleHTTP map[string][]map[string]any
		if err := json.Unmarshal(rawSingle, &singleHTTP); err != nil {
			t.Fatalf("unmarshal single HTTP: %v", err)
		}
		if len(singleHTTP) != 1 || singleHTTP["node-a"][0]["auth_token"] != "alice-secret-auth-token-A" {
			t.Fatalf("unexpected single-node HTTP result: %+v", singleHTTP)
		}

		mcpSingleRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_users",
			Arguments: map[string]any{"target": "node-a"},
		})
		if err != nil || mcpSingleRes.IsError {
			t.Fatalf("MCP list_users target=node-a failed: err=%v res=%+v", err, mcpSingleRes)
		}
		var singleMCP map[string][]map[string]any
		if err := json.Unmarshal([]byte(extractText(t, mcpSingleRes)), &singleMCP); err != nil {
			t.Fatalf("unmarshal single MCP: %v", err)
		}
		if len(singleMCP) != 1 || singleMCP["node-a"][0]["auth_token"] != "[REDACTED]" {
			t.Fatalf("unexpected single-node MCP result: %+v", singleMCP)
		}
	})

	t.Run("normal user GET /api/user delegates to profile; HTTP verbatim vs MCP redacted", func(t *testing.T) {
		code, rawHTTP := doHTTPGet(t, "/api/user", map[string]string{"Authorization": "Bearer " + normalJWT})
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, string(rawHTTP))
		}
		var httpResult map[string][]map[string]any
		if err := json.Unmarshal(rawHTTP, &httpResult); err != nil {
			t.Fatalf("unmarshal HTTP normal /api/user: %v", err)
		}
		if httpResult["node-a"][0]["auth_token"] != "alice-secret-proxy-password-A" {
			t.Fatalf("expected HTTP normal /api/user real auth_token verbatim, got %#v", httpResult["node-a"][0]["auth_token"])
		}
		if httpResult["node-b"][0]["auth_token"] != "" {
			t.Fatalf("expected HTTP normal /api/user empty auth_token on node-b, got %#v", httpResult["node-b"][0]["auth_token"])
		}

		mcpRes, err := normalSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_users"})
		if err != nil {
			t.Fatalf("MCP normal list_users failed: %v", err)
		}
		if mcpRes.IsError {
			t.Fatalf("unexpected MCP IsError: %s", extractText(t, mcpRes))
		}
		mcpText := extractText(t, mcpRes)
		if strings.Contains(mcpText, "alice-secret-proxy-password-A") {
			t.Fatalf("MCP normal list_users leaked real auth_token: %s", mcpText)
		}
		var mcpResult map[string][]map[string]any
		if err := json.Unmarshal([]byte(mcpText), &mcpResult); err != nil {
			t.Fatalf("unmarshal MCP normal list_users: %v", err)
		}
		if mcpResult["node-a"][0]["auth_token"] != "[REDACTED]" {
			t.Fatalf("expected MCP normal list_users auth_token=[REDACTED], got %#v", mcpResult["node-a"][0]["auth_token"])
		}
		if mcpResult["node-b"][0]["auth_token"] != "" {
			t.Fatalf("expected MCP normal list_users empty auth_token preserved, got %#v", mcpResult["node-b"][0]["auth_token"])
		}
	})

	t.Run("GET /api/profile returns real auth_token verbatim; MCP get_profile redacts it", func(t *testing.T) {
		expectedProfileKeys := []string{
			"name", "expire_time", "downlink", "uplink", "role",
			"auth_token", "traffic_limit", "traffic_used_uplink",
			"traffic_used_downlink", "inbounds",
		}

		// Normal user JWT on HTTP /api/profile.
		code, rawHTTP := doHTTPGet(t, "/api/profile", map[string]string{"Authorization": "Bearer " + normalJWT})
		if code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", code, string(rawHTTP))
		}
		var httpProfile map[string][]map[string]any
		if err := json.Unmarshal(rawHTTP, &httpProfile); err != nil {
			t.Fatalf("unmarshal HTTP /api/profile: %v", err)
		}
		entry := httpProfile["node-a"][0]
		for _, k := range expectedProfileKeys {
			if _, ok := entry[k]; !ok {
				t.Errorf("HTTP /api/profile missing key %q: %+v", k, entry)
			}
		}
		if entry["auth_token"] != "alice-secret-proxy-password-A" {
			t.Fatalf("HTTP /api/profile must return real auth_token verbatim, got %#v", entry["auth_token"])
		}
		inbounds, ok := entry["inbounds"].([]any)
		if !ok || len(inbounds) != 1 {
			t.Fatalf("expected 1 inbound on node-a, got %#v", entry["inbounds"])
		}

		// Normal user JWT on MCP get_profile.
		mcpRes, err := normalSess.CallTool(ctx, &mcp.CallToolParams{Name: "get_profile"})
		if err != nil {
			t.Fatalf("MCP get_profile failed: %v", err)
		}
		if mcpRes.IsError {
			t.Fatalf("unexpected MCP IsError: %s", extractText(t, mcpRes))
		}
		mcpText := extractText(t, mcpRes)
		if strings.Contains(mcpText, "alice-secret-proxy-password-A") {
			t.Fatalf("MCP get_profile leaked real auth_token: %s", mcpText)
		}
		var mcpProfile map[string][]map[string]any
		if err := json.Unmarshal([]byte(mcpText), &mcpProfile); err != nil {
			t.Fatalf("unmarshal MCP get_profile: %v", err)
		}
		if mcpProfile["node-a"][0]["auth_token"] != "[REDACTED]" {
			t.Fatalf("expected MCP get_profile auth_token=[REDACTED], got %#v", mcpProfile["node-a"][0]["auth_token"])
		}
		if mcpProfile["node-b"][0]["auth_token"] != "" {
			t.Fatalf("expected MCP get_profile empty auth_token preserved, got %#v", mcpProfile["node-b"][0]["auth_token"])
		}

		// Admin JWT on HTTP vs MCP get_profile (admin also gets own profile).
		codeAdmin, rawAdminHTTP := doHTTPGet(t, "/api/profile?target=node-a", map[string]string{"Authorization": "Bearer " + adminJWT})
		if codeAdmin != http.StatusOK {
			t.Fatalf("expected 200 for admin JWT /api/profile, got %d", codeAdmin)
		}
		var adminHTTPProfile map[string][]map[string]any
		if err := json.Unmarshal(rawAdminHTTP, &adminHTTPProfile); err != nil {
			t.Fatalf("unmarshal admin HTTP profile: %v", err)
		}
		if adminHTTPProfile["node-a"][0]["auth_token"] != "boss-secret-proxy-password-A" {
			t.Fatalf("expected admin HTTP /api/profile real auth_token verbatim, got %#v", adminHTTPProfile["node-a"][0]["auth_token"])
		}

		adminMCPRes, err := adminJWTSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_profile",
			Arguments: map[string]any{"target": "node-a"},
		})
		if err != nil || adminMCPRes.IsError {
			t.Fatalf("admin JWT MCP get_profile failed: err=%v res=%+v", err, adminMCPRes)
		}
		adminMCPText := extractText(t, adminMCPRes)
		if strings.Contains(adminMCPText, "boss-secret-proxy-password-A") {
			t.Fatalf("admin JWT MCP get_profile leaked real auth_token: %s", adminMCPText)
		}
		var adminMCPProfile map[string][]map[string]any
		if err := json.Unmarshal([]byte(adminMCPText), &adminMCPProfile); err != nil {
			t.Fatalf("unmarshal admin MCP profile: %v", err)
		}
		if adminMCPProfile["node-a"][0]["auth_token"] != "[REDACTED]" {
			t.Fatalf("expected admin MCP get_profile auth_token=[REDACTED], got %#v", adminMCPProfile["node-a"][0]["auth_token"])
		}
	})

	t.Run("X-Token without username rejected with JWT required on HTTP and MCP get_profile", func(t *testing.T) {
		code, rawHTTP := doHTTPGet(t, "/api/profile", map[string]string{"X-Token": fix.server.token})
		if code != http.StatusUnauthorized {
			t.Fatalf("expected HTTP 401 for X-Token /api/profile, got %d: %s", code, string(rawHTTP))
		}
		var errBody struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(rawHTTP, &errBody); err != nil {
			t.Fatalf("unmarshal 401 body: %v", err)
		}
		if errBody.Code != 401 || errBody.Msg != "JWT required" {
			t.Fatalf("expected {code:401 msg:'JWT required'}, got %+v", errBody)
		}

		mcpRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "get_profile"})
		if err != nil {
			t.Fatalf("expected MCP tool error (IsError=true), got protocol error: %v", err)
		}
		if !mcpRes.IsError {
			t.Fatalf("expected IsError=true when calling get_profile with X-Token")
		}
		if got := extractText(t, mcpRes); !strings.Contains(got, "JWT required") {
			t.Fatalf("expected MCP error to contain 'JWT required', got %q", got)
		}
	})

	t.Run("no available node preserves 502 on admin /user and 404 on profile", func(t *testing.T) {
		// Admin GET /api/user -> 502
		code, raw := doHTTPGet(t, "/api/user?target=no-such-node", map[string]string{"X-Token": fix.server.token})
		if code != http.StatusBadGateway {
			t.Fatalf("expected 502 for admin /api/user with no node, got %d: %s", code, string(raw))
		}
		if !strings.Contains(string(raw), "no available node") {
			t.Fatalf("expected 'no available node' in body: %s", string(raw))
		}

		// Normal GET /api/user -> 404
		code, raw = doHTTPGet(t, "/api/user?target=no-such-node", map[string]string{"Authorization": "Bearer " + normalJWT})
		if code != http.StatusNotFound {
			t.Fatalf("expected 404 for normal /api/user with no node, got %d: %s", code, string(raw))
		}
		if !strings.Contains(string(raw), "no available node") {
			t.Fatalf("expected 'no available node' in body: %s", string(raw))
		}

		// GET /api/profile -> 404
		code, raw = doHTTPGet(t, "/api/profile?target=no-such-node", map[string]string{"Authorization": "Bearer " + normalJWT})
		if code != http.StatusNotFound {
			t.Fatalf("expected 404 for /api/profile with no node, got %d: %s", code, string(raw))
		}
		if !strings.Contains(string(raw), "no available node") {
			t.Fatalf("expected 'no available node' in body: %s", string(raw))
		}

		// MCP list_users / get_profile with nonexistent target -> IsError=true.
		res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "list_users",
			Arguments: map[string]any{"target": "no-such-node"},
		})
		if err != nil || !res.IsError || !strings.Contains(extractText(t, res), "no available node") {
			t.Fatalf("expected MCP list_users IsError 'no available node', err=%v res=%+v", err, res)
		}

		res, err = normalSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_profile",
			Arguments: map[string]any{"target": "no-such-node"},
		})
		if err != nil || !res.IsError || !strings.Contains(extractText(t, res), "no available node") {
			t.Fatalf("expected MCP get_profile IsError 'no available node', err=%v res=%+v", err, res)
		}
	})

	t.Run("legacy UserListHandler and ProfileHandler delegate and preserve exact help text", func(t *testing.T) {
		ulh := &UserListHandler{}
		ulh.setHttpServer(fix.server)
		ph := &ProfileHandler{}
		ph.setHttpServer(fix.server)

		wantUserHelp := `GET /api/user
	获取用户信息。管理员返回全部用户，普通用户返回自己的数据。
	query: target`
		if ulh.help() != wantUserHelp || fix.server.userListEndpoint().Help != wantUserHelp {
			t.Fatalf("UserListHandler help text changed:\nhandler=%q\nendpoint=%q", ulh.help(), fix.server.userListEndpoint().Help)
		}

		wantProfileHelp := `GET /api/profile
	获取当前登录用户的个人信息，不区分角色，始终返回完整 profile。
	query: target`
		if ph.help() != wantProfileHelp || fix.server.profileEndpoint().Help != wantProfileHelp {
			t.Fatalf("ProfileHandler help text changed:\nhandler=%q\nendpoint=%q", ph.help(), fix.server.profileEndpoint().Help)
		}

		mw := auth.AuthMiddleware(fix.server.token, fix.server.jwtSecret)

		// Direct UserListHandler invocation (admin) returns real auth_token verbatim.
		rUser := handlerEngine(http.MethodGet, "/user", ulh.handlerFunc, mw)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/user?target=node-a", nil)
		req.Header.Set("X-Token", fix.server.token)
		rUser.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "alice-secret-auth-token-A") {
			t.Fatalf("UserListHandler delegation failed: code=%d body=%s", w.Code, w.Body.String())
		}

		// Direct ProfileHandler invocation (JWT) returns real auth_token verbatim.
		rProfile := handlerEngine(http.MethodGet, "/profile", ph.handlerFunc, mw)
		w2 := httptest.NewRecorder()
		req2 := httptest.NewRequest(http.MethodGet, "/profile?target=node-a", nil)
		req2.Header.Set("Authorization", "Bearer "+normalJWT)
		rProfile.ServeHTTP(w2, req2)
		if w2.Code != http.StatusOK || !strings.Contains(w2.Body.String(), "alice-secret-proxy-password-A") {
			t.Fatalf("ProfileHandler delegation failed: code=%d body=%s", w2.Code, w2.Body.String())
		}

		// Direct ProfileHandler invocation with X-Token returns 401 JWT required.
		w3 := httptest.NewRecorder()
		req3 := httptest.NewRequest(http.MethodGet, "/profile", nil)
		req3.Header.Set("X-Token", fix.server.token)
		rProfile.ServeHTTP(w3, req3)
		if w3.Code != http.StatusUnauthorized || !strings.Contains(w3.Body.String(), "JWT required") {
			t.Fatalf("ProfileHandler X-Token 401 failed: code=%d body=%s", w3.Code, w3.Body.String())
		}
	})
}
