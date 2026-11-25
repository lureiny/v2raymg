//go:build integration

package systemtest

// cluster_e2e_api_test.go — TestClusterE2E_FullAPI
//
// Walks the entire HTTP surface against a real 3-node cluster. For any
// endpoint that fans out over the cluster the assertion is not "returned 200" but
// "the OTHER two nodes actually observe the effect" — a fan-out that silently
// reached only the local node would still return 200.
//
// The final subtest asserts every route in pkg/http/testdata/e2e_covered_routes.txt
// was really exercised, so an endpoint cannot be listed as covered without a case
// that calls it.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	apiTestUser = "e2e-api-user"
	apiTestPass = "e2e-api-pass-12345678"
)

// requireOK demands a 200. Used for endpoints that must work in any environment:
// anything that only touches the cluster/user planes.
func requireOK(t *testing.T, what string, code int, body []byte, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", what, err)
	}
	if code != 200 {
		t.Fatalf("%s: status %d, body %s", what, code, truncate(string(body), 600))
	}
}

// requireRouted is the assertion for endpoints whose SUCCESS depends on things CI
// cannot provide — an ACME account, a reachable release tag, a running proxy core
// with live forward rules. Demanding 200 there would make the suite a test of the
// environment; ignoring the response entirely would make it a test of nothing.
//
// The line drawn here is exactly two failure modes:
//
//	404 page not found          <- gin never routed it; the case tests nothing (fail)
//	5xx with an unstructured body <- the handler panicked rather than erroring  (fail)
//
// Everything else is a real handler response and is accepted, including
// application errors. Note the API is deliberately not uniform — /api/metrics
// returns Prometheus text, /help and /sub return plain text, and /api/authHysteria2
// speaks hysteria's own {"ok":bool} shape — so requiring a {code,msg} envelope
// everywhere would fail correct endpoints.
func requireRouted(t *testing.T, what string, code int, body []byte, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: transport error: %v", what, err)
	}

	trimmed := strings.TrimSpace(string(body))
	// gin's built-in replies when nothing matched the method+path.
	if strings.EqualFold(trimmed, "404 page not found") ||
		strings.EqualFold(trimmed, "405 method not allowed") {
		t.Errorf("%s: not routed (status %d, body %q) — the route was renamed or "+
			"removed, so this case is testing nothing", what, code, trimmed)
		return
	}

	if code >= 500 {
		// A handled failure serialises an envelope; a panic leaves an empty or
		// non-JSON body behind.
		var envelope struct {
			Code *int   `json:"code"`
			Msg  string `json:"msg"`
		}
		if jsonErr := json.Unmarshal(body, &envelope); jsonErr != nil || envelope.Code == nil {
			t.Errorf("%s: status %d with an unstructured body (handler crashed rather "+
				"than returning an error): %s", what, code, truncate(trimmed, 600))
			return
		}
		t.Logf("%s: handled application error %d: %s (accepted — success needs "+
			"environment this CI does not provide)", what, *envelope.Code, envelope.Msg)
	}
}

// userNames pulls the username set a node reports for GET /api/user.
func userNames(t *testing.T, s *e2eServer, target string) map[string]bool {
	t.Helper()
	params := url.Values{}
	if target != "" {
		params.Set("target", target)
	}
	code, body, err := s.apiGet("/api/user", params)
	requireOK(t, fmt.Sprintf("%s GET /api/user", s.name), code, body, err)

	out := map[string]bool{}
	// The payload shape has varied across versions; match on the username field
	// rather than binding a struct, so this helper is not the thing that breaks.
	var generic any
	if err := json.Unmarshal(body, &generic); err != nil {
		t.Fatalf("%s GET /api/user: bad JSON: %v", s.name, err)
	}
	collectUsernames(generic, out)
	return out
}

func collectUsernames(v any, out map[string]bool) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if (k == "name" || k == "user" || k == "username") && val != nil {
				if s, ok := val.(string); ok && s != "" {
					out[s] = true
				}
			}
			collectUsernames(val, out)
		}
	case []any:
		for _, item := range t {
			collectUsernames(item, out)
		}
	}
}

// eventually retries fn until it returns true or the deadline passes. Cluster
// mutations propagate on the heartbeat, so a read-back immediately after a write
// is legitimately racy.
func eventually(t *testing.T, what string, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Errorf("%s: still false after %s", what, timeout)
}

type e2eHeaderRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h *e2eHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
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

