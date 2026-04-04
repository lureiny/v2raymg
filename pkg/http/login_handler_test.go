package http

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// handlerEngine creates a fresh gin engine with the given handler registered.
func handlerEngine(method, path string, h gin.HandlerFunc, middlewares ...gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Handle(method, path, append(middlewares, h)...)
	return r
}

// jsonBody encodes v as a JSON request body.
func jsonBody(v interface{}) *bytes.Buffer {
	data, _ := json.Marshal(v)
	return bytes.NewBuffer(data)
}

func TestLoginHandler_Success_Admin(t *testing.T) {
	adminHash, err := auth.HashLoginPassword("admin-pass")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ul := newMockUserLister(&contracts.User{
		Username: "admin", Role: "admin", LoginPassword: adminHash,
	})
	s := newTestHttpServer(ul)
	handler := &LoginHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/login", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", jsonBody(map[string]string{"username": "admin", "password": "admin-pass"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["role"] != "admin" {
		t.Errorf("expected role=admin, got %v", resp["role"])
	}
	if resp["token"] == nil || resp["token"] == "" {
		t.Error("expected non-empty token")
	}
	if resp["username"] != "admin" {
		t.Errorf("expected username=admin, got %v", resp["username"])
	}
}

func TestLoginHandler_Success_Normal(t *testing.T) {
	hash, _ := auth.HashLoginPassword("normal-pass")
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal", LoginPassword: hash})
	s := newTestHttpServer(ul)
	handler := &LoginHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/login", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", jsonBody(map[string]string{"username": "alice", "password": "normal-pass"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["role"] != "normal" {
		t.Errorf("expected role=normal, got %v", resp["role"])
	}
}

func TestLoginHandler_WrongPassword(t *testing.T) {
	hash, _ := auth.HashLoginPassword("correct-pass")
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal", LoginPassword: hash})
	s := newTestHttpServer(ul)
	handler := &LoginHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/login", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", jsonBody(map[string]string{"username": "alice", "password": "wrong-pass"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestLoginHandler_UserNotFound(t *testing.T) {
	ul := newMockUserLister() // empty
	s := newTestHttpServer(ul)
	handler := &LoginHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/login", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", jsonBody(map[string]string{"username": "nobody", "password": "pass"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestLoginHandler_MissingFields(t *testing.T) {
	ul := newMockUserLister()
	s := newTestHttpServer(ul)
	handler := &LoginHandler{}
	handler.setHttpServer(s)
	r := handlerEngine("POST", "/login", handler.handlerFunc)

	for _, body := range []map[string]string{
		{"password": "pass"},  // missing username
		{"username": "alice"}, // missing password
		{},                    // both missing
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/login", jsonBody(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body=%v: expected 400, got %d", body, w.Code)
		}
	}
}

func TestLoginHandler_SHA256HexInput(t *testing.T) {
	// Client pre-hashes password with SHA256 before sending.
	plaintext := "test-password"
	sum := sha256.Sum256([]byte(plaintext))
	sha256hex := hex.EncodeToString(sum[:])

	// Server stores bcrypt(sha256(plaintext)) — same result whether client sends plaintext or hex.
	hash, _ := auth.HashLoginPassword(plaintext)
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal", LoginPassword: hash})
	s := newTestHttpServer(ul)
	handler := &LoginHandler{}
	handler.setHttpServer(s)

	r := handlerEngine("POST", "/login", handler.handlerFunc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/login", jsonBody(map[string]string{"username": "alice", "password": sha256hex}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for SHA256 hex input, got %d body=%s", w.Code, w.Body.String())
	}
}
