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

func TestRotatePortHandler_NormalUser_RotatesOwn(t *testing.T) {
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal"})
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	handler := &RotatePortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotatePort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotatePort", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotatePortHandler_Admin_RotatesOtherUser(t *testing.T) {
	ul := newMockUserLister(
		&contracts.User{Username: "admin-user", Role: "admin"},
		&contracts.User{Username: "alice", Role: "normal"},
	)
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "admin-user", "admin")

	handler := &RotatePortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotatePort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotatePort", jsonBody(map[string]string{"username": "alice"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotatePortHandler_NormalUser_CannotRotateOther(t *testing.T) {
	ul := newMockUserLister(
		&contracts.User{Username: "alice", Role: "normal"},
		&contracts.User{Username: "bob", Role: "normal"},
	)
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	handler := &RotatePortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotatePort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotatePort", jsonBody(map[string]string{"username": "bob"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotatePortHandler_NoToken(t *testing.T) {
	ul := newMockUserLister()
	s := newTestHttpServer(ul)
	handler := &RotatePortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotatePort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotatePort", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRotatePortHandler_XToken_WithUsername(t *testing.T) {
	// X-Token + explicit body username → 200 (break-glass admin rotates target user).
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal"})
	s := newTestHttpServer(ul)
	handler := &RotatePortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotatePort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotatePort", jsonBody(map[string]string{"username": "alice"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", s.token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotatePortHandler_XToken_NoUsername(t *testing.T) {
	// X-Token without body username → 400 (param error, not 401).
	ul := newMockUserLister()
	s := newTestHttpServer(ul)
	handler := &RotatePortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotatePort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotatePort", nil)
	req.Header.Set("X-Token", s.token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (not 401) for X-Token without username, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotatePortHandler_XToken_NonexistentUsername(t *testing.T) {
	// X-Token + nonexistent username → 200 with business error code 300.
	ul := newMockUserLister()
	ul.rotatePortErrs["ghost"] = fmt.Errorf("user ghost not found")
	s := newTestHttpServer(ul)
	handler := &RotatePortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotatePort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotatePort", jsonBody(map[string]string{"username": "ghost"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", s.token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for nonexistent user, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if code, _ := resp["code"].(float64); code != 300 {
		t.Errorf("expected business code 300 for nonexistent user, got %v", resp["code"])
	}
}

func TestRotatePortHandler_AdminJWT_NonexistentUsername(t *testing.T) {
	// admin JWT + nonexistent username → 200 with business error code 300.
	ul := newMockUserLister(&contracts.User{Username: "admin-user", Role: "admin"})
	ul.rotatePortErrs["ghost"] = fmt.Errorf("user ghost not found")
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "admin-user", "admin")
	handler := &RotatePortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotatePort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotatePort", jsonBody(map[string]string{"username": "ghost"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for nonexistent user, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if code, _ := resp["code"].(float64); code != 300 {
		t.Errorf("expected business code 300 for nonexistent user, got %v", resp["code"])
	}
}
