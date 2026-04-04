package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
)

func makeJWT(t *testing.T, s *HttpServer, username, role string) string {
	t.Helper()
	tokenStr, _, err := auth.GenerateJWT(auth.JWTConfig{
		Secret:      s.jwtSecret,
		ExpireHours: s.jwtExpireHours,
	}, username, role)
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}
	return tokenStr
}

func TestLogoutHandler_Success(t *testing.T) {
	ul := newMockUserLister()
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	// Verify token is valid before logout
	claims, err := auth.ParseJWT(s.jwtSecret, tokenStr)
	if err != nil {
		t.Fatalf("ParseJWT before logout: %v", err)
	}
	if auth.BlacklistContains(claims.JTI) {
		t.Fatal("token should not be blacklisted before logout")
	}

	handler := &LogoutHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/logout", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !auth.BlacklistContains(claims.JTI) {
		t.Fatal("expected JTI to be blacklisted after logout")
	}
}

func TestLogoutHandler_IdempotentBlacklist(t *testing.T) {
	// BlacklistAdd is idempotent: calling logout with an already-blacklisted JTI must
	// still return 200 (the handler must not fail on a double-blacklist).
	// A stub middleware is used to bypass the AuthMiddleware blacklist check
	// (which would reject the token before reaching the handler) so that
	// we can verify the handler's own idempotency.
	ul := newMockUserLister()
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "bob", "normal")

	claims, _ := auth.ParseJWT(s.jwtSecret, tokenStr)
	auth.BlacklistAdd(claims.JTI) // pre-blacklist

	handler := &LogoutHandler{}
	handler.setHttpServer(s)
	// Stub middleware: set username to simulate a passed JWT auth without blacklist check.
	stubMW := func(c *gin.Context) {
		c.Set(auth.ContextKeyUsername, "bob")
		c.Next()
	}
	r := handlerEngine("POST", "/logout", handler.handlerFunc, stubMW)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even for already-blacklisted token, got %d", w.Code)
	}
}

func TestLogoutHandler_NoToken_BlockedByMiddleware(t *testing.T) {
	// With AuthMiddleware applied, a request without credentials should get 401.
	ul := newMockUserLister()
	s := newTestHttpServer(ul)

	handler := &LogoutHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/logout", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/logout", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for no credentials, got %d", w.Code)
	}
}

func TestLogoutHandler_XToken_Rejected(t *testing.T) {
	// X-Token must not be accepted by /logout — it has no JWT jti to blacklist.
	ul := newMockUserLister()
	s := newTestHttpServer(ul)

	handler := &LogoutHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/logout", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("X-Token", s.token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for X-Token on /logout, got %d body=%s", w.Code, w.Body.String())
	}
}
