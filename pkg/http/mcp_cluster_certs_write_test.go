package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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

const clusterCertsWriteTestToken = "cluster-certs-write-secret-token-123456"

type clusterCertsWriteFakeRPCServer struct {
	proto.UnimplementedEndNodeAccessServer

	mu sync.Mutex

	// ObtainNewCert
	obtainCertErr   string
	obtainedDomains []string
	// TransferCert
	transferCertErr  string
	transferredCerts []*proto.TransferCertReq
	// DeleteCert
	deleteCertErr  string
	deletedDomains []string
	// SetNodeGroups
	setNodeGroupsErr  string
	setNodeGroupsArgs [][]string
	// SetGatewayModel
	setGatewayErr  string
	setGatewayArgs []bool
	// SetPingCheck
	setPingCheckErr  string
	setPingCheckArgs []bool
	// GetUsers / AddUsers
	getUsersErr       string
	users             []*proto.User
	addUsersErr       string
	addedUsersBatches [][]*proto.User
	// UpdateProxy
	updateProxyErr  string
	updateProxyTags []string
}

func (f *clusterCertsWriteFakeRPCServer) ObtainNewCert(ctx context.Context, req *proto.ObtainNewCertReq) (*proto.ObtainNewCertRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.obtainedDomains = append(f.obtainedDomains, req.GetDomain())
	if f.obtainCertErr != "" {
		return &proto.ObtainNewCertRsp{Code: 500, Msg: f.obtainCertErr}, nil
	}
	return &proto.ObtainNewCertRsp{Code: 0, Msg: "ok"}, nil
}

func (f *clusterCertsWriteFakeRPCServer) TransferCert(ctx context.Context, req *proto.TransferCertReq) (*proto.TransferCertRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.transferredCerts = append(f.transferredCerts, &proto.TransferCertReq{
		Domain:   req.GetDomain(),
		CertData: append([]byte(nil), req.GetCertData()...),
		KeyDatas: append([]byte(nil), req.GetKeyDatas()...),
	})
	if f.transferCertErr != "" {
		return &proto.TransferCertRsp{Code: 500, Msg: f.transferCertErr}, nil
	}
	return &proto.TransferCertRsp{Code: 0, Msg: "ok"}, nil
}

func (f *clusterCertsWriteFakeRPCServer) DeleteCert(ctx context.Context, req *proto.DeleteCertReq) (*proto.DeleteCertRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletedDomains = append(f.deletedDomains, req.GetDomain())
	if f.deleteCertErr != "" {
		return &proto.DeleteCertRsp{Code: 500, Msg: f.deleteCertErr}, nil
	}
	return &proto.DeleteCertRsp{Code: 0, Msg: "ok"}, nil
}

func (f *clusterCertsWriteFakeRPCServer) SetNodeGroups(ctx context.Context, req *proto.SetNodeGroupsReq) (*proto.SetNodeGroupsRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	groupsCopy := append([]string(nil), req.GetGroups()...)
	f.setNodeGroupsArgs = append(f.setNodeGroupsArgs, groupsCopy)
	if f.setNodeGroupsErr != "" {
		return &proto.SetNodeGroupsRsp{Code: 500, Msg: f.setNodeGroupsErr}, nil
	}
	return &proto.SetNodeGroupsRsp{Code: 0, Msg: "ok"}, nil
}

func (f *clusterCertsWriteFakeRPCServer) SetGatewayModel(ctx context.Context, req *proto.SetGatewayModelReq) (*proto.SetGatewayModelRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setGatewayArgs = append(f.setGatewayArgs, req.GetEnableGatewayModel())
	if f.setGatewayErr != "" {
		return &proto.SetGatewayModelRsp{Code: 500, Msg: f.setGatewayErr}, nil
	}
	return &proto.SetGatewayModelRsp{Code: 0, Msg: "ok"}, nil
}

func (f *clusterCertsWriteFakeRPCServer) SetPingCheck(ctx context.Context, req *proto.SetPingCheckReq) (*proto.SetPingCheckRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setPingCheckArgs = append(f.setPingCheckArgs, req.GetEnablePingCheck())
	if f.setPingCheckErr != "" {
		return &proto.SetPingCheckRsp{Code: 500, Msg: f.setPingCheckErr}, nil
	}
	return &proto.SetPingCheckRsp{Code: 0, Msg: "ok"}, nil
}

func (f *clusterCertsWriteFakeRPCServer) GetUsers(ctx context.Context, req *proto.GetUsersReq) (*proto.GetUsersRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getUsersErr != "" {
		return &proto.GetUsersRsp{Code: 500, Msg: f.getUsersErr}, nil
	}
	return &proto.GetUsersRsp{Code: 0, Users: f.users}, nil
}

func (f *clusterCertsWriteFakeRPCServer) AddUsers(ctx context.Context, req *proto.UserOpReq) (*proto.UserOpRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addedUsersBatches = append(f.addedUsersBatches, req.GetUsers())
	if f.addUsersErr != "" {
		return &proto.UserOpRsp{Code: 500, Msg: f.addUsersErr}, nil
	}
	return &proto.UserOpRsp{Code: 0, Msg: "ok"}, nil
}

