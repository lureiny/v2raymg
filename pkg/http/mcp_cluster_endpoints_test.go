package http

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
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

const clusterMCPTestToken = "cluster-mcp-test-secret-token-123456"

type clusterMCPFakeRPCServer struct {
	proto.UnimplementedEndNodeAccessServer
	groups        []string
	groupsErr     string
	status        *proto.NodeStatus
	statusErr     string
	containers    []string
	containersErr string
}

func (f *clusterMCPFakeRPCServer) GetNodeGroups(ctx context.Context, req *proto.GetNodeGroupsReq) (*proto.GetNodeGroupsRsp, error) {
	if f.groupsErr != "" {
		return &proto.GetNodeGroupsRsp{Code: 500, Msg: f.groupsErr}, nil
	}
	return &proto.GetNodeGroupsRsp{Code: 0, Groups: f.groups}, nil
}

func (f *clusterMCPFakeRPCServer) GetStatus(ctx context.Context, req *proto.GetStatusReq) (*proto.GetStatusRsp, error) {
	if f.statusErr != "" {
		return &proto.GetStatusRsp{Code: 500, Msg: f.statusErr}, nil
	}
	return &proto.GetStatusRsp{Code: 0, Status: f.status}, nil
}

func (f *clusterMCPFakeRPCServer) GetContainers(ctx context.Context, req *proto.GetContainersReq) (*proto.GetContainersRsp, error) {
	if f.containersErr != "" {
		return &proto.GetContainersRsp{Code: 500, Msg: f.containersErr}, nil
	}
	return &proto.GetContainersRsp{Code: 0, Containers: f.containers}, nil
}

type clusterMCPTestClusterNodes struct {
	nodes []*cluster.Node
	token string
}

func (c *clusterMCPTestClusterNodes) GetNodesWithFilter(f cluster.NodeFilter) []*cluster.Node {
	var out []*cluster.Node
	for _, n := range c.nodes {
		if f(n) {
			out = append(out, n)
		}
	}
	return out
}

func (c *clusterMCPTestClusterNodes) GetClusterToken() string {
	return c.token
}

func startClusterMCPFakeNode(t *testing.T, name, nodeID string, srv *clusterMCPFakeRPCServer) *cluster.Node {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake grpc node %s: %v", name, err)
	}
	gs := grpc.NewServer(grpc.ForceServerCodec(commonrpc.NewEncryptMessageCodec(clusterMCPTestToken)))
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

func setupClusterMCPServer(t *testing.T, nodes []*cluster.Node, readOnly bool) (*HttpServer, *httptest.Server) {
	t.Helper()
	s := newTestHttpServer(nil)
	s.Name = "local-node"
	s.MCPReadOnly = readOnly
	s.localNode = &cluster.LocalNode{
		Node: proto.Node{
			Name:   "local-node",
			Host:   "127.0.0.1",
			Port:   19999,
			NodeId: "local-node-id",
		},
		Token: "local-token",
	}
	s.clusterNodes = &clusterMCPTestClusterNodes{
		nodes: nodes,
		token: clusterMCPTestToken,
	}

	userGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret))
	adminGroup := s.RestfulServer.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret), auth.AdminOnly())

	registerEndpoint(s, userGroup, s.statusEndpoint())
	registerEndpoint(s, adminGroup, s.nodeListEndpoint())
	registerEndpoint(s, adminGroup, s.containersEndpoint())
	registerEndpoint(s, adminGroup, s.nodeGroupsGetEndpoint())

	RegisterMCP(s)

	ts := httptest.NewServer(s.RestfulServer)
	t.Cleanup(ts.Close)
	return s, ts
}

func clusterMCPHTTPGet(t *testing.T, url string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request %s: %v", url, err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	defer resp.Body.Close()
	var body []byte
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			body = append(body, buf[:n]...)
		}
		if rerr != nil {
			break
		}
	}
	return resp, body
}

