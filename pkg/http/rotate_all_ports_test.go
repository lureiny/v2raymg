package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestRotateAllPortsHandler_NormalUser_RotatesOwn(t *testing.T) {
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal"})
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	handler := &RotateAllPortsHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateAllPorts", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotateAllPorts", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotateAllPortsHandler_Admin_RotatesOtherUser(t *testing.T) {
	ul := newMockUserLister(
		&contracts.User{Username: "admin-user", Role: "admin"},
		&contracts.User{Username: "alice", Role: "normal"},
	)
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "admin-user", "admin")

	handler := &RotateAllPortsHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateAllPorts", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotateAllPorts",
		jsonBody(map[string]string{"username": "alice"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

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

func TestRotateAllPortsHandler_XToken_WithUsername(t *testing.T) {
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal"})
	s := newTestHttpServer(ul)
	handler := &RotateAllPortsHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateAllPorts", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotateAllPorts",
		jsonBody(map[string]string{"username": "alice"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", s.token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
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

func TestRotateAllPortsHandler_RotationError_Returns300(t *testing.T) {
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal"})
	ul.rotateAllPortsErr["alice"] = fmt.Errorf("all ports failed")
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	handler := &RotateAllPortsHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateAllPorts", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotateAllPorts", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for rotation error, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if code, _ := resp["code"].(float64); code != 300 {
		t.Errorf("expected business code 300, got %v", resp["code"])
	}
}
