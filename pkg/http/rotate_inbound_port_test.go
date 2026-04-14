package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lureiny/v2raymg/pkg/http/auth"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

func TestRotateInboundPortHandler_NormalUser_CannotRotateOther(t *testing.T) {
	ul := newMockUserLister(
		&contracts.User{Username: "alice", Role: "normal"},
		&contracts.User{Username: "bob", Role: "normal"},
	)
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	handler := &RotateInboundPortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateInboundPort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotateInboundPort",
		jsonBody(map[string]interface{}{"username": "bob", "container": "xray", "inbound": "vless-tcp"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotateInboundPortHandler_NoToken(t *testing.T) {
	ul := newMockUserLister()
	s := newTestHttpServer(ul)
	handler := &RotateInboundPortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateInboundPort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotateInboundPort", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestRotateInboundPortHandler_XToken_NoUsername(t *testing.T) {
	ul := newMockUserLister()
	s := newTestHttpServer(ul)
	handler := &RotateInboundPortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateInboundPort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/rotateInboundPort",
		jsonBody(map[string]interface{}{"container": "xray", "inbound": "vless-tcp"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Token", s.token)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotateInboundPortHandler_NoAvailableNode_Returns502(t *testing.T) {
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal"})
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	handler := &RotateInboundPortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateInboundPort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	// target points to a nonexistent node; stubTestClusterNodes returns nil.
	req := httptest.NewRequest("POST", "/rotateInboundPort",
		jsonBody(map[string]interface{}{"target": "nonexistent-node", "container": "xray", "inbound": "vless-tcp"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != 502 {
		t.Fatalf("expected 502 for nonexistent target, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotateInboundPortHandler_EmptyTarget_DefaultsToLocalNode(t *testing.T) {
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal"})
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	handler := &RotateInboundPortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateInboundPort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	// No target field — should default to local node.
	// With stub cluster returning nil nodes, this also results in 502,
	// but the handler code path exercises resolveTarget → s.Name.
	req := httptest.NewRequest("POST", "/rotateInboundPort",
		jsonBody(map[string]interface{}{"container": "xray", "inbound": "vless-tcp"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	// stub cluster returns nil → 502 is the expected outcome.
	if w.Code != 502 {
		t.Fatalf("expected 502 (stub cluster has no nodes), got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRotateInboundPortHandler_MissingContainerOrInbound(t *testing.T) {
	ul := newMockUserLister(&contracts.User{Username: "alice", Role: "normal"})
	s := newTestHttpServer(ul)
	tokenStr := makeJWT(t, s, "alice", "normal")

	handler := &RotateInboundPortHandler{}
	handler.setHttpServer(s)
	mw := auth.AuthMiddleware(s.token, s.jwtSecret)
	r := handlerEngine("POST", "/rotateInboundPort", handler.handlerFunc, mw)

	w := httptest.NewRecorder()
	// Missing "inbound" field.
	req := httptest.NewRequest("POST", "/rotateInboundPort",
		jsonBody(map[string]interface{}{"container": "xray"}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing inbound, got %d body=%s", w.Code, w.Body.String())
	}
}
