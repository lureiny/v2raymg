package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newAuthRouter creates a gin router with AuthMiddleware applied and a simple GET /test handler.
func newAuthRouter(staticToken, jwtSecret string) *gin.Engine {
	r := gin.New()
	r.Use(AuthMiddleware(staticToken, jwtSecret))
	r.GET("/test", func(c *gin.Context) {
		role, _ := c.Get(ContextKeyRole)
		username, _ := c.Get(ContextKeyUsername)
		c.JSON(200, gin.H{"role": role, "username": username})
	})
	return r
}

func doRequest(r *gin.Engine, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestAuthMiddleware_XTokenValid(t *testing.T) {
	r := newAuthRouter("static-token", "jwt-secret")
	w := doRequest(r, "GET", "/test", map[string]string{"X-Token": "static-token"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAuthMiddleware_XTokenInvalid(t *testing.T) {
	r := newAuthRouter("static-token", "jwt-secret")
	w := doRequest(r, "GET", "/test", map[string]string{"X-Token": "wrong-token"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_BearerJWTValid(t *testing.T) {
	secret := "jwt-test-secret"
	tokenStr, _, err := GenerateJWT(JWTConfig{Secret: secret, ExpireHours: 1}, "alice", "normal")
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}
	r := newAuthRouter("static-token", secret)
	w := doRequest(r, "GET", "/test", map[string]string{"Authorization": "Bearer " + tokenStr})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAuthMiddleware_BearerJWTBlacklisted(t *testing.T) {
	secret := "jwt-test-secret-bl"
	tokenStr, _, err := GenerateJWT(JWTConfig{Secret: secret, ExpireHours: 1}, "alice", "normal")
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}
	claims, _ := ParseJWT(secret, tokenStr)
	BlacklistAdd(claims.JTI)

	r := newAuthRouter("other-token", secret)
	w := doRequest(r, "GET", "/test", map[string]string{"Authorization": "Bearer " + tokenStr})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for blacklisted token, got %d", w.Code)
	}
}

func TestAuthMiddleware_NoCredentials(t *testing.T) {
	r := newAuthRouter("static-token", "jwt-secret")
	w := doRequest(r, "GET", "/test", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_MalformedBearer(t *testing.T) {
	r := newAuthRouter("static-token", "jwt-secret")
	w := doRequest(r, "GET", "/test", map[string]string{"Authorization": "Bearer not.a.valid.token"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// adminOnlyRouter creates a router with both AuthMiddleware and AdminOnly applied.
func adminOnlyRouter(staticToken, jwtSecret string) *gin.Engine {
	r := gin.New()
	r.Use(AuthMiddleware(staticToken, jwtSecret), AdminOnly())
	r.GET("/admin", func(c *gin.Context) { c.String(200, "ok") })
	return r
}

func TestAdminOnly_RoleAdmin_ViaXToken(t *testing.T) {
	r := adminOnlyRouter("admin-token", "secret")
	w := doRequest(r, "GET", "/admin", map[string]string{"X-Token": "admin-token"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin X-Token, got %d", w.Code)
	}
}

func TestAdminOnly_RoleAdmin_ViaJWT(t *testing.T) {
	secret := "admin-jwt-secret"
	tokenStr, _, _ := GenerateJWT(JWTConfig{Secret: secret, ExpireHours: 1}, "admin-user", "admin")
	r := adminOnlyRouter("x-token", secret)
	w := doRequest(r, "GET", "/admin", map[string]string{"Authorization": "Bearer " + tokenStr})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for JWT admin, got %d", w.Code)
	}
}

func TestAdminOnly_RoleNormal(t *testing.T) {
	secret := "normal-jwt-secret"
	tokenStr, _, _ := GenerateJWT(JWTConfig{Secret: secret, ExpireHours: 1}, "normal-user", "normal")
	r := adminOnlyRouter("x-token", secret)
	w := doRequest(r, "GET", "/admin", map[string]string{"Authorization": "Bearer " + tokenStr})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for normal user, got %d", w.Code)
	}
}

func TestAdminOnly_NoRole(t *testing.T) {
	// A JWT with an empty role (not "admin") must be rejected by AdminOnly with 403.
	secret := "no-role-secret"
	tokenStr, _, _ := GenerateJWT(JWTConfig{Secret: secret, ExpireHours: 1}, "user", "")
	r := adminOnlyRouter("x-token", secret)
	w := doRequest(r, "GET", "/admin", map[string]string{"Authorization": "Bearer " + tokenStr})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for JWT with no role, got %d", w.Code)
	}
}

// TestAuthMiddleware_EmptyJWTSecret_BearerRejected verifies that when jwtSecret is
// empty, a Bearer JWT (even syntactically valid) is rejected with 401.
func TestAuthMiddleware_EmptyJWTSecret_BearerRejected(t *testing.T) {
	tokenStr, _, err := GenerateJWT(JWTConfig{Secret: "some-secret", ExpireHours: 1}, "alice", "normal")
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}
	// Router configured with empty jwtSecret — Bearer JWT must be rejected.
	r := newAuthRouter("static-token", "")
	w := doRequest(r, "GET", "/test", map[string]string{"Authorization": "Bearer " + tokenStr})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with empty jwtSecret, got %d", w.Code)
	}
}

// TestAuthMiddleware_ExpiredJWT verifies that an expired JWT is rejected with 401.
func TestAuthMiddleware_ExpiredJWT(t *testing.T) {
	secret := "exp-test-secret-1234"
	claims := JWTClaims{
		Username: "alice",
		Role:     "normal",
		JTI:      "test-expired-jti",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}

	r := newAuthRouter("x", secret)
	w := doRequest(r, "GET", "/test", map[string]string{"Authorization": "Bearer " + tokenStr})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired JWT, got %d", w.Code)
	}
}