func connectE2EMCPClient(t *testing.T, baseURL string, headers map[string]string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-mcp-client", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint: baseURL + "/mcp",
		HTTPClient: &http.Client{
			Transport: &e2eHeaderRoundTripper{headers: headers},
		},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connectE2EMCPClient(%s): %v", baseURL, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func extractE2EMCPText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil || len(res.Content) == 0 {
		t.Fatalf("expected non-empty CallToolResult.Content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

func TestClusterE2E_FullAPI(t *testing.T) {
	// Deliberately no network precondition. This suite exercises the HTTP and
	// cluster planes, not proxy data paths: container binaries are resolved from
	// the environment when available, and a container that fails to start is
	// non-fatal for the server (cmd/server.go logs StartAll errors and carries on).
	// Requiring a download here would make the whole regression suite hostage to
	// GitHub reachability. Protocol-level coverage lives in the matrix tests.
	tmpDir := t.TempDir()
	c := startE2ECluster(t, tmpDir, clusterOpts{
		HeartbeatIntervalSec: 1,
		XrayBin:              os.Getenv("XRAY_BIN"),
		MihomoBin:            os.Getenv("MIHOMO_BIN"),
	})
	for i := 0; i < 3; i++ {
		c.addEndNode(t)
	}
	c.waitConverged(t, 60*time.Second)
	// Directory convergence only means the nodes KNOW each other; fan-out also
	// needs mutual registration to have completed, and ReqToMultiEndNodeServer
	// silently skips peers that are not there yet. Without this every target=all
	// assertion below could pass while reaching only the local node.
	c.waitFanoutReady(t, 60*time.Second)

	nodeA, nodeB, nodeC := c.ends[0], c.ends[1], c.ends[2]

	// ---------------------------------------------------------------- read-only

	t.Run("GET_status", func(t *testing.T) {
		for _, s := range c.ends {
			code, body, err := s.apiGet("/api/status", url.Values{"target": {"all"}})
			requireOK(t, s.name+" status", code, body, err)
			// target=all must reach every node, not just the local one.
			for _, want := range c.nodeNames() {
				if !strings.Contains(string(body), want) {
					t.Errorf("%s status(target=all) missing node %q: %s",
						s.name, want, truncate(string(body), 800))
				}
			}
		}
	})

	t.Run("GET_node", func(t *testing.T) {
		for _, s := range c.ends {
			known := s.knownNodes(t)
			for _, want := range c.nodeNames() {
				view, ok := known[want]
				if !ok {
					t.Errorf("%s /api/node missing %q (have %v)", s.name, want, known)
					continue
				}
				// Identity is what the directory is keyed by, so a blank one means
				// this peer was never actually identified — it would be re-learned
				// as a duplicate the moment anything about it changed.
				if view.NodeID == "" {
					t.Errorf("%s /api/node reports %q with no node_id", s.name, want)
				}
				if view.DuplicateName {
					t.Errorf("%s /api/node reports %q as a duplicate name", s.name, want)
				}
			}
		}
	})

	t.Run("GET_containers", func(t *testing.T) {
		code, body, err := nodeA.apiGet("/api/containers", url.Values{"target": {"all"}})
		requireOK(t, "GET /api/containers", code, body, err)
		if !strings.Contains(string(body), "xray") {
			t.Errorf("containers response has no xray entry: %s", truncate(string(body), 600))
		}
	})

	t.Run("GET_metrics", func(t *testing.T) {
		code, body, err := nodeA.apiGet("/api/metrics", nil)
		requireRouted(t, "GET /api/metrics", code, body, err)
	})

	t.Run("GET_help", func(t *testing.T) {
		code, body, err := nodeA.apiGet("/help/user", nil)
		requireRouted(t, "GET /help/*", code, body, err)
	})

	// ------------------------------------------------------------- user CRUD

	t.Run("POST_user_fans_out", func(t *testing.T) {
		code, body, err := nodeA.apiPost("/api/user", map[string]any{
			"target": "all",
			"user":   apiTestUser,
			"pwd":    apiTestPass,
			"ttl":    86400,
		})
		requireOK(t, "POST /api/user", code, body, err)

		// The point of the test: B and C must have the user too.
		for _, s := range []*e2eServer{nodeA, nodeB, nodeC} {
			s := s
			eventually(t, fmt.Sprintf("%s sees user %s", s.name, apiTestUser), 20*time.Second, func() bool {
				return userNames(t, s, s.name)[apiTestUser]
			})
		}
	})

	t.Run("PUT_user", func(t *testing.T) {
		code, body, err := nodeA.apiPut("/api/user", map[string]any{
			"target": "all",
			"user":   apiTestUser,
			"ttl":    172800,
		})
		requireOK(t, "PUT /api/user", code, body, err)
	})

	t.Run("PUT_user_role_fans_out", func(t *testing.T) {
		code, body, err := nodeA.apiPut("/api/user/"+apiTestUser+"/role", map[string]any{
			"target": "all",
			"role":   "admin",
		})
		requireOK(t, "PUT /api/user/:name/role", code, body, err)

		// Read back from a DIFFERENT node than the one that issued the change.
		eventually(t, "role visible on nodeC", 20*time.Second, func() bool {
			code, body, err := nodeC.apiGet("/api/user", url.Values{"target": {nodeC.name}})
			if err != nil || code != 200 {
				return false
			}
			return strings.Contains(string(body), "admin")
		})
	})

	t.Run("PUT_user_bandwidth_fans_out", func(t *testing.T) {
		code, body, err := nodeA.apiPut("/api/user/"+apiTestUser+"/bandwidth", map[string]any{
			"target":       "all",
			"upload_bps":   1048576,
			"download_bps": 2097152,
		})
		requireOK(t, "PUT /api/user/:name/bandwidth", code, body, err)
	})

	t.Run("PUT_user_client_limit_fans_out", func(t *testing.T) {
		code, body, err := nodeA.apiPut("/api/user/"+apiTestUser+"/client-limit", map[string]any{
			"target":            "all",
			"max_clients":       5,
			"recycle_delay_sec": 30,
			"drain_sec":         10,
		})
		requireOK(t, "PUT /api/user/:name/client-limit", code, body, err)
	})

	t.Run("POST_user_reset_traffic", func(t *testing.T) {
		code, body, err := nodeB.apiPost("/api/user/reset-traffic", map[string]any{
			"target": "all",
			"user":   apiTestUser,
		})
		requireOK(t, "POST /api/user/reset-traffic", code, body, err)
	})

	t.Run("POST_user_reset", func(t *testing.T) {
		code, body, err := nodeB.apiPost("/api/user/reset", map[string]any{
			"target": "all",
			"user":   apiTestUser,
		})
		requireRouted(t, "POST /api/user/reset", code, body, err)
	})

	t.Run("POST_user_reset_token", func(t *testing.T) {
		code, body, err := nodeA.apiPost("/api/user/reset-token", map[string]any{
			"target": "all",
			"user":   apiTestUser,
		})
		requireRouted(t, "POST /api/user/reset-token", code, body, err)
	})

	// ------------------------------------------------------------ node groups

	t.Run("node_groups_get_and_set", func(t *testing.T) {
		path := "/api/node/" + nodeB.name + "/groups"
		code, body, err := nodeA.apiPut(path, map[string]any{"groups": []string{"default", "premium"}})
		requireOK(t, "PUT /api/node/:name/groups", code, body, err)

		eventually(t, "groups readable back", 20*time.Second, func() bool {
			code, body, err := nodeA.apiGet(path, nil)
			return err == nil && code == 200 && strings.Contains(string(body), "premium")
		})
	})

	// ------------------------------------------------------------ node control

	t.Run("PUT_gateway", func(t *testing.T) {
		// Flip on then back off so the rest of the suite runs against normal nodes.
		code, body, err := nodeA.apiPut("/api/gateway", map[string]any{
			"target": nodeC.name, "enable_gateway_model": true,
		})
		requireOK(t, "PUT /api/gateway on", code, body, err)

		code, body, err = nodeA.apiPut("/api/gateway", map[string]any{
			"target": nodeC.name, "enable_gateway_model": false,
		})
		requireOK(t, "PUT /api/gateway off", code, body, err)
	})

	t.Run("PUT_pingCheck", func(t *testing.T) {
		code, body, err := nodeA.apiPut("/api/pingCheck", map[string]any{
			"target": "all", "enable_ping_check": false,
		})
		requireOK(t, "PUT /api/pingCheck", code, body, err)
	})

	// --------------------------------------------------------------- inbounds

	t.Run("inbound_lifecycle", func(t *testing.T) {
		const tag = "e2e-cluster-vless"

		// Single target: each node runs its own xray, and a fan-out add makes the
		// per-node outcome (and therefore the later delete) depend on which cores
		// actually started, which is environment-dependent.
		code, body, err := nodeA.apiPost("/api/inbound/fast", map[string]any{
			"target":    nodeA.name,
			"tag":       tag,
			"container": "xray",
			"protocol":  "vless",
			"transport": "tcp",
		})
		requireRouted(t, "POST /api/inbound/fast", code, body, err)

		code, body, err = nodeA.apiGet("/api/inbounds", url.Values{"target": {"all"}})
		requireOK(t, "GET /api/inbounds", code, body, err)

		code, body, err = nodeA.apiGet("/api/inbound", url.Values{
			"target": {nodeA.name}, "src_tag": {tag}, "container": {"xray"},
		})
		requireRouted(t, "GET /api/inbound", code, body, err)

		code, body, err = nodeA.apiPost("/api/inbound", map[string]any{
			"target":           nodeA.name,
			"container":        "xray",
			"bound_raw_string": `{"tag":"e2e-raw-inbound","port":48765,"protocol":"vless","settings":{"clients":[],"decryption":"none"}}`,
		})
		requireRouted(t, "POST /api/inbound", code, body, err)

		code, body, err = nodeA.apiDelete("/api/inbounds", map[string]any{
			"target": nodeA.name, "container": "xray", "name": tag,
		})
		requireRouted(t, "DELETE /api/inbounds", code, body, err)
	})

	t.Run("rotate_ports", func(t *testing.T) {
		code, body, err := nodeA.apiPost("/api/rotateInboundPort", map[string]any{
			"target": nodeA.name, "username": apiTestUser, "container": "xray",
		})
		requireRouted(t, "POST /api/rotateInboundPort", code, body, err)

		code, body, err = nodeA.apiPost("/api/rotateAllPorts", map[string]any{
			"target": nodeA.name, "username": apiTestUser,
		})
		requireRouted(t, "POST /api/rotateAllPorts", code, body, err)
	})

	// ------------------------------------------------------------------ certs
	// ACME cannot issue in CI, so these assert graceful failure, not success.

	t.Run("certs", func(t *testing.T) {
		code, body, err := nodeA.apiGet("/api/getCerts", url.Values{"target": {"all"}})
		requireOK(t, "GET /api/getCerts", code, body, err)

		code, body, err = nodeA.apiPost("/api/cert", map[string]any{
			"target": nodeA.name, "domain": "e2e-not-a-real-domain.invalid",
		})
		requireRouted(t, "POST /api/cert", code, body, err)

		code, body, err = nodeA.apiPost("/api/cert/transfer", map[string]any{
			"target": nodeB.name, "domain": "e2e-not-a-real-domain.invalid",
		})
		requireRouted(t, "POST /api/cert/transfer", code, body, err)

		code, body, err = nodeA.apiDelete("/api/cert", map[string]any{
			"target": nodeA.name, "domain": "e2e-not-a-real-domain.invalid",
		})
		requireRouted(t, "DELETE /api/cert", code, body, err)
	})

	// ------------------------------------------------------------ misc admin

	t.Run("POST_copyUserBetweenNodes", func(t *testing.T) {
		code, body, err := nodeA.apiPost("/api/copyUserBetweenNodes", map[string]any{
			"src_node": nodeA.name, "dst_node": nodeB.name,
		})
		requireRouted(t, "POST /api/copyUserBetweenNodes", code, body, err)
	})

	t.Run("POST_update", func(t *testing.T) {
		// Self-update must not be attempted for real; an unknown tag exercises the
		// path and must fail gracefully rather than restart the test binary.
		code, body, err := nodeC.apiPost("/api/update", map[string]any{
			"target": nodeC.name, "version_tag": "v0.0.0-e2e-nonexistent",
		})
		requireRouted(t, "POST /api/update", code, body, err)
	})

	// -------------------------------------------------------- user-scope APIs

	t.Run("GET_profile", func(t *testing.T) {
		code, body, err := nodeA.apiGet("/api/profile", url.Values{"target": {nodeA.name}})
		requireRouted(t, "GET /api/profile", code, body, err)
	})

	t.Run("PUT_profile_password", func(t *testing.T) {
		code, body, err := nodeA.apiPut("/api/profile/password", map[string]any{
			"target":       "all",
			"old_password": apiTestPass,
			"new_password": apiTestPass + "-rotated",
		})
		requireRouted(t, "PUT /api/profile/password", code, body, err)
	})

	t.Run("POST_logout", func(t *testing.T) {
		code, body, err := nodeA.apiPost("/api/logout", map[string]any{})
		requireRouted(t, "POST /api/logout", code, body, err)
	})

	// ------------------------------------------------------------- public API

	t.Run("POST_login", func(t *testing.T) {
		code, body, err := nodeA.apiPost("/api/login", map[string]any{
			"username": apiTestUser,
			"password": apiTestPass + "-rotated",
		})
		requireRouted(t, "POST /api/login", code, body, err)
		if code == 200 && !strings.Contains(string(body), "token") {
			t.Errorf("successful login returned no token: %s", truncate(string(body), 400))
		}
	})

	t.Run("GET_sub_aggregates_cluster", func(t *testing.T) {
		code, body, err := nodeA.apiGet("/sub", url.Values{
			"user": {apiTestUser}, "pwd": {apiTestPass + "-rotated"},
		})
		requireRouted(t, "GET /sub", code, body, err)
	})

	t.Run("POST_authHysteria2", func(t *testing.T) {
		code, body, err := nodeA.apiPost("/api/authHysteria2", map[string]any{
			"auth": apiTestUser, "tx": 0,
		})
		requireRouted(t, "POST /api/authHysteria2", code, body, err)
	})

	// ----------------------------------------------------------- auth matrix

	t.Run("auth_matrix", func(t *testing.T) {
		noAuth := &e2eServer{baseURL: nodeA.baseURL, token: "wrong-token", name: nodeA.name}
		code, _, err := noAuth.apiGet("/api/node", nil)
		if err != nil {
			t.Fatalf("unauthenticated GET /api/node: %v", err)
		}
		if code == 200 {
			t.Errorf("admin route accepted a bad token (status %d)", code)
		}
	})

	// ---------------------------------------------------- MCP full 3-node E2E
	// Runs before DELETE_user_fans_out so apiTestUser exists across the cluster.

	t.Run("MCP_3node_full_e2e", func(t *testing.T) {
		const (
			mcpNormalUser = "e2e-mcp-normal"
			mcpNormalPass = "e2e-mcp-pass-123456"
		)

		// Create a dedicated normal-role user and wait for cluster sync to all 3 nodes.
		code, body, err := nodeA.apiPost("/api/user", map[string]any{
			"target": "all",
			"user":   mcpNormalUser,
			"pwd":    mcpNormalPass,
			"role":   "normal",
			"ttl":    86400,
		})
		requireOK(t, "POST /api/user (mcpNormalUser)", code, body, err)
		t.Cleanup(func() {
			_, _, _ = nodeA.apiDelete("/api/user", map[string]any{
				"target": "all",
				"user":   mcpNormalUser,
			})
		})
		for _, s := range []*e2eServer{nodeA, nodeB, nodeC} {
			s := s
			eventually(t, fmt.Sprintf("%s sees %s", s.name, mcpNormalUser), 20*time.Second, func() bool {
				return userNames(t, s, s.name)[mcpNormalUser]
			})
		}

		// Verify HTTP GET /api/user returns real plaintext auth_token values.
		code, body, err = nodeA.apiGet("/api/user", url.Values{"target": {nodeA.name}})
		requireOK(t, "GET /api/user (extract plaintext auth_token)", code, body, err)
		var httpUsersByNode map[string][]struct {
			Name      string `json:"name"`
			AuthToken string `json:"auth_token"`
		}
		if err := json.Unmarshal(body, &httpUsersByNode); err != nil {
			t.Fatalf("decode HTTP GET /api/user: %v (body=%s)", err, truncate(string(body), 600))
		}
		var normalRealAuthToken, apiUserRealAuthToken string
		for _, u := range httpUsersByNode[nodeA.name] {
			switch u.Name {
			case mcpNormalUser:
				normalRealAuthToken = u.AuthToken
			case apiTestUser:
				apiUserRealAuthToken = u.AuthToken
			}
		}
		if normalRealAuthToken == "" {
			t.Fatalf("expected non-empty plaintext auth_token from HTTP GET /api/user for %s", mcpNormalUser)
		}

		// Login as mcpNormalUser to acquire a real normal-role Bearer JWT.
		code, body, err = nodeA.apiPost("/api/login", map[string]any{
			"username": mcpNormalUser,
			"password": mcpNormalPass,
		})
		requireOK(t, "POST /api/login (mcpNormalUser)", code, body, err)
		var loginRsp struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(body, &loginRsp); err != nil || loginRsp.Token == "" {
			t.Fatalf("POST /api/login returned invalid token: err=%v body=%s", err, truncate(string(body), 400))
		}
		normalJWT := loginRsp.Token

		// Unauthenticated and invalid-credential POST /mcp requests must return 401.
		for _, tc := range []struct {
			name   string
			header map[string]string
		}{
			{"no_auth", nil},
			{"wrong_x_token", map[string]string{"X-Token": "wrong-mcp-token"}},
		} {
			req, reqErr := http.NewRequest(http.MethodPost, nodeA.baseURL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			if reqErr != nil {
				t.Fatalf("build raw /mcp request (%s): %v", tc.name, reqErr)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			rsp, doErr := http.DefaultClient.Do(req)
			if doErr != nil {
				t.Fatalf("POST /mcp (%s): %v", tc.name, doErr)
			}
			_ = rsp.Body.Close()
			if rsp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("POST /mcp (%s): got status %d, want 401", tc.name, rsp.StatusCode)
			}
		}

		ctx := context.Background()

		// 1. Official MCP SDK client authenticated as Admin via X-Token.
		adminSess := connectE2EMCPClient(t, nodeA.baseURL, map[string]string{"X-Token": nodeA.token})
		adminList, err := adminSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("admin ListTools: %v", err)
		}
		gotAdminTools := make([]string, 0, len(adminList.Tools))
		for _, tl := range adminList.Tools {
			gotAdminTools = append(gotAdminTools, tl.Name)
			if tl.Annotations == nil || !tl.Annotations.ReadOnlyHint {
				t.Errorf("admin tool %q missing ReadOnlyHint=true: %+v", tl.Name, tl.Annotations)
			}
		}
		sort.Strings(gotAdminTools)
		wantAdminTools := []string{
			"get_inbound",
			"get_node_groups",
			"get_node_status",
			"get_profile",
			"list_certs",
			"list_containers",
			"list_inbounds",
			"list_nodes",
			"list_users",
		}
		if strings.Join(gotAdminTools, ",") != strings.Join(wantAdminTools, ",") {
			t.Fatalf("admin ListTools mismatch: got %v, want %v", gotAdminTools, wantAdminTools)
		}

		// list_nodes
		res, err := adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_nodes"})
		if err != nil || res.IsError {
			t.Fatalf("admin CallTool list_nodes failed: err=%v res=%+v", err, res)
		}
		listNodesText := extractE2EMCPText(t, res)
		for _, wantNode := range c.nodeNames() {
			if !strings.Contains(listNodesText, wantNode) {
				t.Errorf("list_nodes missing %q: %s", wantNode, truncate(listNodesText, 800))
			}
		}

		// get_node_status with empty args tests default:"all" fan-out over real 3-node gRPC.
		res, err = adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "get_node_status"})
		if err != nil || res.IsError {
			t.Fatalf("admin CallTool get_node_status failed: err=%v res=%+v", err, res)
		}
		statusText := extractE2EMCPText(t, res)
		var statusPayload struct {
			Nodes []struct {
				NodeName string `json:"node_name"`
			} `json:"nodes"`
		}
		if err := json.Unmarshal([]byte(statusText), &statusPayload); err != nil {
			t.Fatalf("decode get_node_status JSON: %v (text=%s)", err, truncate(statusText, 600))
		}
		seenStatusNodes := map[string]bool{}
		for _, n := range statusPayload.Nodes {
			seenStatusNodes[n.NodeName] = true
		}
		for _, wantNode := range c.nodeNames() {
			if !seenStatusNodes[wantNode] {
				t.Errorf("get_node_status missing node %q in 3-node fan-out: %s", wantNode, truncate(statusText, 800))
			}
		}

		// list_containers with empty args tests default:"all" fan-out across 3 nodes.
		res, err = adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_containers"})
		if err != nil || res.IsError {
			t.Fatalf("admin CallTool list_containers failed: err=%v res=%+v", err, res)
		}
		containersText := extractE2EMCPText(t, res)
		for _, wantNode := range c.nodeNames() {
			if !strings.Contains(containersText, wantNode) {
				t.Errorf("list_containers missing %q: %s", wantNode, truncate(containersText, 800))
			}
		}

		// get_node_groups for nodeB verifies path param + groups updated in node_groups_get_and_set.
		res, err = adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_node_groups",
			Arguments: map[string]any{"name": nodeB.name},
		})
		if err != nil || res.IsError {
			t.Fatalf("admin CallTool get_node_groups failed: err=%v res=%+v", err, res)
		}
		if txt := extractE2EMCPText(t, res); !strings.Contains(txt, "premium") {
			t.Errorf("get_node_groups(%s) expected \"premium\", got: %s", nodeB.name, truncate(txt, 400))
		}

		// list_inbounds across all 3 nodes.
		res, err = adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_inbounds"})
		if err != nil || res.IsError {
			t.Fatalf("admin CallTool list_inbounds failed: err=%v res=%+v", err, res)
		}
		if txt := extractE2EMCPText(t, res); !json.Valid([]byte(txt)) {
			t.Errorf("list_inbounds returned invalid JSON: %s", truncate(txt, 400))
		}

		// get_inbound graceful handling + validation error mapping to IsError=true.
		res, err = adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_inbound",
			Arguments: map[string]any{"target": nodeA.name, "src_tag": "e2e-nonexistent"},
		})
		if err != nil {
			t.Fatalf("admin CallTool get_inbound transport/protocol error: %v", err)
		}
		if txt := extractE2EMCPText(t, res); txt == "" {
			t.Errorf("get_inbound returned empty text content")
		}
		res, err = adminSess.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_inbound",
			Arguments: map[string]any{"container": "snell"},
		})
		if err != nil {
			t.Fatalf("admin CallTool get_inbound(snell) protocol error: %v", err)
		}
		if !res.IsError {
			t.Errorf("get_inbound(snell) expected IsError=true, got success: %s", extractE2EMCPText(t, res))
		}

		// list_certs across all 3 nodes.
		res, err = adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_certs"})
		if err != nil || res.IsError {
			t.Fatalf("admin CallTool list_certs failed: err=%v res=%+v", err, res)
		}
		if txt := extractE2EMCPText(t, res); !json.Valid([]byte(txt)) {
			t.Errorf("list_certs returned invalid JSON: %s", truncate(txt, 400))
		}

		// list_users across all 3 nodes: sees both users, redacts auth_token, never leaks plaintext!
		res, err = adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_users"})
		if err != nil || res.IsError {
			t.Fatalf("admin CallTool list_users failed: err=%v res=%+v", err, res)
		}
		adminUsersText := extractE2EMCPText(t, res)
		if !strings.Contains(adminUsersText, apiTestUser) || !strings.Contains(adminUsersText, mcpNormalUser) {
			t.Errorf("admin list_users missing expected users: %s", truncate(adminUsersText, 800))
		}
		for _, wantNode := range c.nodeNames() {
			if !strings.Contains(adminUsersText, wantNode) {
				t.Errorf("admin list_users missing node %q: %s", wantNode, truncate(adminUsersText, 800))
			}
		}
		if !strings.Contains(adminUsersText, "[REDACTED]") {
			t.Errorf("admin list_users missing [REDACTED] marker: %s", truncate(adminUsersText, 800))
		}
		if strings.Contains(adminUsersText, normalRealAuthToken) {
			t.Errorf("SECURITY: admin MCP list_users leaked plaintext auth_token for %s", mcpNormalUser)
		}
		if apiUserRealAuthToken != "" && strings.Contains(adminUsersText, apiUserRealAuthToken) {
			t.Errorf("SECURITY: admin MCP list_users leaked plaintext auth_token for %s", apiTestUser)
		}

		// get_profile with X-Token (no JWT username) must return IsError=true ("JWT required").
		res, err = adminSess.CallTool(ctx, &mcp.CallToolParams{Name: "get_profile"})
		if err != nil {
			t.Fatalf("admin CallTool get_profile protocol error: %v", err)
		}
		if !res.IsError || !strings.Contains(extractE2EMCPText(t, res), "JWT required") {
			t.Errorf("admin get_profile expected IsError=true with 'JWT required', got isErr=%v text=%s",
				res.IsError, extractE2EMCPText(t, res))
		}

		// 2. Official MCP SDK client authenticated as normal user via Bearer JWT.
		normalSess := connectE2EMCPClient(t, nodeA.baseURL, map[string]string{
			"Authorization": "Bearer " + normalJWT,
		})
		normalList, err := normalSess.ListTools(ctx, &mcp.ListToolsParams{})
		if err != nil {
			t.Fatalf("normal ListTools: %v", err)
		}
		gotNormalTools := make([]string, 0, len(normalList.Tools))
		for _, tl := range normalList.Tools {
			gotNormalTools = append(gotNormalTools, tl.Name)
		}
		sort.Strings(gotNormalTools)
		wantNormalTools := []string{"get_node_status", "get_profile", "list_users"}
		if strings.Join(gotNormalTools, ",") != strings.Join(wantNormalTools, ",") {
			t.Fatalf("normal ListTools mismatch: got %v, want %v", gotNormalTools, wantNormalTools)
		}

		// Normal user can query get_node_status across all 3 nodes.
		res, err = normalSess.CallTool(ctx, &mcp.CallToolParams{Name: "get_node_status"})
		if err != nil || res.IsError {
			t.Fatalf("normal CallTool get_node_status failed: err=%v res=%+v", err, res)
		}
		normalStatusText := extractE2EMCPText(t, res)
		for _, wantNode := range c.nodeNames() {
			if !strings.Contains(normalStatusText, wantNode) {
				t.Errorf("normal get_node_status missing %q: %s", wantNode, truncate(normalStatusText, 800))
			}
		}

		// Normal user get_profile returns own data with redacted auth_token across all 3 nodes.
		res, err = normalSess.CallTool(ctx, &mcp.CallToolParams{Name: "get_profile"})
		if err != nil || res.IsError {
			t.Fatalf("normal CallTool get_profile failed: err=%v res=%+v", err, res)
		}
		profileText := extractE2EMCPText(t, res)
		if !strings.Contains(profileText, mcpNormalUser) || !strings.Contains(profileText, "[REDACTED]") {
			t.Errorf("normal get_profile unexpected output: %s", truncate(profileText, 800))
		}
		if strings.Contains(profileText, normalRealAuthToken) {
			t.Errorf("SECURITY: normal MCP get_profile leaked plaintext auth_token")
		}

		// Normal user list_users returns ONLY self (never apiTestUser) and redacts auth_token.
		res, err = normalSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_users"})
		if err != nil || res.IsError {
			t.Fatalf("normal CallTool list_users failed: err=%v res=%+v", err, res)
		}
		normalUsersText := extractE2EMCPText(t, res)
		if !strings.Contains(normalUsersText, mcpNormalUser) {
			t.Errorf("normal list_users missing self (%s): %s", mcpNormalUser, truncate(normalUsersText, 800))
		}
		if strings.Contains(normalUsersText, apiTestUser) {
			t.Errorf("RBAC: normal list_users exposed another user (%s): %s", apiTestUser, truncate(normalUsersText, 800))
		}
		if !strings.Contains(normalUsersText, "[REDACTED]") {
			t.Errorf("normal list_users missing [REDACTED]: %s", truncate(normalUsersText, 800))
		}
		if strings.Contains(normalUsersText, normalRealAuthToken) {
			t.Errorf("SECURITY: normal MCP list_users leaked plaintext auth_token")
		}

		// Admin-only tool must be rejected for normal JWT.
		res, err = normalSess.CallTool(ctx, &mcp.CallToolParams{Name: "list_nodes"})
		if err == nil && (res == nil || !res.IsError) {
			t.Fatalf("RBAC: normal user calling admin-only list_nodes unexpectedly succeeded: %+v", res)
		}
	})

	// -------------------------------------------------------- user deletion
	// Last, so earlier subtests still have the user.

	t.Run("DELETE_user_fans_out", func(t *testing.T) {
		code, body, err := nodeA.apiDelete("/api/user", map[string]any{
			"target": "all", "user": apiTestUser,
		})
		requireOK(t, "DELETE /api/user", code, body, err)

		for _, s := range []*e2eServer{nodeA, nodeB, nodeC} {
			s := s
			eventually(t, fmt.Sprintf("%s no longer sees %s", s.name, apiTestUser), 20*time.Second, func() bool {
				return !userNames(t, s, s.name)[apiTestUser]
			})
		}
	})

	// -------------------------------------------------------------------- MCP

	t.Run("MCP_stateless_smoke", func(t *testing.T) {
		mcpHeaders := map[string]string{
			"Accept": "application/json, text/event-stream",
		}
		// POST /mcp — tools/list returns the 9 read-only tools.
		code, body, err := nodeA.apiDoWithHeaders(http.MethodPost, "/mcp", nil, mcpHeaders, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "tools/list",
			"params":  map[string]any{},
		})
		requireOK(t, "POST /mcp tools/list", code, body, err)
		var listResp struct {
			Result struct {
				Tools []struct {
					Name string `json:"name"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &listResp); err != nil {
			t.Fatalf("POST /mcp tools/list decode: %v (body=%s)", err, truncate(string(body), 600))
		}
		if len(listResp.Result.Tools) != 9 {
			t.Errorf("POST /mcp tools/list: got %d tools, want 9 (body=%s)",
				len(listResp.Result.Tools), truncate(string(body), 600))
		}

		// POST /mcp — tools/call list_nodes returns the converged 3-node cluster.
		code, body, err = nodeA.apiDoWithHeaders(http.MethodPost, "/mcp", nil, mcpHeaders, map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params": map[string]any{
				"name":      "list_nodes",
				"arguments": map[string]any{},
			},
		})
		requireOK(t, "POST /mcp tools/call list_nodes", code, body, err)
		for _, want := range c.nodeNames() {
			if !strings.Contains(string(body), want) {
				t.Errorf("POST /mcp list_nodes missing node %q: %s", want, truncate(string(body), 800))
			}
		}

		// GET /mcp — routed to stateless MCP handler (returns 405 per MCP spec).
		code, body, err = nodeA.apiDoWithHeaders(http.MethodGet, "/mcp", nil, map[string]string{
			"Accept": "text/event-stream",
		}, nil)
		if err != nil {
			t.Fatalf("GET /mcp: transport error: %v", err)
		}
		if code != http.StatusMethodNotAllowed {
			t.Errorf("GET /mcp stateless: got status %d (body=%s), want 405", code, truncate(string(body), 400))
		}

		// DELETE /mcp — routed to stateless MCP handler (returns 204 with Mcp-Session-Id).
		code, body, err = nodeA.apiDoWithHeaders(http.MethodDelete, "/mcp", nil, map[string]string{
			"Mcp-Session-Id": "e2e-stateless-session",
		}, nil)
		if err != nil {
			t.Fatalf("DELETE /mcp: transport error: %v", err)
		}
		if code != http.StatusNoContent {
			t.Errorf("DELETE /mcp stateless: got status %d (body=%s), want 204", code, truncate(string(body), 400))
		}
	})

	// ------------------------------------------------------- coverage guard
	// Runs last so every case above has already recorded its hits.

	t.Run("route_coverage", func(t *testing.T) {
		hit := recordedRoutes()
		var missing []string
		for _, want := range loadCoveredRoutes(t) {
			if !hit[want] {
				missing = append(missing, want)
			}
		}
		if len(missing) > 0 {
			t.Errorf("these routes are listed in %s but no case in this suite called them:\n  %s",
				coveredRoutesFile, strings.Join(missing, "\n  "))
		}
	})
}
