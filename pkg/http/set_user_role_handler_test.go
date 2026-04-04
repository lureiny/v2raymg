package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestSetUserRoleHandler_InvalidRole(t *testing.T) {
	ul := newMockUserLister(&contracts.User{Username: "dave", Role: "normal"})
	s := newTestHttpServer(ul)
	handler := &SetUserRoleHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/user/:name/role", handler.handlerFunc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/user/dave/role", jsonBody(map[string]string{"role": "superuser"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid role, got %d", w.Code)
	}
}

func TestSetUserRoleHandler_MissingRole(t *testing.T) {
	ul := newMockUserLister()
	s := newTestHttpServer(ul)
	handler := &SetUserRoleHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/user/:name/role", handler.handlerFunc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/user/bob/role", jsonBody(map[string]string{}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing role, got %d", w.Code)
	}
}

func TestSetUserRoleHandler_NoNodes_ReturnsError(t *testing.T) {
	// When no nodes are available (stubTestClusterNodes returns nil), handler returns error.
	ul := newMockUserLister(&contracts.User{Username: "bob", Role: "normal"})
	s := newTestHttpServer(ul)
	handler := &SetUserRoleHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/user/:name/role", handler.handlerFunc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/user/bob/role", jsonBody(map[string]string{"role": "admin"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (with error code in body), got %d", w.Code)
	}
}

func TestSetUserRoleHandler_JWTNormal_Rejected(t *testing.T) {
	// Normal user JWT cannot access admin-only endpoint.
	ul := newMockUserLister(&contracts.User{Username: "eve", Role: "normal"})
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "eve", "normal")
	handler := &SetUserRoleHandler{}
	handler.setHttpServer(s)

	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	admMw := auth.AdminOnly()
	r := handlerEngine("PUT", "/user/:name/role", handler.handlerFunc, mw, admMw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/user/eve/role", jsonBody(map[string]string{"role": "admin"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for normal user JWT, got %d", w.Code)
	}
}
