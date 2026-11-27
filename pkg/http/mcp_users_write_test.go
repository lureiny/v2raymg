package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lureiny/v2raymg/pkg/cluster"
	commonrpc "github.com/lureiny/v2raymg/pkg/common/rpc"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testUsersWriteClusterToken = "test-users-write-cluster-secret-token-123456"

type recordedResetAuthTokenCall struct {
	Username string
	NewToken string
}

type fakeUsersWriteRPCServer struct {
	proto.UnimplementedEndNodeAccessServer
	nodeName string

	mu                sync.Mutex
	addCalls          []*proto.User
	updateCalls       []*proto.User
	deleteCalls       []*proto.User
	resetCalls        []*proto.User
	resetTrafficCalls []*proto.User
	resetTokenCalls   []recordedResetAuthTokenCall

	addErrMsg          string
	updateErrMsg       string
	deleteErrMsg       string
	resetErrMsg        string
	resetTrafficErrMsg string
	resetTokenRPCErr   error
}

func (f *fakeUsersWriteRPCServer) resetRecorded() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addCalls = nil
	f.updateCalls = nil
	f.deleteCalls = nil
	f.resetCalls = nil
	f.resetTrafficCalls = nil
	f.resetTokenCalls = nil
	f.addErrMsg = ""
	f.updateErrMsg = ""
	f.deleteErrMsg = ""
	f.resetErrMsg = ""
	f.resetTrafficErrMsg = ""
	f.resetTokenRPCErr = nil
}

func (f *fakeUsersWriteRPCServer) AddUsers(_ context.Context, req *proto.UserOpReq) (*proto.UserOpRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addCalls = append(f.addCalls, req.GetUsers()...)
	if f.addErrMsg != "" {
		return &proto.UserOpRsp{Code: 500, Msg: f.addErrMsg}, nil
	}
	return &proto.UserOpRsp{Code: 0, Msg: "ok"}, nil
}

func (f *fakeUsersWriteRPCServer) UpdateUsers(_ context.Context, req *proto.UserOpReq) (*proto.UserOpRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCalls = append(f.updateCalls, req.GetUsers()...)
	if f.updateErrMsg != "" {
		return &proto.UserOpRsp{Code: 500, Msg: f.updateErrMsg}, nil
	}
	return &proto.UserOpRsp{Code: 0, Msg: "ok"}, nil
}

func (f *fakeUsersWriteRPCServer) DeleteUsers(_ context.Context, req *proto.UserOpReq) (*proto.UserOpRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls = append(f.deleteCalls, req.GetUsers()...)
	if f.deleteErrMsg != "" {
		return &proto.UserOpRsp{Code: 500, Msg: f.deleteErrMsg}, nil
	}
	return &proto.UserOpRsp{Code: 0, Msg: "ok"}, nil
}

func (f *fakeUsersWriteRPCServer) ResetUser(_ context.Context, req *proto.UserOpReq) (*proto.UserOpRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resetCalls = append(f.resetCalls, req.GetUsers()...)
	if f.resetErrMsg != "" {
		return &proto.UserOpRsp{Code: 500, Msg: f.resetErrMsg}, nil
	}
	return &proto.UserOpRsp{Code: 0, Msg: "ok"}, nil
}

func (f *fakeUsersWriteRPCServer) ResetUserTraffic(_ context.Context, req *proto.UserOpReq) (*proto.UserOpRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resetTrafficCalls = append(f.resetTrafficCalls, req.GetUsers()...)
	if f.resetTrafficErrMsg != "" {
		return &proto.UserOpRsp{Code: 500, Msg: f.resetTrafficErrMsg}, nil
	}
	return &proto.UserOpRsp{Code: 0, Msg: "ok"}, nil
}

func (f *fakeUsersWriteRPCServer) ResetAuthToken(_ context.Context, req *proto.ResetAuthTokenReq) (*proto.ResetAuthTokenRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resetTokenCalls = append(f.resetTokenCalls, recordedResetAuthTokenCall{
		Username: req.GetUsername(),
		NewToken: req.GetNewToken(),
	})
	if f.resetTokenRPCErr != nil {
		return nil, f.resetTokenRPCErr
	}
	return &proto.ResetAuthTokenRsp{
		Code:      0,
		Msg:       "ok",
		AuthToken: req.GetNewToken(),
	}, nil
}

type staticUsersWriteClusterNodes struct {
	nodes []*cluster.Node
	token string
}

func (s *staticUsersWriteClusterNodes) GetNodesWithFilter(f cluster.NodeFilter) []*cluster.Node {
	var out []*cluster.Node
	for _, n := range s.nodes {
		if f(n) {
			out = append(out, n)
		}
	}
	return out
}

func (s *staticUsersWriteClusterNodes) GetClusterToken() string {
	return s.token
}

func startFakeUsersWriteNode(t *testing.T, name string) (*cluster.Node, *fakeUsersWriteRPCServer) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake grpc write node %s: %v", name, err)
	}
	rpcImpl := &fakeUsersWriteRPCServer{nodeName: name}
	srv := grpc.NewServer(grpc.ForceServerCodec(commonrpc.NewEncryptMessageCodec(testUsersWriteClusterToken)))
	proto.RegisterEndNodeAccessServer(srv, rpcImpl)
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
	return node, rpcImpl
}

type usersWriteTestFixture struct {
	server *HttpServer
	ts     *httptest.Server
	rpcA   *fakeUsersWriteRPCServer
	rpcB   *fakeUsersWriteRPCServer
}

func setupUsersWriteTestServer(t *testing.T, readOnly bool) *usersWriteTestFixture {
	t.Helper()

	nodeA, rpcA := startFakeUsersWriteNode(t, "node-a")
	nodeB, rpcB := startFakeUsersWriteNode(t, "node-b")

	s := newTestHttpServer(nil)
	s.Name = "node-a"
	s.MCPReadOnly = readOnly
	s.localNode = &cluster.LocalNode{
		Node: proto.Node{
			Name:   "node-a",
			NodeId: "node-a-id",
		},
	}
	s.clusterNodes = &staticUsersWriteClusterNodes{
		nodes: []*cluster.Node{nodeA, nodeB},
		token: testUsersWriteClusterToken,
	}

	userGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret))
	registerEndpoint(s, userGroup, s.userResetAuthTokenEndpoint())

	adminGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret), auth.AdminOnly())
	registerEndpoint(s, adminGroup, s.userAddEndpoint())
	registerEndpoint(s, adminGroup, s.userUpdateEndpoint())
	registerEndpoint(s, adminGroup, s.userDeleteEndpoint())
	registerEndpoint(s, adminGroup, s.userResetEndpoint())
	registerEndpoint(s, adminGroup, s.userResetTrafficEndpoint())

	RegisterMCP(s)

	ts := httptest.NewServer(s.RestfulServer)
	t.Cleanup(ts.Close)
	return &usersWriteTestFixture{
		server: s,
		ts:     ts,
		rpcA:   rpcA,
		rpcB:   rpcB,
	}
}

