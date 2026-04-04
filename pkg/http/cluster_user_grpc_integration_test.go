package http

// Integration tests for cluster_user and node_groups HTTP handlers.
// These tests spin up a minimal in-process gRPC stub server so that
// the handler's RPC dispatch path can be exercised without a full cluster.
//
// Approach:
//   - A stub gRPC server (no auth interceptor) implements GetNodeGroups and ListClusterUsers.
//   - A fake cluster.Node points to the stub server and is configured to pass
//     IsValid() and RegisteredRemote() checks.
//   - The encryption codec is registered globally (once, via init) using a test token.

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lureiny/v2raymg/pkg/cluster"
	commonrpc "github.com/lureiny/v2raymg/pkg/common/rpc"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

// ---------------------------------------------------------------------------
// Codec registration (once per test process)
// ---------------------------------------------------------------------------

const grpcTestToken = "grpc-test-token-32bytesXXXXXXXXX"

func init() {
	// Register the AES encryption codec that the RPC client uses via ForceCodec.
	// The token must match node.OutToken so client and server share the same key.
	encoding.RegisterCodec(commonrpc.NewEncryptMessageCodec(grpcTestToken))
}

// ---------------------------------------------------------------------------
// Minimal gRPC stub server (no auth interceptor)
// ---------------------------------------------------------------------------

type stubEndNodeServer struct {
	proto.UnimplementedEndNodeAccessServer
	groups []string
	users  []*proto.ClusterUserInfo
	// records last write call for assertion
	lastUpsertReq *proto.UpsertClusterUsersReq
	lastDeleteReq *proto.DeleteClusterUsersReq
	lastSetGroups []string
}

func (s *stubEndNodeServer) GetNodeGroups(_ context.Context, _ *proto.GetNodeGroupsReq) (*proto.GetNodeGroupsRsp, error) {
	return &proto.GetNodeGroupsRsp{Code: 0, Groups: s.groups}, nil
}

func (s *stubEndNodeServer) SetNodeGroups(_ context.Context, req *proto.SetNodeGroupsReq) (*proto.SetNodeGroupsRsp, error) {
	s.lastSetGroups = req.GetGroups()
	return &proto.SetNodeGroupsRsp{Code: 0}, nil
}

func (s *stubEndNodeServer) ListClusterUsers(_ context.Context, req *proto.ListClusterUsersReq) (*proto.ListClusterUsersRsp, error) {
	users := s.users
	if req.GetGroup() != "" {
		var filtered []*proto.ClusterUserInfo
		for _, u := range users {
			if u.TargetGroup == req.GetGroup() {
				filtered = append(filtered, u)
			}
		}
		users = filtered
	}
	return &proto.ListClusterUsersRsp{Code: 0, Users: users}, nil
}

func (s *stubEndNodeServer) UpsertClusterUsers(_ context.Context, req *proto.UpsertClusterUsersReq) (*proto.UpsertClusterUsersRsp, error) {
	s.lastUpsertReq = req
	return &proto.UpsertClusterUsersRsp{Code: 0}, nil
}

func (s *stubEndNodeServer) DeleteClusterUsers(_ context.Context, req *proto.DeleteClusterUsersReq) (*proto.DeleteClusterUsersRsp, error) {
	s.lastDeleteReq = req
	return &proto.DeleteClusterUsersRsp{Code: 0}, nil
}

// startStubGRPCServer starts a real gRPC server and returns the addr.
func startStubGRPCServer(t *testing.T, stub *stubEndNodeServer) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer() // no interceptor — auth is bypassed in test
	proto.RegisterEndNodeAccessServer(srv, stub)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.GracefulStop() })
	return ln.Addr().String()
}

// ---------------------------------------------------------------------------
// stubClusterNodesWithNode: returns a specific node when IsValid && name matches
// ---------------------------------------------------------------------------

type stubClusterNodesWithNode struct {
	node *cluster.Node
}

func (s *stubClusterNodesWithNode) GetNodesWithFilter(f cluster.NodeFilter) []*cluster.Node {
	if f(s.node) {
		return []*cluster.Node{s.node}
	}
	return nil
}
func (s *stubClusterNodesWithNode) GetClusterToken() string { return grpcTestToken }

// ---------------------------------------------------------------------------
// newIntegrationHttpServer: HttpServer wired to a fake node at the given gRPC addr
// ---------------------------------------------------------------------------

