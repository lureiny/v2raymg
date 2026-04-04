package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lureiny/v2raymg/pkg/http/auth"
)

func TestGetCertsHandler_NoToken_BlockedByMiddleware(t *testing.T) {
	s := newTestHttpServer(newMockUserLister())
	handler := &GetCertsHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	admMw := auth.AdminOnly()
	r := handlerEngine("GET", "/getCerts", handler.handlerFunc, mw, admMw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/getCerts", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}