func (f *usersWriteTestFixture) resetNodes() {
	f.rpcA.resetRecorded()
	f.rpcB.resetRecorded()
}

func (f *usersWriteTestFixture) doHTTP(t *testing.T, method, path string, body *string, headers map[string]string) (int, []byte) {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewBufferString(*body)
	}
	req, err := http.NewRequest(method, f.ts.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := f.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s %s: %v", method, path, err)
	}
	return resp.StatusCode, raw
}

func TestSanitizeResetAuthTokenForMCP_DeepCopyAndRedaction(t *testing.T) {
	t.Run("nil and empty maps", func(t *testing.T) {
		if got := sanitizeResetAuthTokenForMCP(nil); got != nil {
			t.Fatalf("expected nil for nil input, got %#v", got)
		}
		empty := map[string]any{}
		gotEmpty, ok := sanitizeResetAuthTokenForMCP(empty).(map[string]any)
		if !ok || gotEmpty == nil || len(gotEmpty) != 0 {
			t.Fatalf("expected non-nil empty map, got %#v", gotEmpty)
		}
	})

	t.Run("redacts non-empty auth_token, preserves empty/absent, never mutates original", func(t *testing.T) {
		nested := map[string]any{"node": "node-a"}
		orig := map[string]any{
			"code":       0,
			"msg":        "ok",
			"auth_token": "secret-uuid-token-1234",
			"meta":       nested,
		}

		sanitizedAny := sanitizeResetAuthTokenForMCP(orig)
		sanitized, ok := sanitizedAny.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", sanitizedAny)
		}

		if sanitized["auth_token"] != "[REDACTED]" {
			t.Fatalf("expected auth_token=[REDACTED], got %#v", sanitized["auth_token"])
		}
		if sanitized["code"] != 0 || sanitized["msg"] != "ok" {
			t.Fatalf("unexpected sanitized fields: %#v", sanitized)
		}

		// Original must remain completely untouched.
		if orig["auth_token"] != "secret-uuid-token-1234" {
			t.Fatalf("original auth_token was mutated in place: %#v", orig["auth_token"])
		}

		// Deep copy verification: mutating returned map or nested map does not mutate orig.
		sanitized["msg"] = "mutated"
		sanitized["extra"] = "leaked"
		sanitized["meta"].(map[string]any)["node"] = "mutated-node"
		if orig["msg"] != "ok" {
			t.Fatalf("mutating sanitized map affected original msg: %#v", orig["msg"])
		}
		if _, leaked := orig["extra"]; leaked {
			t.Fatalf("mutating sanitized map added key to original")
		}
		if nested["node"] != "node-a" {
			t.Fatalf("mutating sanitized nested map affected original: %#v", nested["node"])
		}

		// Empty auth_token preserved as empty string.
		emptyTok := sanitizeResetAuthTokenForMCP(map[string]any{"code": 0, "auth_token": ""}).(map[string]any)
		if emptyTok["auth_token"] != "" {
			t.Fatalf("expected empty auth_token preserved, got %#v", emptyTok["auth_token"])
		}

		// Absent auth_token remains absent.
		noTok := sanitizeResetAuthTokenForMCP(map[string]any{"code": 0, "msg": "ok"}).(map[string]any)
		if _, exists := noTok["auth_token"]; exists {
			t.Fatalf("expected absent auth_token key to remain absent")
		}
	})
}

func TestUsersWriteEndpoints_MCPVisibilityAndAnnotations(t *testing.T) {
	ctx := context.Background()

	t.Run("MCPReadOnly=true hides all 6 write tools from both admin and normal user", func(t *testing.T) {
		fix := setupUsersWriteTestServer(t, true)

		adminSess := connectMCPClient(t, fix.ts.URL, map[string]string{"X-Token": fix.server.token})
		adminList, err := adminSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("admin ListTools failed: %v", err)
		}
		if len(adminList.Tools) != 0 {
			t.Fatalf("expected 0 tools for admin in read-only mode, got %v", toolNames(adminList.Tools))
		}

		normalJWT := makeJWT(t, fix.server, "alice", "normal")
		userSess := connectMCPClient(t, fix.ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
		userList, err := userSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("normal user ListTools failed: %v", err)
		}
		if len(userList.Tools) != 0 {
			t.Fatalf("expected 0 tools for normal user in read-only mode, got %v", toolNames(userList.Tools))
		}
	})

	t.Run("MCPReadOnly=false exposes all 6 to admin with accurate hints and ONLY reset_auth_token to normal JWT", func(t *testing.T) {
		fix := setupUsersWriteTestServer(t, false)

		adminSess := connectMCPClient(t, fix.ts.URL, map[string]string{"X-Token": fix.server.token})
		adminList, err := adminSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("admin ListTools failed: %v", err)
		}

		wantAdmin := "add_user,delete_user,reset_auth_token,reset_user,reset_user_traffic,update_user"
		if got := strings.Join(toolNames(adminList.Tools), ","); got != wantAdmin {
			t.Fatalf("admin tools mismatch: got %q, want %q", got, wantAdmin)
		}

		wantDestructive := map[string]bool{
			"add_user":           false,
			"update_user":        false,
			"delete_user":        true,
			"reset_user":         true,
			"reset_user_traffic": true,
			"reset_auth_token":   true,
		}
		wantRequired := map[string][]string{
			"add_user":           {"user"},
			"update_user":        {"user"},
			"delete_user":        {"user"},
			"reset_user":         {"user"},
			"reset_user_traffic": {"user"},
			"reset_auth_token":   {},
		}

		for _, tl := range adminList.Tools {
			if tl.Annotations == nil {
				t.Fatalf("tool %s missing Annotations", tl.Name)
			}
			if tl.Annotations.ReadOnlyHint {
				t.Errorf("tool %s expected ReadOnlyHint=false, got true", tl.Name)
			}
			if tl.Annotations.DestructiveHint == nil {
				t.Errorf("tool %s missing DestructiveHint", tl.Name)
			} else if *tl.Annotations.DestructiveHint != wantDestructive[tl.Name] {
				t.Errorf("tool %s DestructiveHint=%v, want %v", tl.Name, *tl.Annotations.DestructiveHint, wantDestructive[tl.Name])
			}

			// Verify JSON schema required fields match omitempty conventions.
			schemaBytes, err := json.Marshal(tl.InputSchema)
			if err != nil {
				t.Fatalf("marshal schema for %s: %v", tl.Name, err)
			}
			var schemaObj struct {
				Required []string `json:"required"`
			}
			if err := json.Unmarshal(schemaBytes, &schemaObj); err != nil {
				t.Fatalf("unmarshal schema for %s: %v", tl.Name, err)
			}
			sort.Strings(schemaObj.Required)
			if strings.Join(schemaObj.Required, ",") != strings.Join(wantRequired[tl.Name], ",") {
				t.Errorf("tool %s required fields=%v, want %v (schema=%s)", tl.Name, schemaObj.Required, wantRequired[tl.Name], string(schemaBytes))
			}
		}

		// Normal user sees ONLY reset_auth_token.
		normalJWT := makeJWT(t, fix.server, "alice", "normal")
		userSess := connectMCPClient(t, fix.ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
		userList, err := userSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("normal user ListTools failed: %v", err)
		}
		if got := strings.Join(toolNames(userList.Tools), ","); got != "reset_auth_token" {
			t.Fatalf("normal user tools mismatch: got %q, want %q", got, "reset_auth_token")
		}

		// Normal user calling an admin write tool directly is rejected and does NOT hit RPC.
		_, err = userSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "delete_user",
			Arguments: map[string]any{"user": "alice"},
		})
		if err == nil {
			t.Fatalf("expected error when normal user calls admin tool delete_user")
		}
		fix.rpcA.mu.Lock()
		delCount := len(fix.rpcA.deleteCalls)
		fix.rpcA.mu.Unlock()
		if delCount != 0 {
			t.Fatalf("delete_user RPC must not run for normal user, got %d calls", delCount)
		}
	})
}