func (f *clusterCertsWriteFakeRPCServer) UpdateProxy(ctx context.Context, req *proto.UpdateProxyReq) (*proto.UpdateProxyRsp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateProxyTags = append(f.updateProxyTags, req.GetTag())
	if f.updateProxyErr != "" {
		return &proto.UpdateProxyRsp{Code: 500, Msg: f.updateProxyErr}, nil
	}
	return &proto.UpdateProxyRsp{Code: 0, Msg: "ok"}, nil
}

type clusterCertsWriteCertReader struct {
	files map[string][2]string
}

func (r *clusterCertsWriteCertReader) GetCertFiles(domain string) (string, string, bool) {
	pair, ok := r.files[domain]
	if !ok {
		return "", "", false
	}
	return pair[0], pair[1], true
}

type clusterCertsWriteClusterNodes struct {
	nodes []*cluster.Node
	token string
}

func (c *clusterCertsWriteClusterNodes) GetNodesWithFilter(f cluster.NodeFilter) []*cluster.Node {
	var out []*cluster.Node
	for _, n := range c.nodes {
		if f(n) {
			out = append(out, n)
		}
	}
	return out
}

func (c *clusterCertsWriteClusterNodes) GetClusterToken() string {
	return c.token
}

func startClusterCertsWriteFakeNode(t *testing.T, name, nodeID string, srv *clusterCertsWriteFakeRPCServer) *cluster.Node {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake grpc node %s: %v", name, err)
	}
	gs := grpc.NewServer(grpc.ForceServerCodec(commonrpc.NewEncryptMessageCodec(clusterCertsWriteTestToken)))
	proto.RegisterEndNodeAccessServer(gs, srv)
	go func() {
		_ = gs.Serve(lis)
	}()

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
	n.SetOutToken("test-out-token")
	n.SetReportHeartBeatTime(now)
	n.SetRecvHeartBeatTime(now)

	t.Cleanup(func() {
		n.CloseConn()
		gs.Stop()
		_ = lis.Close()
	})
	return n
}

func setupClusterCertsWriteServer(t *testing.T, localName string, nodes []*cluster.Node, cr CertReader, readOnly bool) (*HttpServer, *httptest.Server) {
	t.Helper()
	s := newTestHttpServer(nil)
	s.Name = localName
	s.MCPReadOnly = readOnly
	s.certReader = cr
	s.localNode = &cluster.LocalNode{
		Node: proto.Node{
			Name:   localName,
			Host:   "127.0.0.1",
			Port:   19998,
			NodeId: "id-" + localName,
		},
		Token: "local-token",
	}
	s.clusterNodes = &clusterCertsWriteClusterNodes{
		nodes: nodes,
		token: clusterCertsWriteTestToken,
	}

	adminGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret), auth.AdminOnly())
	registerEndpoint(s, adminGroup, s.applyCertEndpoint())
	registerEndpoint(s, adminGroup, s.transferCertEndpoint())
	registerEndpoint(s, adminGroup, s.deleteCertEndpoint())
	registerEndpoint(s, adminGroup, s.nodeGroupsSetEndpoint())
	registerEndpoint(s, adminGroup, s.setGatewayEndpoint())
	registerEndpoint(s, adminGroup, s.setPingCheckEndpoint())
	registerEndpoint(s, adminGroup, s.copyUserBetweenNodesEndpoint())
	registerEndpoint(s, adminGroup, s.updateNodeEndpoint())

	RegisterMCP(s)

	ts := httptest.NewServer(s.RestfulServer)
	t.Cleanup(ts.Close)
	return s, ts
}

func clusterCertsWriteHTTPDo(t *testing.T, method, url, body string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, url, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp, data
}

func assertClusterCertsWriteOKJSON(t *testing.T, body []byte) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal OK JSON %q: %v", string(body), err)
	}
	if got["code"] != float64(0) || got["msg"] != "ok" || len(got) != 2 {
		t.Fatalf("expected {\"code\":0,\"msg\":\"ok\"}, got %s", string(body))
	}
}

func assertClusterCertsWriteErrJSON(t *testing.T, body []byte, wantCode int, wantSubstr string) {
	t.Helper()
	var got struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal error JSON %q: %v", string(body), err)
	}
	if got.Code != wantCode {
		t.Fatalf("expected error code %d, got %d (body=%s)", wantCode, got.Code, string(body))
	}
	if wantSubstr != "" && !strings.Contains(got.Msg, wantSubstr) {
		t.Fatalf("expected error msg to contain %q, got %q", wantSubstr, got.Msg)
	}
}

