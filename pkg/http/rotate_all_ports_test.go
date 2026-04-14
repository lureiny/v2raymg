package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestRotateAllPortsHandler_NormalUser_CannotRotateOther(t *testing.T) {
	ul := newMockUserLister(
		&contracts.User{Username: "alice", Role: "normal"},
		&contracts.User{Username: "bob", Role: "normal"},
	)
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	handler := &RotateAllPortsHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateAllPorts", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotateAllPorts",
		jsonBody(map[string]string{"username": "bob"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotateAllPortsHandler_NoToken(t *testing.T) {
	ul := newMockUserLister()
	s := newTestHttpServer(ul)
	handler := &RotateAllPortsHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateAllPorts", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotateAllPorts", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRotateAllPortsHandler_NoAvailableNode_Returns502(t *testing.T) {
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal"})
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	handler := &RotateAllPortsHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateAllPorts", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotateAllPorts",
		jsonBody(map[string]string{"target": "nonexistent-node"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != 502 {
		t.Fatalf("expected 502 for nonexistent target, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotateAllPortsHandler_XToken_NoUsername(t *testing.T) {
	ul := newMockUserLister()
	s := newTestHttpServer(ul)
	handler := &RotateAllPortsHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateAllPorts", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotateAllPorts", nil)
	req.Header.Set("X-Token", s.token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}
