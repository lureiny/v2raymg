package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// NodeGroupsGetHandler
// ---------------------------------------------------------------------------

// TestNodeGroupsGetHandler_NoAvailableNode: GetTargetNodes returns [] → "no available node" (502).
func TestNodeGroupsGetHandler_NoAvailableNode(t *testing.T) {
	s := newTestHttpServer(nil)
	s.Name = "test-node"
	s.clusterNodes = &stubClusterNodes{}
	handler := &NodeGroupsGetHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("GET", "/node/:name/groups", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/node/test-node/groups", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
	if !containsStr(w.Body.String(), "no available node") {
		t.Errorf("expected body %q to contain \"no available node\"", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// NodeGroupsSetHandler
// ---------------------------------------------------------------------------

// TestNodeGroupsSetHandler_InvalidJSON: malformed body → 400.
func TestNodeGroupsSetHandler_InvalidJSON(t *testing.T) {
	s := newTestHttpServer(nil)
	s.Name = "test-node"
	s.clusterNodes = &stubClusterNodes{}
	handler := &NodeGroupsSetHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/node/:name/groups", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/node/test-node/groups", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestNodeGroupsSetHandler_NoAvailableNode: valid body, no nodes → "no available node" (502).
func TestNodeGroupsSetHandler_NoAvailableNode(t *testing.T) {
	s := newTestHttpServer(nil)
	s.Name = "test-node"
	s.clusterNodes = &stubClusterNodes{}
	handler := &NodeGroupsSetHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/node/:name/groups", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/node/test-node/groups", jsonBody(map[string]interface{}{
		"groups": []string{"default", "hk"},
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d body=%s", w.Code, w.Body.String())
	}
	if !containsStr(w.Body.String(), "no available node") {
		t.Errorf("expected body %q to contain \"no available node\"", w.Body.String())
	}
}

// TestNodeGroupsSetHandler_EmptyGroups_NoAvailableNode: empty groups list, no nodes → "no available node".
func TestNodeGroupsSetHandler_EmptyGroups_NoAvailableNode(t *testing.T) {
	s := newTestHttpServer(nil)
	s.Name = "test-node"
	s.clusterNodes = &stubClusterNodes{}
	handler := &NodeGroupsSetHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/node/:name/groups", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/node/test-node/groups", jsonBody(map[string]interface{}{
		"groups": []string{},
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