func TestClusterCertsWrite_HTTPBackwardsCompat_AndLegacyHandlers(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "example.com.crt")
	keyPath := filepath.Join(tmpDir, "example.com.key")
	if err := os.WriteFile(certPath, []byte("CERT-PEM-DATA"), 0600); err != nil {
		t.Fatalf("write cert file: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("KEY-PEM-DATA"), 0600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	cr := &clusterCertsWriteCertReader{
		files: map[string][2]string{
			"example.com":  {certPath, keyPath},
			"bad-cert.com": {filepath.Join(tmpDir, "missing.crt"), keyPath},
			"bad-key.com":  {certPath, filepath.Join(tmpDir, "missing.key")},
		},
	}

	localRPC := &clusterCertsWriteFakeRPCServer{
		users: []*proto.User{
			{Name: "alice", AuthToken: "tok-a", Uplink: 1234, Downlink: 5678},
			{Name: "bob", AuthToken: "tok-b", Uplink: 99, Downlink: 88},
		},
	}
	peerRPC := &clusterCertsWriteFakeRPCServer{
		users: nil, // empty user list case
	}
	errRPC := &clusterCertsWriteFakeRPCServer{
		obtainCertErr:    "acme failed",
		transferCertErr:  "disk full",
		deleteCertErr:    "cert busy",
		setNodeGroupsErr: "groups db err",
		setGatewayErr:    "gateway err",
		setPingCheckErr:  "ping err",
		getUsersErr:      "src users err",
		addUsersErr:      "dst add err",
		updateProxyErr:   "download failed",
	}

	localNode := startClusterCertsWriteFakeNode(t, "node-local", "id-node-local", localRPC)
	peerNode := startClusterCertsWriteFakeNode(t, "node-peer", "id-node-peer", peerRPC)
	errNode := startClusterCertsWriteFakeNode(t, "node-err", "id-node-err", errRPC)

	s, ts := setupClusterCertsWriteServer(t, "node-local", []*cluster.Node{localNode, peerNode, errNode}, cr, false)
	adminHeaders := map[string]string{"X-Token": s.token}

	t.Run("normal JWT forbidden on all 8 admin HTTP routes", func(t *testing.T) {
		normalJWT := makeJWT(t, s, "alice", "normal")
		userHeaders := map[string]string{"Authorization": "Bearer " + normalJWT}
		routes := []struct {
			method string
			path   string
		}{
			{http.MethodPost, "/api/cert"},
			{http.MethodPost, "/api/cert/transfer"},
			{http.MethodDelete, "/api/cert"},
			{http.MethodPut, "/api/node/node-peer/groups"},
			{http.MethodPut, "/api/gateway"},
			{http.MethodPut, "/api/pingCheck"},
			{http.MethodPost, "/api/copyUserBetweenNodes"},
			{http.MethodPost, "/api/update"},
		}
		for _, rt := range routes {
			resp, _ := clusterCertsWriteHTTPDo(t, rt.method, ts.URL+rt.path, `{}`, userHeaders)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("%s %s: expected 403 for normal JWT, got %d", rt.method, rt.path, resp.StatusCode)
			}
		}
	})

	t.Run("POST /api/cert apply_cert", func(t *testing.T) {
		// 400 invalid JSON
		resp, body := clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/cert", "bad-json", adminHeaders)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", resp.StatusCode, body)
		}
		// 502 no available node
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/cert", `{"target":"missing","domain":"a.com"}`, adminHeaders)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 502, "no available node")
		// 500 RPC failure
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/cert", `{"target":"node-err","domain":"a.com"}`, adminHeaders)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "acme failed")
		// 200 success (empty target -> defaults to node-local)
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/cert", `{"domain":"example.com"}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteOKJSON(t, body)
		localRPC.mu.Lock()
		gotDomains := append([]string(nil), localRPC.obtainedDomains...)
		localRPC.mu.Unlock()
		if !reflect.DeepEqual(gotDomains, []string{"example.com"}) {
			t.Fatalf("expected ObtainNewCert domain [example.com], got %v", gotDomains)
		}
	})

	t.Run("POST /api/cert/transfer transfer_cert", func(t *testing.T) {
		// 400 invalid JSON
		resp, _ := clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/cert/transfer", "bad", adminHeaders)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		// 404 cert not found
		resp, body := clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/cert/transfer", `{"target":"node-peer","domain":"unknown.com"}`, adminHeaders)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 404, "can't find domain's[unknown.com] cert")
		// 500 read cert file err
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/cert/transfer", `{"target":"node-peer","domain":"bad-cert.com"}`, adminHeaders)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "read cert file err")
		// 500 read key file err
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/cert/transfer", `{"target":"node-peer","domain":"bad-key.com"}`, adminHeaders)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "read key file err")
		// 502 self-only target filtered out (empty target -> node-local -> filtered out)
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/cert/transfer", `{"domain":"example.com"}`, adminHeaders)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("expected 502 when only self matches, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 502, "no available node")
		// 200 success with target="all": self filtered out, peer succeeds, errNode fails -> succList contains only node-peer
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/cert/transfer", `{"target":"all","domain":"example.com"}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		var succMap map[string]any
		if err := json.Unmarshal(body, &succMap); err != nil {
			t.Fatalf("unmarshal succList: %v body=%s", err, body)
		}
		if _, ok := succMap["node-peer"]; !ok {
			t.Fatalf("expected node-peer in succList, got %+v", succMap)
		}
		if _, hasSelf := succMap["node-local"]; hasSelf {
			t.Fatalf("self node-local must be filtered out, got %+v", succMap)
		}
		if _, hasErr := succMap["node-err"]; hasErr {
			t.Fatalf("failed node-err must not be in succList, got %+v", succMap)
		}
		localRPC.mu.Lock()
		localTransfers := len(localRPC.transferredCerts)
		localRPC.mu.Unlock()
		if localTransfers != 0 {
			t.Fatalf("self node must not receive TransferCert RPC, got %d calls", localTransfers)
		}
		peerRPC.mu.Lock()
		if len(peerRPC.transferredCerts) != 1 ||
			peerRPC.transferredCerts[0].Domain != "example.com" ||
			string(peerRPC.transferredCerts[0].CertData) != "CERT-PEM-DATA" ||
			string(peerRPC.transferredCerts[0].KeyDatas) != "KEY-PEM-DATA" {
			t.Fatalf("unexpected peer transferred cert: %+v", peerRPC.transferredCerts)
		}
		peerRPC.mu.Unlock()

		// 500 certReader == nil
		sNoCR, tsNoCR := setupClusterCertsWriteServer(t, "node-local", []*cluster.Node{peerNode}, nil, false)
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, tsNoCR.URL+"/api/cert/transfer", `{"target":"node-peer","domain":"example.com"}`, map[string]string{"X-Token": sNoCR.token})
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500 when certReader is nil, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "cert reader not configured")
	})

	t.Run("DELETE /api/cert delete_cert", func(t *testing.T) {
		// 400 invalid JSON
		resp, _ := clusterCertsWriteHTTPDo(t, http.MethodDelete, ts.URL+"/api/cert", "not-json", adminHeaders)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		// 400 missing domain
		resp, body := clusterCertsWriteHTTPDo(t, http.MethodDelete, ts.URL+"/api/cert", `{"target":"node-peer"}`, adminHeaders)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 400, "domain is required")
		// 502 no available node
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodDelete, ts.URL+"/api/cert", `{"target":"missing","domain":"example.com"}`, adminHeaders)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d body=%s", resp.StatusCode, body)
		}
		// 500 RPC failure
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodDelete, ts.URL+"/api/cert", `{"target":"node-err","domain":"example.com"}`, adminHeaders)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "cert busy")
		// 200 success
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodDelete, ts.URL+"/api/cert", `{"target":"node-peer","domain":"example.com"}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteOKJSON(t, body)
		peerRPC.mu.Lock()
		gotDel := append([]string(nil), peerRPC.deletedDomains...)
		peerRPC.mu.Unlock()
		if !reflect.DeepEqual(gotDel, []string{"example.com"}) {
			t.Fatalf("expected DeleteCert [example.com], got %v", gotDel)
		}
	})

	t.Run("PUT /api/node/:name/groups set_node_groups", func(t *testing.T) {
		// 400 invalid JSON
		resp, _ := clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/node/node-peer/groups", "not-json", adminHeaders)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		// 400 empty name via direct Execute and via handler without :name param
		_, err := s.nodeGroupsSetEndpoint().Execute(context.Background(), Caller{Role: "admin"}, NodeGroupsSetReq{Name: "", Groups: []string{"g1"}})
		epErr, ok := err.(*EndpointError)
		if !ok || epErr.HTTPStatus != http.StatusBadRequest || epErr.Msg != "name is required" {
			t.Fatalf("expected 400 name is required, got %v", err)
		}
		// 502 no available node
		resp, body := clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/node/missing/groups", `{"groups":["hk"]}`, adminHeaders)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d body=%s", resp.StatusCode, body)
		}
		// 500 RPC failure
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/node/node-err/groups", `{"groups":["hk"]}`, adminHeaders)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "groups db err")
		// 200 success + path param overrides body name
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/node/node-peer/groups", `{"name":"node-err","groups":["default","hk"]}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteOKJSON(t, body)
		// 200 empty groups list
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/node/node-peer/groups", `{"groups":[]}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for empty groups, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteOKJSON(t, body)
		peerRPC.mu.Lock()
		if len(peerRPC.setNodeGroupsArgs) != 2 || !reflect.DeepEqual(peerRPC.setNodeGroupsArgs[0], []string{"default", "hk"}) || len(peerRPC.setNodeGroupsArgs[1]) != 0 {
			t.Fatalf("unexpected SetNodeGroups calls on peer: %+v", peerRPC.setNodeGroupsArgs)
		}
		peerRPC.mu.Unlock()
	})

	t.Run("PUT /api/gateway set_gateway", func(t *testing.T) {
		resp, _ := clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/gateway", "bad", adminHeaders)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		resp, _ = clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/gateway", `{"target":"missing","enable_gateway_model":true}`, adminHeaders)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d", resp.StatusCode)
		}
		resp, body := clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/gateway", `{"target":"node-err","enable_gateway_model":true}`, adminHeaders)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "gateway err")

		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/gateway", `{"enable_gateway_model":true}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteOKJSON(t, body)
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/gateway", `{"target":"node-local","enable_gateway_model":false}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		localRPC.mu.Lock()
		gotGW := append([]bool(nil), localRPC.setGatewayArgs...)
		localRPC.mu.Unlock()
		if !reflect.DeepEqual(gotGW, []bool{true, false}) {
			t.Fatalf("expected SetGatewayModel [true false], got %v", gotGW)
		}
	})

	t.Run("PUT /api/pingCheck set_ping_check", func(t *testing.T) {
		resp, _ := clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/pingCheck", "bad", adminHeaders)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		resp, _ = clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/pingCheck", `{"target":"missing","enable_ping_check":true}`, adminHeaders)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d", resp.StatusCode)
		}
		resp, body := clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/pingCheck", `{"target":"node-err","enable_ping_check":true}`, adminHeaders)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "ping err")

		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/pingCheck", `{"enable_ping_check":true}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteOKJSON(t, body)
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPut, ts.URL+"/api/pingCheck", `{"target":"node-local","enable_ping_check":false}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		localRPC.mu.Lock()
		gotPing := append([]bool(nil), localRPC.setPingCheckArgs...)
		localRPC.mu.Unlock()
		if !reflect.DeepEqual(gotPing, []bool{true, false}) {
			t.Fatalf("expected SetPingCheck [true false], got %v", gotPing)
		}
	})

	t.Run("POST /api/copyUserBetweenNodes copy_user_between_nodes", func(t *testing.T) {
		// 400 invalid JSON
		resp, _ := clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/copyUserBetweenNodes", "bad", adminHeaders)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		// 400 same src & dst (explicit and both empty -> both default to node-local)
		for _, payload := range []string{`{"src_node":"node-peer","dst_node":"node-peer"}`, `{}`} {
			resp, body := clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/copyUserBetweenNodes", payload, adminHeaders)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("payload=%s: expected 400, got %d body=%s", payload, resp.StatusCode, body)
			}
			assertClusterCertsWriteErrJSON(t, body, 400, "is same with dst target")
		}
		// 502 missing src
		resp, body := clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/copyUserBetweenNodes", `{"src_node":"missing","dst_node":"node-peer"}`, adminHeaders)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 502, "no available src node")
		// 502 missing dst
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/copyUserBetweenNodes", `{"src_node":"node-local","dst_node":"missing"}`, adminHeaders)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 502, "no available dst node")
		// 500 src RPC failure
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/copyUserBetweenNodes", `{"src_node":"node-err","dst_node":"node-peer"}`, adminHeaders)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "get src node user list err")
		// 500 dst RPC failure
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/copyUserBetweenNodes", `{"src_node":"node-local","dst_node":"node-err"}`, adminHeaders)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "dst add err")
		// 200 success: traffic reset to 0
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/copyUserBetweenNodes", `{"dst_node":"node-peer"}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteOKJSON(t, body)
		peerRPC.mu.Lock()
		if len(peerRPC.addedUsersBatches) != 1 || len(peerRPC.addedUsersBatches[0]) != 2 {
			t.Fatalf("expected 1 batch of 2 users on peer, got %+v", peerRPC.addedUsersBatches)
		}
		for _, u := range peerRPC.addedUsersBatches[0] {
			if u.Uplink != 0 || u.Downlink != 0 {
				t.Fatalf("expected copied user %s traffic reset to 0, got up=%d down=%d", u.Name, u.Uplink, u.Downlink)
			}
		}
		peerRPC.mu.Unlock()
		// 200 success when src has 0 users (typed nil []*proto.User slice must not panic or error)
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/copyUserBetweenNodes", `{"src_node":"node-peer","dst_node":"node-local"}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 copying empty user list, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteOKJSON(t, body)

		// Safe guard when succList has no entry for srcNode (valid node with CreateTime=now
		// that is not yet RegisteredRemote -> skipped by ReqToMultiEndNodeServer -> no panic!)
		now := time.Now().Unix()
		unregSrc := &cluster.Node{
			Node:       &proto.Node{Name: "unreg-src", Host: "127.0.0.1", Port: 1, NodeId: "id-unreg"},
			CreateTime: now,
		}
		sGuard, tsGuard := setupClusterCertsWriteServer(t, "node-local", []*cluster.Node{unregSrc, peerNode}, cr, false)
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, tsGuard.URL+"/api/copyUserBetweenNodes", `{"src_node":"unreg-src","dst_node":"node-peer"}`, map[string]string{"X-Token": sGuard.token})
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500 without panic when succList missing src entry, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "empty response")
	})

	t.Run("POST /api/update update_node_binary", func(t *testing.T) {
		resp, _ := clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/update", "bad", adminHeaders)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
		resp, _ = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/update", `{"target":"missing"}`, adminHeaders)
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("expected 502, got %d", resp.StatusCode)
		}
		resp, body := clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/update", `{"target":"node-err","version_tag":"v1.0.0"}`, adminHeaders)
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteErrJSON(t, body, 500, "download failed")

		// Empty version_tag defaults to "latest"
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/update", `{}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteOKJSON(t, body)
		// Explicit version_tag preserved
		resp, body = clusterCertsWriteHTTPDo(t, http.MethodPost, ts.URL+"/api/update", `{"target":"node-local","version_tag":"v2.10.0"}`, adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		assertClusterCertsWriteOKJSON(t, body)
		localRPC.mu.Lock()
		gotTags := append([]string(nil), localRPC.updateProxyTags...)
		localRPC.mu.Unlock()
		if !reflect.DeepEqual(gotTags, []string{"latest", "v2.10.0"}) {
			t.Fatalf("expected UpdateProxy tags [latest v2.10.0], got %v", gotTags)
		}
	})

	t.Run("legacy handlers delegate and preserve help text", func(t *testing.T) {
		checks := []struct {
			key     string
			method  string
			reqPath string
			body    string
			legacy  HttpHandlerInterface
		}{
			{"POST /cert", http.MethodPost, "/cert", `{"target":"node-local","domain":"legacy.com"}`, &CertHandler{}},
			{"POST /cert/transfer", http.MethodPost, "/cert/transfer", `{"target":"node-peer","domain":"example.com"}`, &TransferCertHandler{}},
			{"DELETE /cert", http.MethodDelete, "/cert", `{"target":"node-local","domain":"legacy.com"}`, &DeleteCertHandler{}},
			{"PUT /node/:name/groups", http.MethodPut, "/node/node-local/groups", `{"groups":["g1"]}`, &NodeGroupsSetHandler{}},
			{"PUT /gateway", http.MethodPut, "/gateway", `{"target":"node-local","enable_gateway_model":false}`, &GatewayHandler{}},
			{"PUT /pingCheck", http.MethodPut, "/pingCheck", `{"target":"node-local","enable_ping_check":false}`, &PingCheckHandler{}},
			{"POST /copyUserBetweenNodes", http.MethodPost, "/copyUserBetweenNodes", `{"src_node":"node-local","dst_node":"node-peer"}`, &CopyUserBetweenNodesHandler{}},
			{"POST /update", http.MethodPost, "/update", `{"target":"node-local","version_tag":"latest"}`, &UpdateHandler{}},
		}
		for _, ch := range checks {
			registered, ok := s.handlersMap[ch.key]
			if !ok {
				t.Fatalf("expected %q in handlersMap", ch.key)
			}
			if registered.help() == "" || registered.help() != ch.legacy.help() {
				t.Fatalf("%s: help text mismatch: registered=%q legacy=%q", ch.key, registered.help(), ch.legacy.help())
			}

			ch.legacy.setHttpServer(s)
			r := handlerEngine(ch.method, ch.legacy.getRelativePath(), ch.legacy.getHandlers()[0])
			w := httptest.NewRecorder()
			req := httptest.NewRequest(ch.method, ch.reqPath, bytes.NewBufferString(ch.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("legacy %s: expected 200, got %d body=%s", ch.key, w.Code, w.Body.String())
			}
		}
	})
}

