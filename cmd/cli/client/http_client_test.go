package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- setAuthHeader tests ---

func TestSetAuthHeader_BearerToken(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	setAuthHeader(req, "Bearer eyJhbGciOiJIUzI1NiJ9.test.sig")
	if got := req.Header.Get("Authorization"); got != "Bearer eyJhbGciOiJIUzI1NiJ9.test.sig" {
		t.Fatalf("expected Authorization header, got %q", got)
	}
	if got := req.Header.Get("X-Token"); got != "" {
		t.Fatalf("expected no X-Token header, got %q", got)
	}
}

func TestSetAuthHeader_StaticToken(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	setAuthHeader(req, "my-static-token")
	if got := req.Header.Get("X-Token"); got != "my-static-token" {
		t.Fatalf("expected X-Token header, got %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected no Authorization header, got %q", got)
	}
}

func TestSetAuthHeader_EmptyToken(t *testing.T) {
	req, _ := http.NewRequest("GET", "/", nil)
	setAuthHeader(req, "")
	if got := req.Header.Get("X-Token"); got != "" {
		t.Fatalf("expected no X-Token header, got %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("expected no Authorization header, got %q", got)
	}
}

// --- RotatePort request body tests ---

func TestRotatePort_SendsTokenAndEmptyBody(t *testing.T) {
	var captured struct {
		method string
		path   string
		token  string
		xtoken string
		body   map[string]interface{}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.token = r.Header.Get("Authorization")
		captured.xtoken = r.Header.Get("X-Token")
		json.NewDecoder(r.Body).Decode(&captured.body)
		w.WriteHeader(200)
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	RotatePort(srv.URL, "Bearer testjwt", "")

	if captured.method != "POST" {
		t.Fatalf("expected POST, got %s", captured.method)
	}
	if captured.path != "/rotatePort" {
		t.Fatalf("expected /rotatePort, got %s", captured.path)
	}
	if captured.token != "Bearer testjwt" {
		t.Fatalf("expected Authorization header, got %q", captured.token)
	}
	if _, ok := captured.body["username"]; ok {
		t.Fatal("expected no username in body when empty")
	}
}

func TestRotatePort_SendsUsernameWhenSet(t *testing.T) {
	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	RotatePort(srv.URL, "Bearer admin-jwt", "alice")

	if capturedBody["username"] != "alice" {
		t.Fatalf("expected username=alice in body, got %v", capturedBody["username"])
	}
}

// --- RotateInboundPort request body tests ---

func TestRotateInboundPort_SendsContainerAndInbound(t *testing.T) {
	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(200)
		w.Write([]byte(`{"code":0,"port":12345}`))
	}))
	defer srv.Close()

	RotateInboundPort(srv.URL, "Bearer jwt", "", "xray", "vless-tcp", 0)

	if capturedBody["container"] != "xray" {
		t.Fatalf("expected container=xray, got %v", capturedBody["container"])
	}
	if capturedBody["inbound"] != "vless-tcp" {
		t.Fatalf("expected inbound=vless-tcp, got %v", capturedBody["inbound"])
	}
	if _, ok := capturedBody["username"]; ok {
		t.Fatal("expected no username when not set")
	}
}

// --- RotateAllPorts request body tests ---

func TestRotateAllPorts_EmptyBodyWhenNoUsername(t *testing.T) {
	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.WriteHeader(200)
		w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	RotateAllPorts(srv.URL, "Bearer jwt", "")

	if _, ok := capturedBody["username"]; ok {
		t.Fatal("expected no username field when not specified")
	}
}

// --- Logout request tests ---