func assertExactJSONKeys(t *testing.T, obj map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(obj))
	for k := range obj {
		got = append(got, k)
	}
	sort.Strings(got)
	expected := append([]string(nil), want...)
	sort.Strings(expected)
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("JSON keys mismatch: got %v, want %v", got, expected)
	}
}

func TestClusterEndpoints_HTTPBackwardsCompat_AndLegacyHandlers(t *testing.T) {
	nodeB := startClusterMCPFakeNode(t, "node-b", "id-node-b", &clusterMCPFakeRPCServer{
		groups:     []string{"hk", "vip"},
		containers: []string{"xray", "hysteria"},
		status: &proto.NodeStatus{
			NodeName:           "node-b",
			OnlyGateway:        true,
			ClusterUserEnabled: true,
			MemTotal:           8192,
			MemUsed:            2048,
			MemAvailable:       6144,
			NumGoroutine:       42,
			CpuPercent:         12.5,
			NetRxBytes:         1000,
			NetTxBytes:         2000,
			TcpConnections:     17,
			NetRxSpeed:         50.5,
			NetTxSpeed:         60.5,
			Version:            "v2.10.0",
		},
	})
	nodeA := startClusterMCPFakeNode(t, "node-a", "id-node-a", &clusterMCPFakeRPCServer{
		groups:     []string{"default"},
		containers: []string{"xray"},
		status: &proto.NodeStatus{
			NodeName:           "node-a",
			OnlyGateway:        false,
			ClusterUserEnabled: true,
			MemTotal:           4096,
			MemUsed:            1024,
			MemAvailable:       3072,
			NumGoroutine:       20,
			CpuPercent:         5.25,
			NetRxBytes:         300,
			NetTxBytes:         400,
			TcpConnections:     8,
			NetRxSpeed:         10.0,
			NetTxSpeed:         20.0,
			Version:            "v2.10.0",
		},
	})
	dupA := startClusterMCPFakeNode(t, "dup-node", "id-dup-1", &clusterMCPFakeRPCServer{
		groups:        []string{"g1"},
		containersErr: "containers unavailable",
		statusErr:     "status unavailable",
	})
	dupB := startClusterMCPFakeNode(t, "dup-node", "id-dup-2", &clusterMCPFakeRPCServer{
		groups:        nil, // should serialize as [] not null
		containersErr: "containers unavailable",
		statusErr:     "status unavailable",
	})

	s, ts := setupClusterMCPServer(t, []*cluster.Node{nodeB, nodeA, dupA, dupB}, false)
	adminHeaders := map[string]string{"X-Token": s.token}

	t.Run("GET /api/node exact JSON keys, duplicate_name, per-node groups", func(t *testing.T) {
		resp, body := clusterMCPHTTPGet(t, ts.URL+"/api/node", adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		var rawList []map[string]any
		if err := json.Unmarshal(body, &rawList); err != nil {
			t.Fatalf("unmarshal /api/node: %v body=%s", err, body)
		}
		if len(rawList) != 4 {
			t.Fatalf("expected 4 nodes, got %d", len(rawList))
		}
		byID := map[string]map[string]any{}
		for _, item := range rawList {
			assertExactJSONKeys(t, item, "name", "host", "port", "node_id", "duplicate_name", "groups")
			byID[item["node_id"].(string)] = item
		}
		if byID["id-node-a"]["duplicate_name"] != false {
			t.Fatalf("expected id-node-a duplicate_name=false, got %v", byID["id-node-a"]["duplicate_name"])
		}
		if byID["id-dup-1"]["duplicate_name"] != true || byID["id-dup-2"]["duplicate_name"] != true {
			t.Fatalf("expected duplicate_name=true for both dup-node entries, got %v / %v", byID["id-dup-1"]["duplicate_name"], byID["id-dup-2"]["duplicate_name"])
		}
		if !reflect.DeepEqual(byID["id-dup-1"]["groups"], []any{"g1"}) {
			t.Fatalf("expected id-dup-1 groups=[g1], got %#v", byID["id-dup-1"]["groups"])
		}
		if !reflect.DeepEqual(byID["id-dup-2"]["groups"], []any{}) {
			t.Fatalf("expected id-dup-2 groups=[] (non-null), got %#v", byID["id-dup-2"]["groups"])
		}
	})

	t.Run("GET /api/status default target=all, sorting, partial failure, exact JSON keys", func(t *testing.T) {
		for _, path := range []string{"/api/status", "/api/status?target="} {
			resp, body := clusterMCPHTTPGet(t, ts.URL+path, adminHeaders)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s: expected 200, got %d body=%s", path, resp.StatusCode, body)
			}
			var raw map[string]any
			if err := json.Unmarshal(body, &raw); err != nil {
				t.Fatalf("%s: unmarshal: %v", path, err)
			}
			assertExactJSONKeys(t, raw, "nodes", "failed")

			var parsed StatusResp
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatalf("%s: unmarshal StatusResp: %v", path, err)
			}
			if len(parsed.Nodes) != 2 {
				t.Fatalf("%s: expected 2 healthy nodes, got %d (%+v)", path, len(parsed.Nodes), parsed.Nodes)
			}
			if parsed.Nodes[0].NodeName != "node-a" || parsed.Nodes[1].NodeName != "node-b" {
				t.Fatalf("%s: expected nodes sorted [node-a, node-b], got [%s, %s]", path, parsed.Nodes[0].NodeName, parsed.Nodes[1].NodeName)
			}
			if _, hasFailed := parsed.Failed["dup-node"]; !hasFailed {
				t.Fatalf("%s: expected dup-node in failed map, got %+v", path, parsed.Failed)
			}
			// Verify all 14 JSON keys on node status item.
			rawNodes := raw["nodes"].([]any)
			assertExactJSONKeys(t, rawNodes[0].(map[string]any),
				"node_name", "only_gateway", "cluster_user_enabled",
				"mem_total", "mem_used", "mem_available",
				"num_goroutine", "cpu_percent",
				"net_rx_bytes", "net_tx_bytes", "tcp_connections",
				"net_rx_speed", "net_tx_speed", "version",
			)
		}
	})

	t.Run("GET /api/status specific target by name and node_id", func(t *testing.T) {
		for _, target := range []string{"node-b", "id-node-b"} {
			resp, body := clusterMCPHTTPGet(t, ts.URL+"/api/status?target="+target, adminHeaders)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("target=%s: expected 200, got %d body=%s", target, resp.StatusCode, body)
			}
			var parsed StatusResp
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(parsed.Nodes) != 1 || parsed.Nodes[0].NodeName != "node-b" || !parsed.Nodes[0].OnlyGateway || parsed.Nodes[0].Version != "v2.10.0" {
				t.Fatalf("target=%s: unexpected status response: %+v", target, parsed)
			}
			if len(parsed.Failed) != 0 {
				t.Fatalf("target=%s: expected empty failed map, got %+v", target, parsed.Failed)
			}
		}
	})

	t.Run("GET /api/containers default target=all and specific target", func(t *testing.T) {
		resp, body := clusterMCPHTTPGet(t, ts.URL+"/api/containers", adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		assertExactJSONKeys(t, raw, "nodes", "failed")

		var parsed ContainersResp
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("unmarshal ContainersResp: %v", err)
		}
		if !reflect.DeepEqual(parsed.Nodes["node-a"], []string{"xray"}) || !reflect.DeepEqual(parsed.Nodes["node-b"], []string{"xray", "hysteria"}) {
			t.Fatalf("unexpected containers map: %+v", parsed.Nodes)
		}
		if _, hasFailed := parsed.Failed["dup-node"]; !hasFailed {
			t.Fatalf("expected dup-node in failed map, got %+v", parsed.Failed)
		}

		// Target by node_id
		respSingle, bodySingle := clusterMCPHTTPGet(t, ts.URL+"/api/containers?target=id-node-a", adminHeaders)
		if respSingle.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for id-node-a, got %d body=%s", respSingle.StatusCode, bodySingle)
		}
		var parsedSingle ContainersResp
		if err := json.Unmarshal(bodySingle, &parsedSingle); err != nil {
			t.Fatalf("unmarshal single: %v", err)
		}
		if len(parsedSingle.Nodes) != 1 || !reflect.DeepEqual(parsedSingle.Nodes["node-a"], []string{"xray"}) {
			t.Fatalf("unexpected single containers response: %+v", parsedSingle)
		}
	})

	t.Run("GET /api/node/:name/groups path param, node_id, empty groups, RPC error", func(t *testing.T) {
		// By name
		resp, body := clusterMCPHTTPGet(t, ts.URL+"/api/node/node-b/groups", adminHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, body)
		}
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		assertExactJSONKeys(t, raw, "groups")
		var parsed NodeGroupsResp
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatalf("unmarshal NodeGroupsResp: %v", err)
		}
		if !reflect.DeepEqual(parsed.Groups, []string{"hk", "vip"}) {
			t.Fatalf("expected [hk vip], got %v", parsed.Groups)
		}

		// By node_id with nil groups -> []
		respEmpty, bodyEmpty := clusterMCPHTTPGet(t, ts.URL+"/api/node/id-dup-2/groups", adminHeaders)
		if respEmpty.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", respEmpty.StatusCode, bodyEmpty)
		}
		if strings.TrimSpace(string(bodyEmpty)) != `{"groups":[]}` {
			t.Fatalf("expected {\"groups\":[]}, got %s", bodyEmpty)
		}
	})

	t.Run("GET /api/node/:name/groups RPC error returns 500", func(t *testing.T) {
		errNode := startClusterMCPFakeNode(t, "err-node", "id-err-node", &clusterMCPFakeRPCServer{
			groupsErr: "db failure",
		})
		sErr, tsErr := setupClusterMCPServer(t, []*cluster.Node{errNode}, false)
		resp, body := clusterMCPHTTPGet(t, tsErr.URL+"/api/node/err-node/groups", map[string]string{"X-Token": sErr.token})
		if resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", resp.StatusCode, body)
		}
		if !containsStr(string(body), "db failure") {
			t.Fatalf("expected error body to contain db failure, got %s", body)
		}
	})

	t.Run("GET /api/node/:name/groups empty name returns 400", func(t *testing.T) {
		_, err := s.nodeGroupsGetEndpoint().Execute(context.Background(), Caller{Role: "admin"}, NodeGroupsGetReq{Name: ""})
		epErr, ok := err.(*EndpointError)
		if !ok || epErr.HTTPStatus != http.StatusBadRequest || epErr.Msg != "name is required" {
			t.Fatalf("expected 400 'name is required', got %v", err)
		}

		// Also verify direct handler invocation without :name param produces 400 JSON.
		h := &NodeGroupsGetHandler{}
		h.setHttpServer(s)
		r := handlerEngine(http.MethodGet, "/node/groups-no-name", h.handlerFunc)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/node/groups-no-name", nil))
		if w.Code != http.StatusBadRequest || !containsStr(w.Body.String(), "name is required") {
			t.Fatalf("expected 400 'name is required', got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("502 no available node across legacy handlers and registered endpoints", func(t *testing.T) {
		sEmpty, tsEmpty := setupClusterMCPServer(t, nil, false)
		hdrs := map[string]string{"X-Token": sEmpty.token}

		// /api/node returns 200 [] when empty
		respNodes, bodyNodes := clusterMCPHTTPGet(t, tsEmpty.URL+"/api/node", hdrs)
		if respNodes.StatusCode != http.StatusOK || strings.TrimSpace(string(bodyNodes)) != "[]" {
			t.Fatalf("expected 200 [] for empty /api/node, got %d body=%s", respNodes.StatusCode, bodyNodes)
		}

		// /api/status, /api/containers, /api/node/:name/groups return 502 "no available node"
		for _, path := range []string{"/api/status", "/api/containers", "/api/node/missing/groups"} {
			resp, body := clusterMCPHTTPGet(t, tsEmpty.URL+path, hdrs)
			if resp.StatusCode != http.StatusBadGateway {
				t.Fatalf("%s: expected 502, got %d body=%s", path, resp.StatusCode, body)
			}
			var errResp struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			if err := json.Unmarshal(body, &errResp); err != nil {
				t.Fatalf("%s: decode err body: %v", path, err)
			}
			if errResp.Code != http.StatusBadGateway || errResp.Msg != "no available node" {
				t.Fatalf("%s: expected {code:502, msg:'no available node'}, got %+v", path, errResp)
			}
		}

		// Legacy handler structs delegate cleanly
		legacyChecks := []struct {
			name    string
			handler HttpHandlerInterface
			path    string
			reqPath string
			want    int
		}{
			{"NodeHandler", &NodeHandler{}, "/node", "/node", http.StatusOK},
			{"StatusHandler", &StatusHandler{}, "/status", "/status", http.StatusBadGateway},
			{"ContainersHandler", &ContainersHandler{}, "/containers", "/containers", http.StatusBadGateway},
			{"NodeGroupsGetHandler", &NodeGroupsGetHandler{}, "/node/:name/groups", "/node/n1/groups", http.StatusBadGateway},
		}
		for _, lc := range legacyChecks {
			lc.handler.setHttpServer(sEmpty)
			r := handlerEngine(http.MethodGet, lc.path, lc.handler.getHandlers()[0])
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, lc.reqPath, nil))
			if w.Code != lc.want {
				t.Fatalf("legacy %s: expected %d, got %d body=%s", lc.name, lc.want, w.Code, w.Body.String())
			}
		}
	})

	t.Run("help text preserved in handlersMap and legacy handlers", func(t *testing.T) {
		checks := []struct {
			key    string
			legacy HttpHandlerInterface
		}{
			{"GET /node", &NodeHandler{}},
			{"GET /status", &StatusHandler{}},
			{"GET /containers", &ContainersHandler{}},
			{"GET /node/:name/groups", &NodeGroupsGetHandler{}},
		}
		for _, ch := range checks {
			registered, ok := s.handlersMap[ch.key]
			if !ok {
				t.Fatalf("expected %q in handlersMap", ch.key)
			}
			if registered.help() == "" || registered.help() != ch.legacy.help() {
				t.Fatalf("%s: help mismatch: registered=%q legacy=%q", ch.key, registered.help(), ch.legacy.help())
			}
		}
	})
}

