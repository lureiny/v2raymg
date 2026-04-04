package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestProfileHandler_Normal(t *testing.T) {
	ul := newMockUserLister(&contracts.User{
		Username: "alice",
		Role:     "normal",
	})
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	handler := &ProfileHandler{}
	handler.setHttpServer(s)

	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("GET", "/profile", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/profile", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["username"] != "alice" {
		t.Errorf("expected username=alice, got %v", resp["username"])
	}
	if resp["role"] != "normal" {
		t.Errorf("expected role=normal, got %v", resp["role"])
	}
}

func TestProfileHandler_Admin(t *testing.T) {
	ul := newMockUserLister(&contracts.User{
		Username: "admin-user",
		Role:     "admin",
	})
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "admin-user", "admin")

	handler := &ProfileHandler{}
	handler.setHttpServer(s)

	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("GET", "/profile", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/profile", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["role"] != "admin" {
		t.Errorf("expected role=admin, got %v", resp["role"])
	}
}

func TestProfileHandler_XToken_Rejected(t *testing.T) {
	// /profile is JWT-only; X-Token must be rejected with 401.
	ul := newMockUserLister()
	s := newTestHttpServer(ul)

	handler := &ProfileHandler{}
	handler.setHttpServer(s)

	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("GET", "/profile", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/profile", nil)
	req.Header.Set("X-Token", s.token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for X-Token on /profile, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestProfileHandler_NoToken(t *testing.T) {
	ul := newMockUserLister()
	s := newTestHttpServer(ul)
	handler := &ProfileHandler{}
	handler.setHttpServer(s)

	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("GET", "/profile", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/profile", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