func TestClusterCertsWrite_MCP_Visibility_Annotations_Execution_AndErrors(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "mcp.example.com.crt")
	keyPath := filepath.Join(tmpDir, "mcp.example.com.key")
	if err := os.WriteFile(certPath, []byte("MCP-CERT"), 0600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("MCP-KEY"), 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	cr := &clusterCertsWriteCertReader{
		files: map[string][2]string{
			"mcp.example.com": {certPath, keyPath},
		},
	}

	localRPC := &clusterCertsWriteFakeRPCServer{
		users: []*proto.User{
			{Name: "u1", AuthToken: "t1", Uplink: 500, Downlink: 600},
		},
	}
	peerRPC := &clusterCertsWriteFakeRPCServer{}
	errRPC := &clusterCertsWriteFakeRPCServer{
		obtainCertErr: "rpc boom",
	}

	localNode := startClusterCertsWriteFakeNode(t, "node-local", "id-node-local", localRPC)
	peerNode := startClusterCertsWriteFakeNode(t, "node-peer", "id-node-peer", peerRPC)
	errNode := startClusterCertsWriteFakeNode(t, "node-err", "id-node-err", errRPC)

	ctx := context.Background()

	all8Tools := []string{
		"apply_cert",
		"copy_user_between_nodes",
		"delete_cert",
		"set_gateway",
		"set_node_groups",
		"set_ping_check",
		"transfer_cert",
		"update_node_binary",
	}

	t.Run("MCPReadOnly=true hides all 8 write tools from admin and normal user", func(t *testing.T) {
		sRO, tsRO := setupClusterCertsWriteServer(t, "node-local", []*cluster.Node{localNode, peerNode}, cr, true)

		adminSess := connectMCPClient(t, tsRO.URL, map[string]string{"X-Token": sRO.token})
		adminList, err := adminSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("admin ListTools failed: %v", err)
		}
		if len(adminList.Tools) != 0 {
			t.Fatalf("MCPReadOnly=true must hide all 8 write tools from admin, got %v", toolNames(adminList.Tools))
		}

		normalJWT := makeJWT(t, sRO, "alice", "normal")
		userSess := connectMCPClient(t, tsRO.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
		userList, err := userSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("user ListTools failed: %v", err)
		}
		if len(userList.Tools) != 0 {
			t.Fatalf("MCPReadOnly=true must hide all 8 write tools from normal user, got %v", toolNames(userList.Tools))
		}
	})

	t.Run("MCPReadOnly=false exposes all 8 to admin, hides all 8 from normal JWT, verifies annotations & execution", func(t *testing.T) {
		s, ts := setupClusterCertsWriteServer(t, "node-local", []*cluster.Node{localNode, peerNode, errNode}, cr, false)

		// 1. Admin sees all 8 tools with exact annotations and descriptions.
		adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})
		adminList, err := adminSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("admin ListTools failed: %v", err)
		}
		if got := toolNames(adminList.Tools); !reflect.DeepEqual(got, all8Tools) {
			t.Fatalf("admin tools mismatch: got %v, want %v", got, all8Tools)
		}

		wantDestructive := map[string]bool{
			"apply_cert":              false,
			"transfer_cert":           false,
			"delete_cert":             true,
			"set_node_groups":         false,
			"set_gateway":             false,
			"set_ping_check":          false,
			"copy_user_between_nodes": false,
			"update_node_binary":      true,
		}
		wantRequired := map[string][]string{
			"apply_cert":              {"domain"},
			"transfer_cert":           {"domain"},
			"delete_cert":             {"domain"},
			"set_node_groups":         {"groups", "name"},
			"set_gateway":             {"enable_gateway_model"},
			"set_ping_check":          {"enable_ping_check"},
			"copy_user_between_nodes": nil,
			"update_node_binary":      nil,
		}

		for _, tl := range adminList.Tools {
			if tl.Description == "" {
				t.Fatalf("tool %s has empty description", tl.Name)
			}
			if tl.Name == "update_node_binary" {
				descLower := strings.ToLower(tl.Description)
				if !strings.Contains(descLower, "restart") || !strings.Contains(descLower, "update") {
					t.Fatalf("update_node_binary description should warn about binary update/restart, got %q", tl.Description)
				}
			}
			if tl.Annotations == nil {
				t.Fatalf("tool %s missing annotations", tl.Name)
			}
			if tl.Annotations.ReadOnlyHint {
				t.Fatalf("tool %s expected ReadOnlyHint=false, got true", tl.Name)
			}
			if tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint != wantDestructive[tl.Name] {
				t.Fatalf("tool %s expected DestructiveHint=%v, got %+v", tl.Name, wantDestructive[tl.Name], tl.Annotations.DestructiveHint)
			}

			// Verify required fields in generated JSON schema
			rawSchema, err := json.Marshal(tl.InputSchema)
			if err != nil {
				t.Fatalf("marshal schema for %s: %v", tl.Name, err)
			}
			var schemaObj struct {
				Required []string `json:"required"`
			}
			if err := json.Unmarshal(rawSchema, &schemaObj); err != nil {
				t.Fatalf("unmarshal schema for %s: %v", tl.Name, err)
			}
			sort.Strings(schemaObj.Required)
			wantReq := append([]string(nil), wantRequired[tl.Name]...)
			sort.Strings(wantReq)
			if !(len(schemaObj.Required) == 0 && len(wantReq) == 0) && !reflect.DeepEqual(schemaObj.Required, wantReq) {
				t.Fatalf("tool %s required fields mismatch: got %v, want %v (schema=%s)", tl.Name, schemaObj.Required, wantReq, string(rawSchema))
			}
		}

		// 2. Normal JWT sees 0 tools and cannot invoke any of the 8 admin tools.
		normalJWT := makeJWT(t, s, "alice", "normal")
		userSess := connectMCPClient(t, ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
		userList, err := userSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("normal user ListTools failed: %v", err)
		}
		if len(userList.Tools) != 0 {
			t.Fatalf("normal user must see 0 admin write tools, got %v", toolNames(userList.Tools))
		}
		for _, toolName := range all8Tools {
			_, err := userSess.CallTool(ctx, &mcp.CallToolParams{
				Name:      toolName,
				Arguments: map[string]any{},
			})
			if err == nil {
				t.Fatalf("normal user calling admin tool %s must fail", toolName)
			}
		}

		// 3. Admin executes all 8 tools via MCP.
		callOK := func(name string, args map[string]any) string {
			t.Helper()
			res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
				Name:      name,
				Arguments: args,
			})
			if err != nil {
				t.Fatalf("CallTool %s protocol error: %v", name, err)
			}
			if res.IsError {
				t.Fatalf("CallTool %s unexpected IsError: %s", name, extractText(t, res))
			}
			return extractText(t, res)
		}

		assertClusterCertsWriteOKJSON(t, []byte(callOK("apply_cert", map[string]any{"domain": "mcp.example.com"})))
		transferText := callOK("transfer_cert", map[string]any{"target": "node-peer", "domain": "mcp.example.com"})
		var transferSucc map[string]any
		if err := json.Unmarshal([]byte(transferText), &transferSucc); err != nil {
			t.Fatalf("unmarshal transfer_cert result: %v text=%s", err, transferText)
		}
		if _, ok := transferSucc["node-peer"]; !ok {
			t.Fatalf("expected node-peer in transfer_cert succList, got %+v", transferSucc)
		}
		assertClusterCertsWriteOKJSON(t, []byte(callOK("delete_cert", map[string]any{"target": "node-peer", "domain": "mcp.example.com"})))
		assertClusterCertsWriteOKJSON(t, []byte(callOK("set_node_groups", map[string]any{"name": "node-peer", "groups": []string{"vip", "sg"}})))
		assertClusterCertsWriteOKJSON(t, []byte(callOK("set_gateway", map[string]any{"target": "node-peer", "enable_gateway_model": true})))
		assertClusterCertsWriteOKJSON(t, []byte(callOK("set_ping_check", map[string]any{"target": "node-peer", "enable_ping_check": true})))
		assertClusterCertsWriteOKJSON(t, []byte(callOK("copy_user_between_nodes", map[string]any{"src_node": "node-local", "dst_node": "node-peer"})))
		// update_node_binary with omitted version_tag defaults to "latest", and explicit tag is passed through.
		assertClusterCertsWriteOKJSON(t, []byte(callOK("update_node_binary", map[string]any{"target": "node-peer"})))
		assertClusterCertsWriteOKJSON(t, []byte(callOK("update_node_binary", map[string]any{"target": "node-peer", "version_tag": "v2.9.9"})))

		peerRPC.mu.Lock()
		if !reflect.DeepEqual(peerRPC.updateProxyTags, []string{"latest", "v2.9.9"}) {
			t.Fatalf("expected MCP update_node_binary tags [latest v2.9.9], got %v", peerRPC.updateProxyTags)
		}
		if len(peerRPC.setNodeGroupsArgs) != 1 || !reflect.DeepEqual(peerRPC.setNodeGroupsArgs[0], []string{"vip", "sg"}) {
			t.Fatalf("expected MCP set_node_groups [vip sg], got %v", peerRPC.setNodeGroupsArgs)
		}
		if !reflect.DeepEqual(peerRPC.setGatewayArgs, []bool{true}) {
			t.Fatalf("expected MCP set_gateway [true], got %v", peerRPC.setGatewayArgs)
		}
		if !reflect.DeepEqual(peerRPC.setPingCheckArgs, []bool{true}) {
			t.Fatalf("expected MCP set_ping_check [true], got %v", peerRPC.setPingCheckArgs)
		}
		peerRPC.mu.Unlock()

		// 4. Error paths return IsError=true.
		errorCases := []struct {
			name       string
			tool       string
			args       map[string]any
			wantSubstr string
		}{
			{"apply_cert no node", "apply_cert", map[string]any{"target": "missing", "domain": "a.com"}, "no available node"},
			{"apply_cert rpc err", "apply_cert", map[string]any{"target": "node-err", "domain": "a.com"}, "rpc boom"},
			{"transfer_cert 404", "transfer_cert", map[string]any{"target": "node-peer", "domain": "nope.com"}, "can't find domain's[nope.com] cert"},
			{"transfer_cert self filtered 502", "transfer_cert", map[string]any{"target": "node-local", "domain": "mcp.example.com"}, "no available node"},
			{"delete_cert empty domain", "delete_cert", map[string]any{"domain": ""}, "domain is required"},
			{"delete_cert no node", "delete_cert", map[string]any{"target": "missing", "domain": "a.com"}, "no available node"},
			{"set_node_groups empty name", "set_node_groups", map[string]any{"name": "", "groups": []string{"g"}}, "name is required"},
			{"set_node_groups no node", "set_node_groups", map[string]any{"name": "missing", "groups": []string{"g"}}, "no available node"},
			{"set_gateway no node", "set_gateway", map[string]any{"target": "missing", "enable_gateway_model": false}, "no available node"},
			{"set_ping_check no node", "set_ping_check", map[string]any{"target": "missing", "enable_ping_check": false}, "no available node"},
			{"copy_user same src/dst", "copy_user_between_nodes", map[string]any{"src_node": "node-peer", "dst_node": "node-peer"}, "is same with dst target"},
			{"copy_user no src", "copy_user_between_nodes", map[string]any{"src_node": "missing", "dst_node": "node-peer"}, "no available src node"},
			{"copy_user no dst", "copy_user_between_nodes", map[string]any{"src_node": "node-local", "dst_node": "missing"}, "no available dst node"},
			{"update_node_binary no node", "update_node_binary", map[string]any{"target": "missing"}, "no available node"},
		}
		for _, tc := range errorCases {
			t.Run(tc.name, func(t *testing.T) {
				res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
					Name:      tc.tool,
					Arguments: tc.args,
				})
				if err != nil {
					t.Fatalf("%s: expected IsError tool result, got protocol error: %v", tc.name, err)
				}
				if !res.IsError {
					t.Fatalf("%s: expected IsError=true, got text=%q", tc.name, extractText(t, res))
				}
				if got := extractText(t, res); !strings.Contains(got, tc.wantSubstr) {
					t.Fatalf("%s: expected error text containing %q, got %q", tc.name, tc.wantSubstr, got)
				}
			})
		}
	})
}