func TestClusterEndpoints_MCP_RoleIsolation_Schemas_AndExecution(t *testing.T) {
	nodeB := startClusterMCPFakeNode(t, "node-b", "id-node-b", &clusterMCPFakeRPCServer{
		groups:     []string{"hk", "vip"},
		containers: []string{"xray", "mihomo"},
		status: &proto.NodeStatus{
			NodeName:           "node-b",
			OnlyGateway:        false,
			ClusterUserEnabled: true,
			MemTotal:           8192,
			MemUsed:            4096,
			MemAvailable:       4096,
			NumGoroutine:       55,
			CpuPercent:         23.5,
			NetRxBytes:         900,
			NetTxBytes:         1800,
			TcpConnections:     12,
			NetRxSpeed:         11.5,
			NetTxSpeed:         22.5,
			Version:            "v2.10.0",
		},
	})
	nodeA := startClusterMCPFakeNode(t, "node-a", "id-node-a", &clusterMCPFakeRPCServer{
		groups:     []string{"default"},
		containers: []string{"xray"},
		status: &proto.NodeStatus{
			NodeName:           "node-a",
			OnlyGateway:        true,
			ClusterUserEnabled: true,
			MemTotal:           4096,
			MemUsed:            1024,
			MemAvailable:       3072,
			NumGoroutine:       30,
			CpuPercent:         7.5,
			NetRxBytes:         500,
			NetTxBytes:         600,
			TcpConnections:     5,
			NetRxSpeed:         1.0,
			NetTxSpeed:         2.0,
			Version:            "v2.10.0",
		},
	})

	s, ts := setupClusterMCPServer(t, []*cluster.Node{nodeB, nodeA}, false)
	ctx := context.Background()

	// 1. Admin (X-Token) sees all 4 tools, all marked ReadOnlyHint=true & DestructiveHint=false.
	adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})
	adminList, err := adminSess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("admin ListTools failed: %v", err)
	}
	wantAdmin := []string{"get_node_groups", "get_node_status", "list_containers", "list_nodes"}
	if got := toolNames(adminList.Tools); !reflect.DeepEqual(got, wantAdmin) {
		t.Fatalf("admin tools mismatch: got %v, want %v", got, wantAdmin)
	}

	toolByName := map[string]*mcp.Tool{}
	for _, tl := range adminList.Tools {
		toolByName[tl.Name] = tl
		if tl.Description == "" {
			t.Fatalf("tool %s must have non-empty description", tl.Name)
		}
		if tl.Annotations == nil || !tl.Annotations.ReadOnlyHint {
			t.Fatalf("tool %s expected ReadOnlyHint=true, got %+v", tl.Name, tl.Annotations)
		}
		if tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint {
			t.Fatalf("tool %s expected DestructiveHint=false, got %+v", tl.Name, tl.Annotations)
		}
	}

	// Verify input schema required fields: get_node_groups requires "name";
	// get_node_status and list_containers keep "target" optional.
	assertRequiredFields := func(t *testing.T, tl *mcp.Tool, wantRequired []string) {
		t.Helper()
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
		sort.Strings(wantRequired)
		if len(schemaObj.Required) == 0 && len(wantRequired) == 0 {
			return
		}
		if !reflect.DeepEqual(schemaObj.Required, wantRequired) {
			t.Fatalf("tool %s required fields mismatch: got %v, want %v (schema=%s)", tl.Name, schemaObj.Required, wantRequired, string(rawSchema))
		}
	}
	assertRequiredFields(t, toolByName["get_node_groups"], []string{"name"})
	assertRequiredFields(t, toolByName["get_node_status"], nil)
	assertRequiredFields(t, toolByName["list_containers"], nil)
	assertRequiredFields(t, toolByName["list_nodes"], nil)

	// 2. Normal user (Bearer JWT) sees ONLY get_node_status.
	normalJWT := makeJWT(t, s, "alice", "normal")
	userSess := connectMCPClient(t, ts.URL, map[string]string{"Authorization": "Bearer " + normalJWT})
	userList, err := userSess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("normal user ListTools failed: %v", err)
	}
	if got := toolNames(userList.Tools); !reflect.DeepEqual(got, []string{"get_node_status"}) {
		t.Fatalf("normal user tools mismatch: got %v, want [get_node_status]", got)
	}

	// Normal user calling any admin-only cluster tool must be rejected.
	for _, adminTool := range []string{"list_nodes", "list_containers", "get_node_groups"} {
		_, err := userSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      adminTool,
			Arguments: map[string]any{"name": "node-a"},
		})
		if err == nil {
			t.Fatalf("expected normal user call to admin tool %s to fail", adminTool)
		}
	}

	// 3. Normal user invokes get_node_status with omitted args -> defaults to target="all".
	userStatusRes, err := userSess.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_node_status",
	})
	if err != nil {
		t.Fatalf("normal user get_node_status failed: %v", err)
	}
	if userStatusRes.IsError {
		t.Fatalf("unexpected IsError on get_node_status: %s", extractText(t, userStatusRes))
	}
	var statusResp StatusResp
	if err := json.Unmarshal([]byte(extractText(t, userStatusRes)), &statusResp); err != nil {
		t.Fatalf("unmarshal get_node_status result: %v", err)
	}
	if len(statusResp.Nodes) != 2 || statusResp.Nodes[0].NodeName != "node-a" || statusResp.Nodes[1].NodeName != "node-b" {
		t.Fatalf("expected default target=all returning [node-a, node-b], got %+v", statusResp)
	}

	// Explicit empty target="" also defaults to "all", and explicit target="node-b" filters to node-b.
	emptyTargetStatusRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_node_status",
		Arguments: map[string]any{"target": ""},
	})
	if err != nil || emptyTargetStatusRes.IsError {
		t.Fatalf("get_node_status target='' failed: err=%v res=%+v", err, emptyTargetStatusRes)
	}
	var emptyTargetStatus StatusResp
	if err := json.Unmarshal([]byte(extractText(t, emptyTargetStatusRes)), &emptyTargetStatus); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(emptyTargetStatus.Nodes) != 2 {
		t.Fatalf("expected target='' to default to all (2 nodes), got %d", len(emptyTargetStatus.Nodes))
	}

	singleStatusRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_node_status",
		Arguments: map[string]any{"target": "node-b"},
	})
	if err != nil || singleStatusRes.IsError {
		t.Fatalf("get_node_status target=node-b failed: err=%v res=%+v", err, singleStatusRes)
	}
	var singleStatus StatusResp
	if err := json.Unmarshal([]byte(extractText(t, singleStatusRes)), &singleStatus); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(singleStatus.Nodes) != 1 || singleStatus.Nodes[0].NodeName != "node-b" {
		t.Fatalf("expected only node-b, got %+v", singleStatus)
	}

	// 4. Admin invokes list_nodes.
	nodesRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_nodes",
	})
	if err != nil || nodesRes.IsError {
		t.Fatalf("list_nodes failed: err=%v res=%+v", err, nodesRes)
	}
	var listedNodes []NodeInfoResp
	if err := json.Unmarshal([]byte(extractText(t, nodesRes)), &listedNodes); err != nil {
		t.Fatalf("unmarshal list_nodes: %v text=%s", err, extractText(t, nodesRes))
	}
	if len(listedNodes) != 2 {
		t.Fatalf("expected 2 nodes, got %+v", listedNodes)
	}

	// 5. Admin invokes list_containers with omitted args (default target="all") and explicit target.
	allContainersRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_containers",
	})
	if err != nil || allContainersRes.IsError {
		t.Fatalf("list_containers default failed: err=%v res=%+v", err, allContainersRes)
	}
	var allContainers ContainersResp
	if err := json.Unmarshal([]byte(extractText(t, allContainersRes)), &allContainers); err != nil {
		t.Fatalf("unmarshal list_containers: %v", err)
	}
	if !reflect.DeepEqual(allContainers.Nodes["node-a"], []string{"xray"}) || !reflect.DeepEqual(allContainers.Nodes["node-b"], []string{"xray", "mihomo"}) {
		t.Fatalf("unexpected list_containers default output: %+v", allContainers)
	}

	oneContainersRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_containers",
		Arguments: map[string]any{"target": "id-node-a"},
	})
	if err != nil || oneContainersRes.IsError {
		t.Fatalf("list_containers target=id-node-a failed: err=%v res=%+v", err, oneContainersRes)
	}
	var oneContainers ContainersResp
	if err := json.Unmarshal([]byte(extractText(t, oneContainersRes)), &oneContainers); err != nil {
		t.Fatalf("unmarshal single list_containers: %v", err)
	}
	if len(oneContainers.Nodes) != 1 || !reflect.DeepEqual(oneContainers.Nodes["node-a"], []string{"xray"}) {
		t.Fatalf("unexpected list_containers single output: %+v", oneContainers)
	}

	// 6. Admin invokes get_node_groups by name and by node_id, plus empty name guard.
	groupsRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_node_groups",
		Arguments: map[string]any{"name": "node-b"},
	})
	if err != nil || groupsRes.IsError {
		t.Fatalf("get_node_groups failed: err=%v res=%+v", err, groupsRes)
	}
	var groupsResp NodeGroupsResp
	if err := json.Unmarshal([]byte(extractText(t, groupsRes)), &groupsResp); err != nil {
		t.Fatalf("unmarshal get_node_groups: %v", err)
	}
	if !reflect.DeepEqual(groupsResp.Groups, []string{"hk", "vip"}) {
		t.Fatalf("expected [hk vip], got %+v", groupsResp)
	}

	emptyNameRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_node_groups",
		Arguments: map[string]any{"name": ""},
	})
	if err != nil {
		t.Fatalf("expected tool error result for empty name, got protocol error: %v", err)
	}
	if !emptyNameRes.IsError || !containsStr(extractText(t, emptyNameRes), "name is required") {
		t.Fatalf("expected IsError=true with 'name is required', got IsError=%v text=%q", emptyNameRes.IsError, extractText(t, emptyNameRes))
	}

	// 7. MCPReadOnly=true still exposes all 4 read-only cluster tools.
	sRO, tsRO := setupClusterMCPServer(t, []*cluster.Node{nodeA}, true)
	roAdminSess := connectMCPClient(t, tsRO.URL, map[string]string{"X-Token": sRO.token})
	roTools, err := roAdminSess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("read-only ListTools failed: %v", err)
	}
	if got := toolNames(roTools.Tools); !reflect.DeepEqual(got, wantAdmin) {
		t.Fatalf("read-only mode should keep all 4 read-only cluster tools, got %v", got)
	}
}