func TestUsersWriteEndpoints_HTTPBackwardsCompatAndMCPExecution(t *testing.T) {
	fix := setupUsersWriteTestServer(t, false)
	ctx := context.Background()

	adminHeaders := map[string]string{"X-Token": fix.server.token}
	normalJWT := makeJWT(t, fix.server, "alice", "normal")
	normalHeaders := map[string]string{"Authorization": "Bearer " + normalJWT}
	adminJWT := makeJWT(t, fix.server, "boss", "admin")

	adminSess := connectMCPClient(t, fix.ts.URL, adminHeaders)
	normalSess := connectMCPClient(t, fix.ts.URL, normalHeaders)
	adminJWTSess := connectMCPClient(t, fix.ts.URL, map[string]string{"Authorization": "Bearer " + adminJWT})

	strPtr := func(s string) *string { return &s }

	parseCodeMsg := func(t *testing.T, raw []byte) (int, string) {
		t.Helper()
		var body struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("unmarshal code/msg from %q: %v", string(raw), err)
		}
		return body.Code, body.Msg
	}

	t.Run("POST /api/user and MCP add_user", func(t *testing.T) {
		fix.resetNodes()

		// HTTP success with all fields, ttl taking precedence over expire, default target -> local node-a.
		before := time.Now().Unix()
		body := `{"user":"charlie","pwd":"pw123","expire":1000,"ttl":3600,"role":"normal","group":"vip","upload_bps":1024,"download_bps":2048,"max_clients":3,"client_recycle_delay_sec":15,"client_drain_sec":5}`
		statusCode, raw := fix.doHTTP(t, http.MethodPost, "/api/user", strPtr(body), adminHeaders)
		after := time.Now().Unix()
		if statusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", statusCode, string(raw))
		}
		code, msg := parseCodeMsg(t, raw)
		if code != 0 || msg != "ok" {
			t.Fatalf("expected {code:0 msg:ok}, got {%d %q}", code, msg)
		}
		fix.rpcA.mu.Lock()
		gotA := append([]*proto.User(nil), fix.rpcA.addCalls...)
		fix.rpcA.mu.Unlock()
		fix.rpcB.mu.Lock()
		gotB := append([]*proto.User(nil), fix.rpcB.addCalls...)
		fix.rpcB.mu.Unlock()
		if len(gotA) != 1 || len(gotB) != 0 {
			t.Fatalf("expected default target to hit only local node-a, got A=%d B=%d", len(gotA), len(gotB))
		}
		u := gotA[0]
		if u.GetName() != "charlie" || u.GetPasswd() != "pw123" || u.GetRole() != "normal" || u.GetGroup() != "vip" ||
			u.GetUploadBps() != 1024 || u.GetDownloadBps() != 2048 || u.GetMaxClients() != 3 ||
			u.GetClientRecycleDelaySec() != 15 || u.GetClientDrainSec() != 5 {
			t.Fatalf("unexpected proto.User forwarded to RPC: %+v", u)
		}
		if u.GetExpireTime() < before+3600 || u.GetExpireTime() > after+3600 {
			t.Fatalf("expected ttl to take precedence over expire, got expire_time=%d (now=%d..%d)", u.GetExpireTime(), before, after)
		}

		// HTTP malformed JSON -> 400.
		statusCode, raw = fix.doHTTP(t, http.MethodPost, "/api/user", strPtr(`{bad-json`), adminHeaders)
		if statusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for malformed JSON, got %d: %s", statusCode, string(raw))
		}

		// HTTP nonexistent target -> 502 "no available node".
		statusCode, raw = fix.doHTTP(t, http.MethodPost, "/api/user", strPtr(`{"target":"missing-node","user":"u1"}`), adminHeaders)
		if statusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d: %s", statusCode, string(raw))
		}
		code, msg = parseCodeMsg(t, raw)
		if code != 502 || msg != "no available node" {
			t.Fatalf("expected {502 'no available node'}, got {%d %q}", code, msg)
		}

		// HTTP RPC failure -> 500 with joinFailedList.
		fix.rpcB.mu.Lock()
		fix.rpcB.addErrMsg = "duplicate user"
		fix.rpcB.mu.Unlock()
		statusCode, raw = fix.doHTTP(t, http.MethodPost, "/api/user", strPtr(`{"target":"node-b","user":"u1"}`), adminHeaders)
		if statusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500 on RPC failure, got %d: %s", statusCode, string(raw))
		}
		code, msg = parseCodeMsg(t, raw)
		if code != 500 || !strings.Contains(msg, "node: node-b > err: duplicate user") {
			t.Fatalf("unexpected 500 body: code=%d msg=%q", code, msg)
		}

		// HTTP normal user -> 403.
		statusCode, _ = fix.doHTTP(t, http.MethodPost, "/api/user", strPtr(`{"user":"u2"}`), normalHeaders)
		if statusCode != http.StatusForbidden {
			t.Fatalf("expected 403 for normal user POST /api/user, got %d", statusCode)
		}

		// MCP add_user success (expire used when ttl omitted, explicit target=node-b).
		fix.resetNodes()
		mcpRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name: "add_user",
			Arguments: map[string]any{
				"target":                   "node-b",
				"user":                     "dave",
				"pwd":                      "secret",
				"expire":                   1893456000,
				"role":                     "normal",
				"group":                    "g1",
				"upload_bps":               500,
				"download_bps":             600,
				"max_clients":              2,
				"client_recycle_delay_sec": 10,
				"client_drain_sec":         4,
			},
		})
		if err != nil || mcpRes.IsError {
			t.Fatalf("MCP add_user failed: err=%v res=%+v", err, mcpRes)
		}
		code, msg = parseCodeMsg(t, []byte(extractText(t, mcpRes)))
		if code != 0 || msg != "ok" {
			t.Fatalf("unexpected MCP add_user result: code=%d msg=%q", code, msg)
		}
		fix.rpcB.mu.Lock()
		gotB = append([]*proto.User(nil), fix.rpcB.addCalls...)
		fix.rpcB.mu.Unlock()
		if len(gotB) != 1 || gotB[0].GetName() != "dave" || gotB[0].GetExpireTime() != 1893456000 || gotB[0].GetGroup() != "g1" {
			t.Fatalf("unexpected RPC call from MCP add_user: %+v", gotB)
		}

		// MCP add_user nonexistent target -> IsError=true.
		errRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "add_user",
			Arguments: map[string]any{"target": "ghost", "user": "dave"},
		})
		if err != nil || !errRes.IsError || !strings.Contains(extractText(t, errRes), "no available node") {
			t.Fatalf("expected MCP add_user IsError 'no available node', err=%v res=%+v", err, errRes)
		}
	})

	t.Run("PUT /api/user and MCP update_user", func(t *testing.T) {
		fix.resetNodes()

		// HTTP success with sentinel -1 values and default target -> node-a.
		body := `{"user":"alice","pwd":"newpw","expire":1900000000,"role":"admin","group":"ops","upload_bps":-1,"download_bps":4096,"max_clients":-1,"client_recycle_delay_sec":20,"client_drain_sec":8}`
		statusCode, raw := fix.doHTTP(t, http.MethodPut, "/api/user", strPtr(body), adminHeaders)
		if statusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", statusCode, string(raw))
		}
		code, msg := parseCodeMsg(t, raw)
		if code != 0 || msg != "ok" {
			t.Fatalf("expected {0 ok}, got {%d %q}", code, msg)
		}
		fix.rpcA.mu.Lock()
		gotA := append([]*proto.User(nil), fix.rpcA.updateCalls...)
		fix.rpcA.mu.Unlock()
		if len(gotA) != 1 || gotA[0].GetName() != "alice" || gotA[0].GetUploadBps() != -1 ||
			gotA[0].GetDownloadBps() != 4096 || gotA[0].GetMaxClients() != -1 || gotA[0].GetGroup() != "ops" {
			t.Fatalf("unexpected update RPC payload: %+v", gotA)
		}

		// HTTP malformed JSON -> 400.
		statusCode, _ = fix.doHTTP(t, http.MethodPut, "/api/user", strPtr(`not-json`), adminHeaders)
		if statusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", statusCode)
		}

		// HTTP nonexistent target -> 502.
		statusCode, _ = fix.doHTTP(t, http.MethodPut, "/api/user", strPtr(`{"target":"missing","user":"alice"}`), adminHeaders)
		if statusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d", statusCode)
		}

		// HTTP RPC failure -> 500.
		fix.rpcA.mu.Lock()
		fix.rpcA.updateErrMsg = "user not found"
		fix.rpcA.mu.Unlock()
		statusCode, raw = fix.doHTTP(t, http.MethodPut, "/api/user", strPtr(`{"user":"alice"}`), adminHeaders)
		if statusCode != http.StatusInternalServerError || !strings.Contains(string(raw), "user not found") {
			t.Fatalf("expected 500 with 'user not found', got %d: %s", statusCode, string(raw))
		}

		// MCP update_user success and error propagation.
		fix.resetNodes()
		mcpRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "update_user",
			Arguments: map[string]any{"target": "node-b", "user": "bob", "group": "pro", "upload_bps": 8192},
		})
		if err != nil || mcpRes.IsError {
			t.Fatalf("MCP update_user failed: err=%v res=%+v", err, mcpRes)
		}
		fix.rpcB.mu.Lock()
		gotB := append([]*proto.User(nil), fix.rpcB.updateCalls...)
		fix.rpcB.mu.Unlock()
		if len(gotB) != 1 || gotB[0].GetName() != "bob" || gotB[0].GetGroup() != "pro" || gotB[0].GetUploadBps() != 8192 {
			t.Fatalf("unexpected MCP update_user RPC call: %+v", gotB)
		}

		fix.rpcB.mu.Lock()
		fix.rpcB.updateErrMsg = "update boom"
		fix.rpcB.mu.Unlock()
		errRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "update_user",
			Arguments: map[string]any{"target": "node-b", "user": "bob"},
		})
		if err != nil || !errRes.IsError || !strings.Contains(extractText(t, errRes), "update boom") {
			t.Fatalf("expected MCP update_user IsError 'update boom', err=%v res=%+v", err, errRes)
		}
	})

	t.Run("DELETE /api/user and MCP delete_user", func(t *testing.T) {
		fix.resetNodes()

		// HTTP success -> defaults to local node-a.
		statusCode, raw := fix.doHTTP(t, http.MethodDelete, "/api/user", strPtr(`{"user":"alice"}`), adminHeaders)
		if statusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", statusCode, string(raw))
		}
		code, msg := parseCodeMsg(t, raw)
		if code != 0 || msg != "ok" {
			t.Fatalf("expected {0 ok}, got {%d %q}", code, msg)
		}
		fix.rpcA.mu.Lock()
		gotA := append([]*proto.User(nil), fix.rpcA.deleteCalls...)
		fix.rpcA.mu.Unlock()
		fix.rpcB.mu.Lock()
		gotB := append([]*proto.User(nil), fix.rpcB.deleteCalls...)
		fix.rpcB.mu.Unlock()
		if len(gotA) != 1 || gotA[0].GetName() != "alice" || len(gotB) != 0 {
			t.Fatalf("expected delete on node-a only, got A=%+v B=%+v", gotA, gotB)
		}

		// HTTP malformed JSON -> 400.
		statusCode, _ = fix.doHTTP(t, http.MethodDelete, "/api/user", strPtr(`{`), adminHeaders)
		if statusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", statusCode)
		}

		// HTTP nonexistent target -> 502.
		statusCode, _ = fix.doHTTP(t, http.MethodDelete, "/api/user", strPtr(`{"target":"missing","user":"alice"}`), adminHeaders)
		if statusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d", statusCode)
		}

		// HTTP RPC failure -> 500.
		fix.rpcA.mu.Lock()
		fix.rpcA.deleteErrMsg = "delete denied"
		fix.rpcA.mu.Unlock()
		statusCode, raw = fix.doHTTP(t, http.MethodDelete, "/api/user", strPtr(`{"user":"alice"}`), adminHeaders)
		if statusCode != http.StatusInternalServerError || !strings.Contains(string(raw), "delete denied") {
			t.Fatalf("expected 500 'delete denied', got %d: %s", statusCode, string(raw))
		}

		// MCP delete_user across target="all".
		fix.resetNodes()
		mcpRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "delete_user",
			Arguments: map[string]any{"target": "all", "user": "alice"},
		})
		if err != nil || mcpRes.IsError {
			t.Fatalf("MCP delete_user failed: err=%v res=%+v", err, mcpRes)
		}
		fix.rpcA.mu.Lock()
		cntA := len(fix.rpcA.deleteCalls)
		fix.rpcA.mu.Unlock()
		fix.rpcB.mu.Lock()
		cntB := len(fix.rpcB.deleteCalls)
		fix.rpcB.mu.Unlock()
		if cntA != 1 || cntB != 1 {
			t.Fatalf("expected target=all delete to hit both nodes, got A=%d B=%d", cntA, cntB)
		}
	})

	t.Run("POST /api/user/reset and MCP reset_user", func(t *testing.T) {
		fix.resetNodes()

		// HTTP missing user -> 400 "user is required".
		statusCode, raw := fix.doHTTP(t, http.MethodPost, "/api/user/reset", strPtr(`{"target":"node-a"}`), adminHeaders)
		if statusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for empty user, got %d: %s", statusCode, string(raw))
		}
		code, msg := parseCodeMsg(t, raw)
		if code != 400 || msg != "user is required" {
			t.Fatalf("expected {400 'user is required'}, got {%d %q}", code, msg)
		}

		// HTTP malformed JSON -> 400.
		statusCode, _ = fix.doHTTP(t, http.MethodPost, "/api/user/reset", strPtr(`{bad`), adminHeaders)
		if statusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", statusCode)
		}

		// HTTP nonexistent target -> 502.
		statusCode, _ = fix.doHTTP(t, http.MethodPost, "/api/user/reset", strPtr(`{"target":"missing","user":"alice"}`), adminHeaders)
		if statusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d", statusCode)
		}

		// HTTP success -> defaults to local node-a.
		statusCode, raw = fix.doHTTP(t, http.MethodPost, "/api/user/reset", strPtr(`{"user":"alice"}`), adminHeaders)
		if statusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", statusCode, string(raw))
		}
		code, msg = parseCodeMsg(t, raw)
		if code != 0 || msg != "ok" {
			t.Fatalf("expected {0 ok}, got {%d %q}", code, msg)
		}
		fix.rpcA.mu.Lock()
		gotA := append([]*proto.User(nil), fix.rpcA.resetCalls...)
		fix.rpcA.mu.Unlock()
		if len(gotA) != 1 || gotA[0].GetName() != "alice" {
			t.Fatalf("unexpected reset_user RPC call: %+v", gotA)
		}

		// HTTP RPC failure -> 500.
		fix.rpcA.mu.Lock()
		fix.rpcA.resetErrMsg = "reset failed"
		fix.rpcA.mu.Unlock()
		statusCode, raw = fix.doHTTP(t, http.MethodPost, "/api/user/reset", strPtr(`{"user":"alice"}`), adminHeaders)
		if statusCode != http.StatusInternalServerError || !strings.Contains(string(raw), "reset failed") {
			t.Fatalf("expected 500 'reset failed', got %d: %s", statusCode, string(raw))
		}

		// MCP reset_user empty user -> IsError "user is required".
		fix.resetNodes()
		errRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "reset_user",
			Arguments: map[string]any{"user": ""},
		})
		if err != nil || !errRes.IsError || !strings.Contains(extractText(t, errRes), "user is required") {
			t.Fatalf("expected MCP reset_user IsError 'user is required', err=%v res=%+v", err, errRes)
		}

		// MCP reset_user success.
		mcpRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "reset_user",
			Arguments: map[string]any{"user": "bob", "target": "node-b"},
		})
		if err != nil || mcpRes.IsError {
			t.Fatalf("MCP reset_user failed: err=%v res=%+v", err, mcpRes)
		}
		fix.rpcB.mu.Lock()
		gotB := append([]*proto.User(nil), fix.rpcB.resetCalls...)
		fix.rpcB.mu.Unlock()
		if len(gotB) != 1 || gotB[0].GetName() != "bob" {
			t.Fatalf("unexpected MCP reset_user RPC call: %+v", gotB)
		}
	})

	t.Run("POST /api/user/reset-traffic and MCP reset_user_traffic", func(t *testing.T) {
		fix.resetNodes()

		// HTTP missing user -> 400 "user is required".
		statusCode, raw := fix.doHTTP(t, http.MethodPost, "/api/user/reset-traffic", strPtr(`{}`), adminHeaders)
		if statusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", statusCode, string(raw))
		}
		code, msg := parseCodeMsg(t, raw)
		if code != 400 || msg != "user is required" {
			t.Fatalf("expected {400 'user is required'}, got {%d %q}", code, msg)
		}

		// HTTP nonexistent target -> 502.
		statusCode, _ = fix.doHTTP(t, http.MethodPost, "/api/user/reset-traffic", strPtr(`{"target":"missing","user":"alice"}`), adminHeaders)
		if statusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d", statusCode)
		}

		// HTTP success -> defaults to local node-a.
		statusCode, raw = fix.doHTTP(t, http.MethodPost, "/api/user/reset-traffic", strPtr(`{"user":"alice"}`), adminHeaders)
		if statusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", statusCode, string(raw))
		}
		code, msg = parseCodeMsg(t, raw)
		if code != 0 || msg != "ok" {
			t.Fatalf("expected {0 ok}, got {%d %q}", code, msg)
		}
		fix.rpcA.mu.Lock()
		gotA := append([]*proto.User(nil), fix.rpcA.resetTrafficCalls...)
		fix.rpcA.mu.Unlock()
		if len(gotA) != 1 || gotA[0].GetName() != "alice" {
			t.Fatalf("unexpected reset_user_traffic RPC call: %+v", gotA)
		}

		// HTTP RPC failure -> 500.
		fix.rpcA.mu.Lock()
		fix.rpcA.resetTrafficErrMsg = "traffic reset boom"
		fix.rpcA.mu.Unlock()
		statusCode, raw = fix.doHTTP(t, http.MethodPost, "/api/user/reset-traffic", strPtr(`{"user":"alice"}`), adminHeaders)
		if statusCode != http.StatusInternalServerError || !strings.Contains(string(raw), "traffic reset boom") {
			t.Fatalf("expected 500 'traffic reset boom', got %d: %s", statusCode, string(raw))
		}

		// MCP reset_user_traffic empty user -> IsError "user is required".
		fix.resetNodes()
		errRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "reset_user_traffic",
			Arguments: map[string]any{"user": ""},
		})
		if err != nil || !errRes.IsError || !strings.Contains(extractText(t, errRes), "user is required") {
			t.Fatalf("expected MCP reset_user_traffic IsError 'user is required', err=%v res=%+v", err, errRes)
		}

		// MCP reset_user_traffic success.
		mcpRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "reset_user_traffic",
			Arguments: map[string]any{"user": "alice"},
		})
		if err != nil || mcpRes.IsError {
			t.Fatalf("MCP reset_user_traffic failed: err=%v res=%+v", err, mcpRes)
		}
		fix.rpcA.mu.Lock()
		gotA = append([]*proto.User(nil), fix.rpcA.resetTrafficCalls...)
		fix.rpcA.mu.Unlock()
		if len(gotA) != 1 || gotA[0].GetName() != "alice" {
			t.Fatalf("unexpected MCP reset_user_traffic RPC call: %+v", gotA)
		}
	})

	t.Run("POST /api/user/reset-token and MCP reset_auth_token (HTTP verbatim vs MCP redacted, 401, HTTP 200 code=300)", func(t *testing.T) {
		fix.resetNodes()

		// 1. X-Token (no username) rejected with 401 "JWT required" on HTTP (with empty body) and IsError on MCP.
		statusCode, raw := fix.doHTTP(t, http.MethodPost, "/api/user/reset-token", nil, adminHeaders)
		if statusCode != http.StatusUnauthorized {
			t.Fatalf("expected 401 for X-Token /api/user/reset-token, got %d: %s", statusCode, string(raw))
		}
		code, msg := parseCodeMsg(t, raw)
		if code != 401 || msg != "JWT required" {
			t.Fatalf("expected {401 'JWT required'}, got {%d %q}", code, msg)
		}

		mcpXTokRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "reset_auth_token"})
		if err != nil {
			t.Fatalf("expected MCP tool error, got protocol error: %v", err)
		}
		if !mcpXTokRes.IsError || !strings.Contains(extractText(t, mcpXTokRes), "JWT required") {
			t.Fatalf("expected MCP IsError 'JWT required', got %+v", mcpXTokRes)
		}

		// 2. HTTP with normal JWT and EMPTY body (OptionalBody=true) returns real UUID verbatim.
		statusCode, raw = fix.doHTTP(t, http.MethodPost, "/api/user/reset-token", nil, normalHeaders)
		if statusCode != http.StatusOK {
			t.Fatalf("expected 200 for JWT reset-token empty body, got %d: %s", statusCode, string(raw))
		}
		var httpResp struct {
			Code      int    `json:"code"`
			Msg       string `json:"msg"`
			AuthToken string `json:"auth_token"`
		}
		if err := json.Unmarshal(raw, &httpResp); err != nil {
			t.Fatalf("unmarshal reset-token response: %v", err)
		}
		if httpResp.Code != 0 || httpResp.Msg != "ok" {
			t.Fatalf("expected {code:0 msg:ok}, got %+v", httpResp)
		}
		if _, err := uuid.Parse(httpResp.AuthToken); err != nil {
			t.Fatalf("expected valid UUID auth_token from HTTP, got %q: %v", httpResp.AuthToken, err)
		}
		fix.rpcA.mu.Lock()
		callsA := append([]recordedResetAuthTokenCall(nil), fix.rpcA.resetTokenCalls...)
		fix.rpcA.mu.Unlock()
		fix.rpcB.mu.Lock()
		callsB := append([]recordedResetAuthTokenCall(nil), fix.rpcB.resetTokenCalls...)
		fix.rpcB.mu.Unlock()
		if len(callsA) != 1 || len(callsB) != 0 {
			t.Fatalf("expected reset-token default target to hit only local node-a, got A=%d B=%d", len(callsA), len(callsB))
		}
		if callsA[0].Username != "alice" || callsA[0].NewToken != httpResp.AuthToken {
			t.Fatalf("HTTP returned token %q but RPC received %+v", httpResp.AuthToken, callsA[0])
		}

		// 3. MCP with normal JWT executes RPC with a real UUID but redacts auth_token to "[REDACTED]".
		fix.resetNodes()
		mcpRes, err := normalSess.CallTool(ctx, &mcp.CallToolParams{Name: "reset_auth_token"})
		if err != nil {
			t.Fatalf("MCP reset_auth_token failed: %v", err)
		}
		if mcpRes.IsError {
			t.Fatalf("unexpected MCP IsError: %s", extractText(t, mcpRes))
		}
		mcpText := extractText(t, mcpRes)
		fix.rpcA.mu.Lock()
		callsA = append([]recordedResetAuthTokenCall(nil), fix.rpcA.resetTokenCalls...)
		fix.rpcA.mu.Unlock()
		if len(callsA) != 1 || callsA[0].Username != "alice" {
			t.Fatalf("expected 1 RPC call for alice, got %+v", callsA)
		}
		realToken := callsA[0].NewToken
		if _, err := uuid.Parse(realToken); err != nil {
			t.Fatalf("expected RPC to receive real UUID, got %q: %v", realToken, err)
		}
		if strings.Contains(mcpText, realToken) {
			t.Fatalf("MCP reset_auth_token leaked plaintext auth_token %q in response: %s", realToken, mcpText)
		}
		var mcpResp map[string]any
		if err := json.Unmarshal([]byte(mcpText), &mcpResp); err != nil {
			t.Fatalf("unmarshal MCP reset_auth_token response: %v", err)
		}
		if mcpResp["auth_token"] != "[REDACTED]" || mcpResp["msg"] != "ok" || mcpResp["code"] != float64(0) {
			t.Fatalf("expected {code:0 msg:ok auth_token:[REDACTED]}, got %+v", mcpResp)
		}

		// 4. Admin JWT also works on MCP reset_auth_token and redacts auth_token.
		fix.resetNodes()
		adminMCPRes, err := adminJWTSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "reset_auth_token",
			Arguments: map[string]any{"target": "node-b"},
		})
		if err != nil || adminMCPRes.IsError {
			t.Fatalf("admin JWT MCP reset_auth_token failed: err=%v res=%+v", err, adminMCPRes)
		}
		fix.rpcB.mu.Lock()
		callsB = append([]recordedResetAuthTokenCall(nil), fix.rpcB.resetTokenCalls...)
		fix.rpcB.mu.Unlock()
		if len(callsB) != 1 || callsB[0].Username != "boss" {
			t.Fatalf("expected RPC call on node-b for boss, got %+v", callsB)
		}
		adminMCPText := extractText(t, adminMCPRes)
		if strings.Contains(adminMCPText, callsB[0].NewToken) || !strings.Contains(adminMCPText, "[REDACTED]") {
			t.Fatalf("admin JWT MCP reset_auth_token redaction failed: %s", adminMCPText)
		}

		// 5. No available node -> HTTP 200 with code=300; MCP IsError=true.
		statusCode, raw = fix.doHTTP(t, http.MethodPost, "/api/user/reset-token", strPtr(`{"target":"missing"}`), normalHeaders)
		if statusCode != http.StatusOK {
			t.Fatalf("expected legacy HTTP 200 on no available node for reset-token, got %d: %s", statusCode, string(raw))
		}
		code, msg = parseCodeMsg(t, raw)
		if code != 300 || msg != "no available node" {
			t.Fatalf("expected {code:300 msg:'no available node'}, got {%d %q}", code, msg)
		}

		mcpNoNode, err := normalSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "reset_auth_token",
			Arguments: map[string]any{"target": "missing"},
		})
		if err != nil || !mcpNoNode.IsError || !strings.Contains(extractText(t, mcpNoNode), "no available node") {
			t.Fatalf("expected MCP IsError 'no available node', err=%v res=%+v", err, mcpNoNode)
		}

		// 6. All target nodes fail -> HTTP 200 with code=300; MCP IsError=true.
		fix.resetNodes()
		fix.rpcB.mu.Lock()
		fix.rpcB.resetTokenRPCErr = status.Error(codes.Internal, "disk full")
		fix.rpcB.mu.Unlock()

		statusCode, raw = fix.doHTTP(t, http.MethodPost, "/api/user/reset-token", strPtr(`{"target":"node-b"}`), normalHeaders)
		if statusCode != http.StatusOK {
			t.Fatalf("expected legacy HTTP 200 when all nodes fail reset-token, got %d: %s", statusCode, string(raw))
		}
		code, msg = parseCodeMsg(t, raw)
		if code != 300 || !strings.Contains(msg, "disk full") {
			t.Fatalf("expected {code:300 msg:*disk full*}, got {%d %q}", code, msg)
		}

		mcpAllFail, err := normalSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "reset_auth_token",
			Arguments: map[string]any{"target": "node-b"},
		})
		if err != nil || !mcpAllFail.IsError || !strings.Contains(extractText(t, mcpAllFail), "disk full") {
			t.Fatalf("expected MCP IsError containing 'disk full', err=%v res=%+v", err, mcpAllFail)
		}

		// 7. Partial failure (target="all", node-a succeeds, node-b fails):
		// HTTP returns 200 code=0 with failed msg and REAL auth_token; MCP returns code=0 with "[REDACTED]".
		statusCode, raw = fix.doHTTP(t, http.MethodPost, "/api/user/reset-token", strPtr(`{"target":"all"}`), normalHeaders)
		if statusCode != http.StatusOK {
			t.Fatalf("expected 200 on partial failure, got %d: %s", statusCode, string(raw))
		}
		httpResp = struct {
			Code      int    `json:"code"`
			Msg       string `json:"msg"`
			AuthToken string `json:"auth_token"`
		}{}
		if err := json.Unmarshal(raw, &httpResp); err != nil {
			t.Fatalf("unmarshal partial failure HTTP response: %v", err)
		}
		if httpResp.Code != 0 || !strings.Contains(httpResp.Msg, "disk full") {
			t.Fatalf("expected partial failure {code:0 msg:*disk full*}, got %+v", httpResp)
		}
		if _, err := uuid.Parse(httpResp.AuthToken); err != nil {
			t.Fatalf("expected real UUID on HTTP partial failure, got %q", httpResp.AuthToken)
		}

		fix.rpcA.mu.Lock()
		fix.rpcA.resetTokenCalls = nil
		fix.rpcA.mu.Unlock()
		mcpPartial, err := normalSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "reset_auth_token",
			Arguments: map[string]any{"target": "all"},
		})
		if err != nil || mcpPartial.IsError {
			t.Fatalf("expected MCP partial failure to succeed with warning msg, err=%v res=%+v", err, mcpPartial)
		}
		mcpPartialText := extractText(t, mcpPartial)
		fix.rpcA.mu.Lock()
		partialRealToken := fix.rpcA.resetTokenCalls[0].NewToken
		fix.rpcA.mu.Unlock()
		if strings.Contains(mcpPartialText, partialRealToken) {
			t.Fatalf("MCP partial failure leaked real auth_token %q: %s", partialRealToken, mcpPartialText)
		}
		var partialObj map[string]any
		if err := json.Unmarshal([]byte(mcpPartialText), &partialObj); err != nil {
			t.Fatalf("unmarshal MCP partial response: %v", err)
		}
		if partialObj["code"] != float64(0) || partialObj["auth_token"] != "[REDACTED]" || !strings.Contains(partialObj["msg"].(string), "disk full") {
			t.Fatalf("unexpected MCP partial failure payload: %+v", partialObj)
		}
	})
}

