package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestSetUserRoleHandler_SetAdmin(t *testing.T) {
	ul := newMockUserLister(&contracts.User{Username: "bob", Role: "normal"})
	s := newTestHttpServer(ul)
	handler := &SetUserRoleHandler{}
	handler.setHttpServer(s)

	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	admMw := auth.AdminOnly()
	r := handlerEngine("PUT", "/user/:name/role", handler.handlerFunc, mw, admMw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/user/bob/role", jsonBody(map[string]string{"role": "admin"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", s.token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	// Verify role was updated in mock
	if ul.users["bob"].Role != "admin" {
		t.Errorf("expected role=admin after update, got %q", ul.users["bob"].Role)
	}
}

func TestSetUserRoleHandler_SetNormal(t *testing.T) {
	ul := newMockUserLister(&contracts.User{Username: "carol", Role: "admin"})
	s := newTestHttpServer(ul)
	handler := &SetUserRoleHandler{}
	handler.setHttpServer(s)

	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	admMw := auth.AdminOnly()
	r := handlerEngine("PUT", "/user/:name/role", handler.handlerFunc, mw, admMw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/user/carol/role", jsonBody(map[string]string{"role": "normal"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", s.token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

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

func TestSetUserRoleHandler_UserNotFound(t *testing.T) {
	ul := newMockUserLister() // empty
	s := newTestHttpServer(ul)
	handler := &SetUserRoleHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("PUT", "/user/:name/role", handler.handlerFunc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/user/nobody/role", jsonBody(map[string]string{"role": "admin"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
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

func TestSetUserRoleHandler_JWTAdmin_Allowed(t *testing.T) {
	// JWT admin can also update user roles (not restricted to X-Token only).
	ul := newMockUserLister(&contracts.User{Username: "frank", Role: "normal"})
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "admin-user", "admin")
	handler := &SetUserRoleHandler{}
	handler.setHttpServer(s)

	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	admMw := auth.AdminOnly()
	r := handlerEngine("PUT", "/user/:name/role", handler.handlerFunc, mw, admMw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/user/frank/role", jsonBody(map[string]string{"role": "admin"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for JWT admin, got %d body=%s", w.Code, w.Body.String())
	}
}
