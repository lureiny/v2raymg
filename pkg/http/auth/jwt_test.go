package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

func TestGenerateJWT_ContainsFields(t *testing.T) {
	cfg := JWTConfig{Secret: "test-secret", ExpireHours: 1}
	tokenStr, expire, err := GenerateJWT(cfg, "alice", "normal")
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("expected non-empty token string")
	}
	if expire <= 0 {
		t.Fatal("expected positive expiry timestamp")
	}

	claims, err := ParseJWT("test-secret", tokenStr)
	if err != nil {
		t.Fatalf("ParseJWT: %v", err)
	}
	if claims.Username != "alice" {
		t.Errorf("expected username=alice, got %q", claims.Username)
	}
	if claims.Role != "normal" {
		t.Errorf("expected role=normal, got %q", claims.Role)
	}
	if claims.JTI == "" {
		t.Fatal("expected non-empty jti")
	}
}

func TestGenerateJWT_UniqueJTI(t *testing.T) {
	cfg := JWTConfig{Secret: "s", ExpireHours: 1}
	t1, _, _ := GenerateJWT(cfg, "alice", "normal")
	t2, _, _ := GenerateJWT(cfg, "alice", "normal")
	c1, _ := ParseJWT("s", t1)
	c2, _ := ParseJWT("s", t2)
	if c1 == nil || c2 == nil {
		t.Fatal("expected valid claims")
	}
	if c1.JTI == c2.JTI {
		t.Fatal("expected unique JTIs for two separate GenerateJWT calls")
	}
}

func TestGenerateJWT_EmptySecret(t *testing.T) {
	cfg := JWTConfig{Secret: "", ExpireHours: 1}
	_, _, err := GenerateJWT(cfg, "alice", "normal")
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}

func TestGenerateJWT_DefaultExpireHours(t *testing.T) {
	cfg := JWTConfig{Secret: "s", ExpireHours: 0} // should default to 24
	_, expire, err := GenerateJWT(cfg, "u", "r")
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}
	in24h := time.Now().Add(24 * time.Hour).Unix()
	diff := expire - in24h
	if diff < -60 || diff > 60 { // within ±1 minute
		t.Fatalf("expected expire ~24h from now, diff=%ds", diff)
	}
}

func TestParseJWT_Valid(t *testing.T) {
	cfg := JWTConfig{Secret: "sec", ExpireHours: 1}
	tokenStr, _, _ := GenerateJWT(cfg, "bob", "admin")
	claims, err := ParseJWT("sec", tokenStr)
	if err != nil {
		t.Fatalf("ParseJWT: %v", err)
	}
	if claims.Username != "bob" || claims.Role != "admin" {
		t.Errorf("unexpected claims: username=%q role=%q", claims.Username, claims.Role)
	}
}

func TestParseJWT_WrongSecret(t *testing.T) {
	tokenStr, _, _ := GenerateJWT(JWTConfig{Secret: "real-secret", ExpireHours: 1}, "u", "r")
	_, err := ParseJWT("wrong-secret", tokenStr)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestParseJWT_Tampered(t *testing.T) {
	tokenStr, _, _ := GenerateJWT(JWTConfig{Secret: "s", ExpireHours: 1}, "u", "r")
	_, err := ParseJWT("s", tokenStr+"x")
	if err == nil {
		t.Fatal("expected error for tampered token")
	}
}

func TestParseJWT_EmptyToken(t *testing.T) {
	_, err := ParseJWT("s", "")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestParseJWT_Expired(t *testing.T) {
	// Build a token with an already-expired timestamp and verify ParseJWT rejects it.
	secret := "test-secret"
	claims := JWTClaims{
		Username: "alice",
		Role:     "normal",
		JTI:      "test-jti",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	_, err = ParseJWT(secret, signed)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}