func newIntegrationHttpServer(t *testing.T, nodeName string, grpcAddr string) *HttpServer {
	t.Helper()
	tcpAddr, err := net.ResolveTCPAddr("tcp", grpcAddr)
	if err != nil {
		t.Fatalf("ResolveTCPAddr(%q): %v", grpcAddr, err)
	}

	now := time.Now().Unix()
	// Configure the node to pass:
	//   IsValid()        → CreateTime+NodeTimeOut > now (60s window)
	//   RegisteredRemote → OutToken != "" && ReportHeartBeatTime+NodeTimeOut > now
	fakeNode := &cluster.Node{
		Node: &proto.Node{
			Name: nodeName,
			Host: "127.0.0.1",
			Port: int32(tcpAddr.Port),
		},
		OutToken:            grpcTestToken,
		ReportHeartBeatTime: now,
		CreateTime:          now,
	}

	localNode := &cluster.LocalNode{
		Node:  proto.Node{Name: "local-test-node"},
		Token: grpcTestToken,
	}

	s := newTestHttpServer(nil)
	s.Name = nodeName
	s.localNode = localNode
	s.clusterNodes = &stubClusterNodesWithNode{node: fakeNode}
	return s
}

// ---------------------------------------------------------------------------
// NodeGroupsGetHandler — success path: JSON structure assertion
// ---------------------------------------------------------------------------

// TestNodeGroupsGetHandler_Success_JSONStructure: stub returns ["default","hk"] →
// handler serialises to {"groups":["default","hk"]} with HTTP 200.
func TestNodeGroupsGetHandler_Success_JSONStructure(t *testing.T) {
	stub := &stubEndNodeServer{groups: []string{"default", "hk"}}
	addr := startStubGRPCServer(t, stub)

	s := newIntegrationHttpServer(t, "test-node", addr)
	handler := &NodeGroupsGetHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("GET", "/node/:name/groups", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/node/test-node/groups", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Groups []string `json:"groups"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v — body=%s", err, w.Body.String())
	}
	if len(body.Groups) != 2 || body.Groups[0] != "default" || body.Groups[1] != "hk" {
		t.Errorf("expected groups=[default,hk], got %v", body.Groups)
	}
}

// ---------------------------------------------------------------------------
// ClusterUserListHandler — success path + group filter
// ---------------------------------------------------------------------------

// TestClusterUserListHandler_Success_JSONStructure: stub returns 2 users →
// handler serialises to {"users":[...]} with HTTP 200.
func TestClusterUserListHandler_Success_JSONStructure(t *testing.T) {
	stub := &stubEndNodeServer{
		users: []*proto.ClusterUserInfo{
			{Username: "alice", TargetGroup: "default"},
			{Username: "bob", TargetGroup: "hk"},
		},
	}
	addr := startStubGRPCServer(t, stub)

	s := newIntegrationHttpServer(t, "test-node", addr)
	handler := &ClusterUserListHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("GET", "/cluster-users", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/cluster-users", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Users []struct {
			Username string `json:"username"`
		} `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v — body=%s", err, w.Body.String())
	}
	if len(body.Users) != 2 {
		t.Errorf("expected 2 users, got %d", len(body.Users))
	}
}

// TestClusterUserListHandler_GroupFilter: ?group=hk filters to only bob.
func TestClusterUserListHandler_GroupFilter(t *testing.T) {
	stub := &stubEndNodeServer{
		users: []*proto.ClusterUserInfo{
			{Username: "alice", TargetGroup: "default"},
			{Username: "bob", TargetGroup: "hk"},
		},
	}
	addr := startStubGRPCServer(t, stub)

	s := newIntegrationHttpServer(t, "test-node", addr)
	handler := &ClusterUserListHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("GET", "/cluster-users", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/cluster-users?group=hk", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Users []struct {
			Username string `json:"username"`
		} `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v — body=%s", err, w.Body.String())
	}
	if len(body.Users) != 1 || body.Users[0].Username != "bob" {
		t.Errorf("expected only bob in group=hk, got %+v", body.Users)
	}
}

// ---------------------------------------------------------------------------
// ClusterUserAddHandler — write success (POST /cluster-users)
// ---------------------------------------------------------------------------

// TestClusterUserAddHandler_Success: valid input → stub receives Upsert with correct fields → 200 "Succ".
func TestClusterUserAddHandler_Success(t *testing.T) {
	stub := &stubEndNodeServer{}
	addr := startStubGRPCServer(t, stub)

	s := newIntegrationHttpServer(t, "test-node", addr)
	handler := &ClusterUserAddHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/cluster-users", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cluster-users", jsonBody(map[string]interface{}{
		"username": "carol",
		"password": "pw123",
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), "Succ") {
		t.Errorf("expected body to contain \"Succ\", got %q", w.Body.String())
	}
	// Assert the gRPC request content was correctly translated from HTTP fields.
	if stub.lastUpsertReq == nil {
		t.Fatal("stub did not receive an UpsertClusterUsers call")
	}
	if len(stub.lastUpsertReq.Users) != 1 {
		t.Fatalf("expected 1 user in UpsertReq, got %d", len(stub.lastUpsertReq.Users))
	}
	if stub.lastUpsertReq.Users[0].Username != "carol" {
		t.Errorf("expected username=carol in gRPC req, got %q", stub.lastUpsertReq.Users[0].Username)
	}
	if stub.lastUpsertReq.Users[0].Password != "pw123" {
		t.Errorf("expected password=pw123 in gRPC req, got %q", stub.lastUpsertReq.Users[0].Password)
	}
}