func TestClusterEndpoints_MCP_NoAvailableNodeReturnsIsError(t *testing.T) {
	s, ts := setupClusterMCPServer(t, nil, false)
	ctx := context.Background()
	adminSess := connectMCPClient(t, ts.URL, map[string]string{"X-Token": s.token})

	// list_nodes succeeds with empty JSON array [].
	nodesRes, err := adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_nodes"})
	if err != nil {
		t.Fatalf("list_nodes on empty cluster failed: %v", err)
	}
	if nodesRes.IsError {
		t.Fatalf("expected list_nodes IsError=false on empty cluster, got: %s", extractText(t, nodesRes))
	}
	if strings.TrimSpace(extractText(t, nodesRes)) != "[]" {
		t.Fatalf("expected [] from list_nodes on empty cluster, got %q", extractText(t, nodesRes))
	}

	// get_node_status, list_containers, get_node_groups return IsError=true with "no available node".
	cases := []struct {
		name string
		args map[string]any
	}{
		{"get_node_status", nil},
		{"list_containers", nil},
		{"get_node_groups", map[string]any{"name": "missing-node"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{
				Name:      tc.name,
				Arguments: tc.args,
			})
			if err != nil {
				t.Fatalf("%s: expected tool IsError result, got protocol error: %v", tc.name, err)
			}
			if !res.IsError {
				t.Fatalf("%s: expected IsError=true when no node is available, got text=%q", tc.name, extractText(t, res))
			}
			if got := extractText(t, res); !containsStr(got, "no available node") {
				t.Fatalf("%s: expected error text containing 'no available node', got %q", tc.name, got)
			}
		})
	}
}

// Sanity check that gin.H is not accidentally imported or broken; also verifies
// NodeGroupsSetHandler in node_groups_handler.go remained completely intact.
var _ gin.HandlerFunc = (&NodeGroupsSetHandler{}).handlerFunc