func TestLogout_SendsBearerTokenAndPOST(t *testing.T) {
	var captured struct {
		method string
		path   string
		auth   string
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.auth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	Logout(srv.URL, "Bearer testjwt")

	if captured.method != "POST" {
		t.Fatalf("expected POST, got %s", captured.method)
	}
	if captured.path != "/logout" {
		t.Fatalf("expected /logout, got %s", captured.path)
	}
	if captured.auth != "Bearer testjwt" {
		t.Fatalf("expected Authorization header, got %q", captured.auth)
	}
}

// --- Profile request tests ---

func TestProfile_SendsGETWithBearerToken(t *testing.T) {
	var captured struct {
		method string
		path   string
		auth   string
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.auth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		w.Write([]byte(`{"username":"alice","role":"normal"}`))
	}))
	defer srv.Close()

	result, err := Profile(srv.URL, "Bearer testjwt")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured.method != "GET" {
		t.Fatalf("expected GET, got %s", captured.method)
	}
	if captured.path != "/profile" {
		t.Fatalf("expected /profile, got %s", captured.path)
	}
	if captured.auth != "Bearer testjwt" {
		t.Fatalf("expected Authorization header, got %q", captured.auth)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

// --- SetUserRole request tests ---

func TestSetUserRole_SendsCorrectRequest(t *testing.T) {
	var captured struct {
		method string
		path   string
		token  string
		body   map[string]interface{}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		captured.token = r.Header.Get("X-Token")
		json.NewDecoder(r.Body).Decode(&captured.body)
		w.WriteHeader(200)
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	SetUserRole(srv.URL, "admin-token", "bob", "admin")

	if captured.method != "PUT" {
		t.Fatalf("expected PUT, got %s", captured.method)
	}
	if captured.path != "/user/bob/role" {
		t.Fatalf("expected /user/bob/role, got %s", captured.path)
	}
	if captured.token != "admin-token" {
		t.Fatalf("expected X-Token=admin-token, got %q", captured.token)
	}
	if captured.body["role"] != "admin" {
		t.Fatalf("expected role=admin in body, got %v", captured.body["role"])
	}
}

// --- SetUserBandwidth request tests ---

func TestSetUserBandwidth_SendsCorrectRequest(t *testing.T) {
	var captured struct {
		method string
		path   string
		body   map[string]interface{}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&captured.body)
		w.WriteHeader(200)
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	up := int64(1000000)
	down := int64(500000)
	SetUserBandwidth(srv.URL, "my-token", "node1", "alice", &up, &down)

	if captured.method != "PUT" {
		t.Fatalf("expected PUT, got %s", captured.method)
	}
	if captured.path != "/api/user/alice/bandwidth" {
		t.Fatalf("expected /api/user/alice/bandwidth, got %s", captured.path)
	}
	if captured.body["upload_bps"] != float64(1000000) {
		t.Fatalf("expected upload_bps=1000000, got %v", captured.body["upload_bps"])
	}
	if captured.body["download_bps"] != float64(500000) {
		t.Fatalf("expected download_bps=500000, got %v", captured.body["download_bps"])
	}
	if captured.body["target"] != "node1" {
		t.Fatalf("expected target=node1, got %v", captured.body["target"])
	}
}

func TestSetUserBandwidth_OmitsNilFields(t *testing.T) {
	var captured struct {
		body map[string]interface{}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured.body)
		w.WriteHeader(200)
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	up := int64(1000000)
	SetUserBandwidth(srv.URL, "my-token", "", "alice", &up, nil)

	if _, ok := captured.body["download_bps"]; ok {
		t.Fatalf("expected download_bps to be absent, got %v", captured.body["download_bps"])
	}
	if captured.body["upload_bps"] != float64(1000000) {
		t.Fatalf("expected upload_bps=1000000, got %v", captured.body["upload_bps"])
	}
}

// --- SetUserClientLimit request tests ---

func TestSetUserClientLimit_SendsCorrectRequest(t *testing.T) {
	var captured struct {
		method string
		path   string
		body   map[string]interface{}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.method = r.Method
		captured.path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&captured.body)
		w.WriteHeader(200)
		w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer srv.Close()

	SetUserClientLimit(srv.URL, "my-token", "", "bob", 3, 60, 2)

	if captured.method != "PUT" {
		t.Fatalf("expected PUT, got %s", captured.method)
	}
	if captured.path != "/api/user/bob/client-limit" {
		t.Fatalf("expected /api/user/bob/client-limit, got %s", captured.path)
	}
	if captured.body["max_clients"] != float64(3) {
		t.Fatalf("expected max_clients=3, got %v", captured.body["max_clients"])
	}
	if captured.body["recycle_delay_sec"] != float64(60) {
		t.Fatalf("expected recycle_delay_sec=60, got %v", captured.body["recycle_delay_sec"])
	}
	if captured.body["drain_sec"] != float64(2) {
		t.Fatalf("expected drain_sec=2, got %v", captured.body["drain_sec"])
	}
}

func TestListNode_Unauthorized_ReturnsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Unauthorized"))
	}))
	defer srv.Close()

	_, err := ListNode(srv.URL, "bad-token")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "HTTP 401: Unauthorized" {
		t.Fatalf("expected clear HTTP error, got %q", got)
	}
}

func TestListCert_Forbidden_ReturnsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("Admin only"))
	}))
	defer srv.Close()

	_, err := ListCert(srv.URL, "normal-jwt", "all")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "HTTP 403: Admin only" {
		t.Fatalf("expected clear HTTP error, got %q", got)
	}
}
