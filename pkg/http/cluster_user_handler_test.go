package http

import (
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
// Route registration: user CRUD always registered, node groups only when cluster enabled
// ---------------------------------------------------------------------------

// TestUserRoutes_AlwaysRegistered: user CRUD routes are always available.
func TestUserRoutes_AlwaysRegistered(t *testing.T) {
	s := NewHttpServer()
	s.Init(HttpServerConfig{
		Token:     "tok",
		JWTSecret: "secret",
	}, nil, &stubClusterNodes{}, nil, nil, false)

	routes := []struct{ method, path string }{
		{"GET", "/api/user"},
		{"POST", "/api/user"},
		{"PUT", "/api/user"},
		{"DELETE", "/api/user"},
	}
	for _, p := range routes {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(p.method, p.path, nil)
		s.RestfulServer.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s: expected route to exist (not 404)", p.method, p.path)
		}
	}
}

// TestNodeGroupRoutes_DisabledCluster_Return404: cluster disabled → node group routes not registered.
func TestNodeGroupRoutes_DisabledCluster_Return404(t *testing.T) {
	s := NewHttpServer()
	s.Init(HttpServerConfig{
		Token:     "tok",
		JWTSecret: "secret",
	}, nil, &stubClusterNodes{}, nil, nil, false)

	routes := []struct{ method, path string }{
		{"GET", "/api/node/test-node/groups"},
		{"PUT", "/api/node/test-node/groups"},
	}
	for _, p := range routes {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(p.method, p.path, nil)
		s.RestfulServer.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: expected 404 when cluster disabled, got %d", p.method, p.path, w.Code)
		}
	}
}

// TestNodeGroupRoutes_EnabledCluster_NotReturn404: cluster enabled → node group routes registered.
func TestNodeGroupRoutes_EnabledCluster_NotReturn404(t *testing.T) {
	s := NewHttpServer()
	s.Init(HttpServerConfig{
		Token:     "tok",
		JWTSecret: "secret",
	}, nil, &stubClusterNodes{}, nil, nil, true)

	routes := []struct{ method, path string }{
		{"GET", "/api/node/test-node/groups"},
		{"PUT", "/api/node/test-node/groups"},
	}
	for _, p := range routes {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(p.method, p.path, nil)
		s.RestfulServer.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s: expected route to exist (not 404) when cluster enabled", p.method, p.path)
		}
	}
}
