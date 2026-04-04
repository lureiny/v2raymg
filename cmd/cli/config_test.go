package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// resetState resets globalConfig and jwtCache to a clean baseline for each test.
func resetState(host, username, password, staticToken string) {
	globalConfig.Host = host
	globalConfig.Username = username
	globalConfig.Password = password
	globalConfig.Token = staticToken

	jwtCache.mu.Lock()
	jwtCache.token = ""
	jwtCache.expire = 0
	jwtCache.mu.Unlock()
}

// newLoginServer creates a mock /login server that returns a JWT with the given expiry.
// Each call to /login increments loginCalls.
func newLoginServer(t *testing.T, returnToken string, expireOffset int64, loginCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/login" {
			http.NotFound(w, r)
			return
		}
		loginCalls.Add(1)
		expire := time.Now().Unix() + expireOffset
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":  returnToken,
			"expire": expire,
		})
	}))
}

// newFailingLoginServer creates a mock /login server that always returns 401.
func newFailingLoginServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
}

func TestGetAuthToken_JWTSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := newLoginServer(t, "fresh-jwt", 3600, &calls)
	defer srv.Close()

	resetState(srv.URL, "alice", "secret", "static-token")

	token := getAuthToken()
	if token != "Bearer fresh-jwt" {
		t.Fatalf("expected 'Bearer fresh-jwt', got %q", token)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 login call, got %d", calls.Load())
	}
}

func TestGetAuthToken_CacheHit(t *testing.T) {
	var calls atomic.Int32
	srv := newLoginServer(t, "cached-jwt", 3600, &calls)
	defer srv.Close()

	resetState(srv.URL, "alice", "secret", "")

	// First call populates the cache.
	getAuthToken()
	// Second call should reuse the cache without hitting /login again.
	token := getAuthToken()

	if token != "Bearer cached-jwt" {
		t.Fatalf("expected cached token, got %q", token)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected exactly 1 login call (cache hit on 2nd), got %d", calls.Load())
	}
}

func TestGetAuthToken_NearExpiryRefresh(t *testing.T) {
	var calls atomic.Int32
	srv := newLoginServer(t, "refreshed-jwt", 3600, &calls)
	defer srv.Close()

	resetState(srv.URL, "alice", "secret", "")

	// Pre-seed the cache with a token that expires in 30s (< 60s buffer → must refresh).
	jwtCache.mu.Lock()
	jwtCache.token = "expiring-soon"
	jwtCache.expire = time.Now().Unix() + 30
	jwtCache.mu.Unlock()

	token := getAuthToken()

	if token != "Bearer refreshed-jwt" {
		t.Fatalf("expected refreshed token, got %q", token)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 login call for refresh, got %d", calls.Load())
	}
}

func TestGetAuthToken_LoginFailureFallback(t *testing.T) {
	srv := newFailingLoginServer(t)
	defer srv.Close()

	resetState(srv.URL, "alice", "wrongpass", "fallback-xtoken")

	token := getAuthToken()

	if token != "fallback-xtoken" {
		t.Fatalf("expected fallback X-Token, got %q", token)
	}
}

func TestGetAuthToken_NoCredentials_ReturnsStaticToken(t *testing.T) {
	resetState("http://unused", "", "", "only-xtoken")

	token := getAuthToken()

	if token != "only-xtoken" {
		t.Fatalf("expected static token when no credentials, got %q", token)
	}
}

func TestClearJWTCache(t *testing.T) {
	jwtCache.mu.Lock()
	jwtCache.token = "some-token"
	jwtCache.expire = time.Now().Unix() + 3600
	jwtCache.mu.Unlock()

	ClearJWTCache()

	jwtCache.mu.Lock()
	tok := jwtCache.token
	exp := jwtCache.expire
	jwtCache.mu.Unlock()

	if tok != "" || exp != 0 {
		t.Fatalf("expected cache cleared, got token=%q expire=%d", tok, exp)
	}
}