// ---------------------------------------------------------------------------
// ClusterUserUpdateHandler — write success (PUT /cluster-users/:name)
// ---------------------------------------------------------------------------

// TestClusterUserUpdateHandler_Success: path username is correctly carried into the gRPC request.
func TestClusterUserUpdateHandler_Success(t *testing.T) {
	stub := &stubEndNodeServer{}
	addr := startStubGRPCServer(t, stub)

	s := newIntegrationHttpServer(t, "test-node", addr)
	handler := &ClusterUserUpdateHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/cluster-users/:name", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/cluster-users/carol", jsonBody(map[string]interface{}{
		"password": "newpw",
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), "Succ") {
		t.Errorf("expected body to contain \"Succ\", got %q", w.Body.String())
	}
	// Assert path param ":name" was correctly mapped to Users[0].Username.
	if stub.lastUpsertReq == nil {
		t.Fatal("stub did not receive an UpsertClusterUsers call")
	}
	if len(stub.lastUpsertReq.Users) != 1 {
		t.Fatalf("expected 1 user in UpsertReq, got %d", len(stub.lastUpsertReq.Users))
	}
	if stub.lastUpsertReq.Users[0].Username != "carol" {
		t.Errorf("expected username=carol from path param, got %q", stub.lastUpsertReq.Users[0].Username)
	}
}

// ---------------------------------------------------------------------------
// ClusterUserDeleteHandler — write success (DELETE /cluster-users/:name)
// ---------------------------------------------------------------------------

// TestClusterUserDeleteHandler_Success: path username is correctly carried into DeleteClusterUsers.
func TestClusterUserDeleteHandler_Success(t *testing.T) {
	stub := &stubEndNodeServer{}
	addr := startStubGRPCServer(t, stub)

	s := newIntegrationHttpServer(t, "test-node", addr)
	handler := &ClusterUserDeleteHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("DELETE", "/cluster-users/:name", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/cluster-users/carol", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), "Succ") {
		t.Errorf("expected body to contain \"Succ\", got %q", w.Body.String())
	}
	// Assert the correct username was forwarded in the gRPC DeleteClusterUsers request.
	if stub.lastDeleteReq == nil {
		t.Fatal("stub did not receive a DeleteClusterUsers call")
	}
	if len(stub.lastDeleteReq.Usernames) != 1 || stub.lastDeleteReq.Usernames[0] != "carol" {
		t.Errorf("expected Usernames=[carol], got %v", stub.lastDeleteReq.Usernames)
	}
}

// ---------------------------------------------------------------------------
// NodeGroupsSetHandler — write success (PUT /node/:name/groups)
// ---------------------------------------------------------------------------

// TestNodeGroupsSetHandler_Success: groups are passed through unchanged to SetNodeGroups gRPC call.
func TestNodeGroupsSetHandler_Success(t *testing.T) {
	stub := &stubEndNodeServer{}
	addr := startStubGRPCServer(t, stub)

	s := newIntegrationHttpServer(t, "test-node", addr)
	handler := &NodeGroupsSetHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/node/:name/groups", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/node/test-node/groups", jsonBody(map[string]interface{}{
		"groups": []string{"default", "hk"},
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), "Succ") {
		t.Errorf("expected body to contain \"Succ\", got %q", w.Body.String())
	}
	// Assert the groups were forwarded unchanged to the gRPC SetNodeGroups call.
	if len(stub.lastSetGroups) != 2 || stub.lastSetGroups[0] != "default" || stub.lastSetGroups[1] != "hk" {
		t.Errorf("expected lastSetGroups=[default,hk], got %v", stub.lastSetGroups)
	}
}

// ---------------------------------------------------------------------------
// failedList aggregation: downstream RPC failure → error body (not "Succ")
// ---------------------------------------------------------------------------

// unreachableAddr returns an address that will immediately produce "connection refused".
// We listen, record the port, then close before the test uses it.
func unreachableAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // close immediately → connection refused on any dial
	return addr
}