func TestUsersWriteEndpoints_LegacyHandlerDelegationAndHelp(t *testing.T) {
	fix := setupUsersWriteTestServer(t, false)
	normalJWT := makeJWT(t, fix.server, "alice", "normal")
	mw := auth.AuthMiddleware(fix.server.token, fix.server.jwtSecret)

	addH := &UserAddHandler{}
	addH.setHttpServer(fix.server)
	updH := &UserUpdateHandler{}
	updH.setHttpServer(fix.server)
	delH := &UserDeleteHandler{}
	delH.setHttpServer(fix.server)
	rstH := &UserResetHandler{}
	rstH.setHttpServer(fix.server)
	rstTrH := &UserResetTrafficHandler{}
	rstTrH.setHttpServer(fix.server)
	rstTokH := &UserResetAuthTokenHandler{}
	rstTokH.setHttpServer(fix.server)

	helpChecks := []struct {
		name     string
		handler  HttpHandlerInterface
		epHelp   string
		wantPath string
		wantHelp string
	}{
		{
			name:     "UserAddHandler",
			handler:  addH,
			epHelp:   fix.server.userAddEndpoint().Help,
			wantPath: "/user",
			wantHelp: `POST /api/user
	添加用户
	body: {"target": "", "user": "", "pwd": "", "expire": 0, "ttl": 0, "role": "", "group": "", "inbound_tags": [], "upload_bps": 0, "download_bps": 0, "max_clients": 0, "client_recycle_delay_sec": 0, "client_drain_sec": 0}
	user: 用户名, pwd: password, expire: 过期时间戳(与ttl同时存在时优先使用ttl), ttl: 存活时间(秒), inbound_tags: Layer 2 访问标签数组`,
		},
		{
			name:     "UserUpdateHandler",
			handler:  updH,
			epHelp:   fix.server.userUpdateEndpoint().Help,
			wantPath: "/user",
			wantHelp: `PUT /api/user
	更新用户信息
	body: {"target": "", "user": "", "pwd": "", "expire": 0, "ttl": 0, "role": "", "group": "", "inbound_tags": [], "upload_bps": 0, "download_bps": 0, "max_clients": 0, "client_recycle_delay_sec": 0, "client_drain_sec": 0}
	inbound_tags: 省略或 null 保持不变, [] 清空全部标签, 非空全量替换; 配额字段 0 保持不变, -1 取消限制`,
		},
		{
			name:     "UserDeleteHandler",
			handler:  delH,
			epHelp:   fix.server.userDeleteEndpoint().Help,
			wantPath: "/user",
			wantHelp: `DELETE /api/user
	删除用户
	body: {"target": "", "user": ""}`,
		},
		{
			name:     "UserResetHandler",
			handler:  rstH,
			epHelp:   fix.server.userResetEndpoint().Help,
			wantPath: "/user/reset",
			wantHelp: `POST /api/user/reset
	重置用户proxy密钥
	body: {"target": "", "user": ""}`,
		},
		{
			name:     "UserResetTrafficHandler",
			handler:  rstTrH,
			epHelp:   fix.server.userResetTrafficEndpoint().Help,
			wantPath: "/user/reset-traffic",
			wantHelp: `POST /api/user/reset-traffic
	重置用户累计持久化流量统计 (TrafficTotalUplink/Downlink 清零)
	节点自治: 每个目标节点各自清零本地累计值; target 为空默认作用本机。
	body: {"target": "", "user": ""}`,
		},
		{
			name:     "UserResetAuthTokenHandler",
			handler:  rstTokH,
			epHelp:   fix.server.userResetAuthTokenEndpoint().Help,
			wantPath: "/user/reset-token",
			wantHelp: `POST /api/user/reset-token
	重置当前用户的auth token
	Body (optional): {"target":"node1"}
	  target: 目标节点名，空则默认本机`,
		},
	}

	for _, hc := range helpChecks {
		if hc.handler.getRelativePath() != hc.wantPath {
			t.Errorf("%s relativePath=%q, want %q", hc.name, hc.handler.getRelativePath(), hc.wantPath)
		}
		if hc.handler.help() != hc.wantHelp || hc.epHelp != hc.wantHelp {
			t.Errorf("%s help text mismatch:\nhandler=%q\nendpoint=%q\nwant=%q", hc.name, hc.handler.help(), hc.epHelp, hc.wantHelp)
		}
	}

	// Verify direct legacy handlerFunc delegation for all 6 structs.
	invoke := func(method, path string, h gin.HandlerFunc, body string, headers map[string]string) *httptest.ResponseRecorder {
		r := handlerEngine(method, path, h, mw)
		w := httptest.NewRecorder()
		var reader io.Reader
		if body != "" {
			reader = bytes.NewBufferString(body)
		}
		req := httptest.NewRequest(method, path, reader)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		r.ServeHTTP(w, req)
		return w
	}

	adminHdr := map[string]string{"X-Token": fix.server.token}
	jwtHdr := map[string]string{"Authorization": "Bearer " + normalJWT}

	if w := invoke(http.MethodPost, "/user", addH.handlerFunc, `{"user":"u1"}`, adminHdr); w.Code != http.StatusOK {
		t.Fatalf("UserAddHandler delegation failed: %d %s", w.Code, w.Body.String())
	}
	if w := invoke(http.MethodPut, "/user", updH.handlerFunc, `{"user":"u1"}`, adminHdr); w.Code != http.StatusOK {
		t.Fatalf("UserUpdateHandler delegation failed: %d %s", w.Code, w.Body.String())
	}
	if w := invoke(http.MethodDelete, "/user", delH.handlerFunc, `{"user":"u1"}`, adminHdr); w.Code != http.StatusOK {
		t.Fatalf("UserDeleteHandler delegation failed: %d %s", w.Code, w.Body.String())
	}
	if w := invoke(http.MethodPost, "/user/reset", rstH.handlerFunc, `{"user":"u1"}`, adminHdr); w.Code != http.StatusOK {
		t.Fatalf("UserResetHandler delegation failed: %d %s", w.Code, w.Body.String())
	}
	if w := invoke(http.MethodPost, "/user/reset", rstH.handlerFunc, `{}`, adminHdr); w.Code != http.StatusBadRequest {
		t.Fatalf("UserResetHandler missing user expected 400: %d %s", w.Code, w.Body.String())
	}
	if w := invoke(http.MethodPost, "/user/reset-traffic", rstTrH.handlerFunc, `{"user":"u1"}`, adminHdr); w.Code != http.StatusOK {
		t.Fatalf("UserResetTrafficHandler delegation failed: %d %s", w.Code, w.Body.String())
	}
	if w := invoke(http.MethodPost, "/user/reset-traffic", rstTrH.handlerFunc, `{}`, adminHdr); w.Code != http.StatusBadRequest {
		t.Fatalf("UserResetTrafficHandler missing user expected 400: %d %s", w.Code, w.Body.String())
	}
	if w := invoke(http.MethodPost, "/user/reset-token", rstTokH.handlerFunc, "", jwtHdr); w.Code != http.StatusOK {
		t.Fatalf("UserResetAuthTokenHandler JWT delegation failed: %d %s", w.Code, w.Body.String())
	} else {
		var body struct {
			Code      int    `json:"code"`
			AuthToken string `json:"auth_token"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != 0 {
			t.Fatalf("UserResetAuthTokenHandler unexpected body: %s", w.Body.String())
		}
		if _, err := uuid.Parse(body.AuthToken); err != nil {
			t.Fatalf("UserResetAuthTokenHandler must return real UUID verbatim, got %q", body.AuthToken)
		}
	}
	if w := invoke(http.MethodPost, "/user/reset-token", rstTokH.handlerFunc, "", adminHdr); w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "JWT required") {
		t.Fatalf("UserResetAuthTokenHandler X-Token expected 401 JWT required: %d %s", w.Code, w.Body.String())
	}
}
