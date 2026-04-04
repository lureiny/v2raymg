package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lureiny/v2raymg/pkg/cluster"
)

// ---------------------------------------------------------------------------
// stubClusterNodes: minimal ClusterNodes implementation that returns no nodes
// ---------------------------------------------------------------------------

type stubClusterNodes struct{}

func (s *stubClusterNodes) GetNodesWithFilter(f cluster.NodeFilter) []*cluster.Node { return nil }
func (s *stubClusterNodes) GetClusterToken() string                                  { return "" }

// ---------------------------------------------------------------------------
// ClusterUserAddHandler — input validation
// ---------------------------------------------------------------------------

// TestClusterUserAddHandler_MissingUsername: no username field → 400
func TestClusterUserAddHandler_MissingUsername(t *testing.T) {
	s := newTestHttpServer(nil)
	handler := &ClusterUserAddHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/cluster-users", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cluster-users", jsonBody(map[string]string{"password": "pw"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing username, got %d body=%s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), "username") {
		t.Errorf("expected error message to mention 'username', got %q", w.Body.String())
	}
}

// TestClusterUserAddHandler_MissingPassword: no password field → 400
func TestClusterUserAddHandler_MissingPassword(t *testing.T) {
	s := newTestHttpServer(nil)
	handler := &ClusterUserAddHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/cluster-users", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cluster-users", jsonBody(map[string]string{"username": "alice"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing password, got %d body=%s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), "password") {
		t.Errorf("expected error message to mention 'password', got %q", w.Body.String())
	}
}

// TestClusterUserAddHandler_InvalidJSON: malformed JSON → 400
func TestClusterUserAddHandler_InvalidJSON(t *testing.T) {
	s := newTestHttpServer(nil)
	handler := &ClusterUserAddHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/cluster-users", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cluster-users", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// TestClusterUserAddHandler_BothMissing: empty body → 400 (username missing)
func TestClusterUserAddHandler_BothMissing(t *testing.T) {
	s := newTestHttpServer(nil)
	handler := &ClusterUserAddHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/cluster-users", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cluster-users", jsonBody(map[string]string{}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestClusterUserAddHandler_ValidInput_PassesValidation: valid username+password → proceeds
// past validation. GetTargetNodes returns [] → "no available node" (502), not 400.
func TestClusterUserAddHandler_ValidInput_PassesValidation(t *testing.T) {
	s := newTestHttpServer(nil)
	s.Name = "test-node"
	s.clusterNodes = &stubClusterNodes{}
	handler := &ClusterUserAddHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/cluster-users", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/cluster-users", jsonBody(map[string]interface{}{
		"username": "alice",
		"password": "pw",
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusBadRequest {
		t.Errorf("valid input should pass validation (not 400), got body=%s", w.Body.String())
	}
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for no available node, got %d", w.Code)
	}
	if !containsStr(w.Body.String(), "no available node") {
		t.Errorf("expected body to contain \"no available node\", got %q", w.Body.String())
	}
}

// TestClusterUserDeleteHandler_NoAvailableNode: valid path, no nodes → "no available node" (502).
func TestClusterUserDeleteHandler_NoAvailableNode(t *testing.T) {
	s := newTestHttpServer(nil)
	s.Name = "test-node"
	s.clusterNodes = &stubClusterNodes{}
	handler := &ClusterUserDeleteHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("DELETE", "/cluster-users/:name", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/cluster-users/alice", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
	if !containsStr(w.Body.String(), "no available node") {
		t.Errorf("expected body %q to contain \"no available node\"", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// ClusterUserUpdateHandler — no required-field validation (username from path)
// ---------------------------------------------------------------------------

// TestClusterUserUpdateHandler_InvalidJSON: malformed JSON → 400
func TestClusterUserUpdateHandler_InvalidJSON(t *testing.T) {
	s := newTestHttpServer(nil)
	handler := &ClusterUserUpdateHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/cluster-users/:name", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/cluster-users/alice", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// TestClusterUserUpdateHandler_NoAvailableNode: valid body, no nodes → "no available node" (502).
func TestClusterUserUpdateHandler_NoAvailableNode(t *testing.T) {
	s := newTestHttpServer(nil)
	s.Name = "test-node"
	s.clusterNodes = &stubClusterNodes{}
	handler := &ClusterUserUpdateHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/cluster-users/:name", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/cluster-users/alice", jsonBody(map[string]interface{}{
		"password": "newpw",
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
	if !containsStr(w.Body.String(), "no available node") {
		t.Errorf("expected body %q to contain \"no available node\"", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Route registration: disabled → 404, enabled → routes exist
// ---------------------------------------------------------------------------

// TestClusterUserRoutes_Disabled_Return404: ClusterUserEnabled=false → routes not registered
func TestClusterUserRoutes_Disabled_Return404(t *testing.T) {
	s := NewHttpServer()
	s.Init(HttpServerConfig{
		ClusterUserEnabled: false,
		Token:              "tok",
		JWTSecret:          "secret",
	}, nil, &stubClusterNodes{}, nil, nil)

	routes := []struct{ method, path string }{
		{"GET", "/api/cluster-users"},
		{"POST", "/api/cluster-users"},
		{"PUT", "/api/cluster-users/alice"},
		{"DELETE", "/api/cluster-users/alice"},
		{"GET", "/api/node/test-node/groups"},
		{"PUT", "/api/node/test-node/groups"},
	}
	for _, p := range routes {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(p.method, p.path, nil)
		s.RestfulServer.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: expected 404 when disabled, got %d", p.method, p.path, w.Code)
		}
	}
}

// TestClusterUserRoutes_Enabled_NotReturn404: ClusterUserEnabled=true → routes registered
// (may return 401/403 from auth middleware, but NOT 404)
func TestClusterUserRoutes_Enabled_NotReturn404(t *testing.T) {
	s := NewHttpServer()
	s.Init(HttpServerConfig{
		ClusterUserEnabled: true,
		Token:              "tok",
		JWTSecret:          "secret",
	}, nil, &stubClusterNodes{}, nil, nil)

	routes := []struct{ method, path string }{
		{"GET", "/api/cluster-users"},
		{"POST", "/api/cluster-users"},
		{"PUT", "/api/cluster-users/alice"},
		{"DELETE", "/api/cluster-users/alice"},
		{"GET", "/api/node/test-node/groups"},
		{"PUT", "/api/node/test-node/groups"},
	}
	for _, p := range routes {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(p.method, p.path, nil)
		s.RestfulServer.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s: expected route to exist (not 404) when enabled", p.method, p.path)
		}
	}
}