// newFailingHttpServer creates an HttpServer whose node is "registered" but
// points to an unreachable address — forcing every RPC call into failedList.
func newFailingHttpServer(t *testing.T, nodeName string) *HttpServer {
	t.Helper()
	addr := unreachableAddr(t)
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		t.Fatalf("ResolveTCPAddr: %v", err)
	}
	now := time.Now().Unix()
	fakeNode := &cluster.Node{
		Node: &proto.Node{
			Name: nodeName,
			Host: "127.0.0.1",
			Port: int32(tcpAddr.Port),
		},
		OutToken:            grpcTestToken,
		ReportHeartBeatTime: now,
		CreateTime:          now,
	}
	localNode := &cluster.LocalNode{
		Node:  proto.Node{Name: "local-test-node"},
		Token: grpcTestToken,
	}
	s := newTestHttpServer(nil)
	s.Name = nodeName
	s.localNode = localNode
	s.clusterNodes = &stubClusterNodesWithNode{node: fakeNode}
	return s
}

// TestNodeGroupsGetHandler_DownstreamFailure_ErrorBody: node unreachable →
// failedList non-empty → HTTP 200 but body is an error message (not JSON groups).
func TestNodeGroupsGetHandler_DownstreamFailure_ErrorBody(t *testing.T) {
	s := newFailingHttpServer(t, "test-node")
	handler := &NodeGroupsGetHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("GET", "/node/:name/groups", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/node/test-node/groups", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for downstream failure, got %d", w.Code)
	}
	// Body should NOT be "no available node" (we have a node) and NOT be JSON.
	// It should be the aggregated error message from failedList.
	body := w.Body.String()
	if containsStr(body, "no available node") {
		t.Errorf("should not be 'no available node' — node exists but is unreachable")
	}
	if containsStr(body, `"groups"`) {
		t.Errorf("should not return JSON groups on downstream failure, got %q", body)
	}
}

// TestClusterUserAddHandler_DownstreamFailure_ErrorBody: node unreachable →
// failedList non-empty → body is error, not "Succ".
func TestClusterUserAddHandler_DownstreamFailure_ErrorBody(t *testing.T) {
	s := newFailingHttpServer(t, "test-node")
	handler := &ClusterUserAddHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/cluster-users", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cluster-users", jsonBody(map[string]interface{}{
		"username": "dave",
		"password": "pw",
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for downstream failure, got %d", w.Code)
	}
	body := w.Body.String()
	if containsStr(body, "Succ") {
		t.Errorf("should not return 'Succ' on downstream failure, got %q", body)
	}
	if containsStr(body, "no available node") {
		t.Errorf("should not be 'no available node' — node exists but unreachable")
	}
}

// TestClusterUserUpdateHandler_DownstreamFailure_ErrorBody: PUT /cluster-users/:name, node
// unreachable → failedList non-empty → body is error, not "Succ".
func TestClusterUserUpdateHandler_DownstreamFailure_ErrorBody(t *testing.T) {
	s := newFailingHttpServer(t, "test-node")
	handler := &ClusterUserUpdateHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/cluster-users/:name", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/cluster-users/alice", jsonBody(map[string]interface{}{
		"password": "newpw",
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for downstream failure, got %d", w.Code)
	}
	body := w.Body.String()
	if containsStr(body, "Succ") {
		t.Errorf("should not return 'Succ' on downstream failure, got %q", body)
	}
	if containsStr(body, "no available node") {
		t.Errorf("should not be 'no available node' — node exists but unreachable")
	}
}

// TestClusterUserDeleteHandler_DownstreamFailure_ErrorBody: DELETE /cluster-users/:name, node
// unreachable → failedList non-empty → body is error, not "Succ".
func TestClusterUserDeleteHandler_DownstreamFailure_ErrorBody(t *testing.T) {
	s := newFailingHttpServer(t, "test-node")
	handler := &ClusterUserDeleteHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("DELETE", "/cluster-users/:name", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/cluster-users/alice", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for downstream failure, got %d", w.Code)
	}
	body := w.Body.String()
	if containsStr(body, "Succ") {
		t.Errorf("should not return 'Succ' on downstream failure, got %q", body)
	}
	if containsStr(body, "no available node") {
		t.Errorf("should not be 'no available node' — node exists but unreachable")
	}
}

// TestNodeGroupsSetHandler_DownstreamFailure_ErrorBody: PUT /node/:name/groups, node
// unreachable → failedList non-empty → body is error, not "Succ".
func TestNodeGroupsSetHandler_DownstreamFailure_ErrorBody(t *testing.T) {
	s := newFailingHttpServer(t, "test-node")
	handler := &NodeGroupsSetHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/node/:name/groups", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/node/test-node/groups", jsonBody(map[string]interface{}{
		"groups": []string{"default"},
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for downstream failure, got %d", w.Code)
	}
	body := w.Body.String()
	if containsStr(body, "Succ") {
		t.Errorf("should not return 'Succ' on downstream failure, got %q", body)
	}
	if containsStr(body, "no available node") {
		t.Errorf("should not be 'no available node' — node exists but unreachable")
	}
}
