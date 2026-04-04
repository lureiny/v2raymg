package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
)

// JWTClaims are the custom JWT claims.
type JWTClaims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	JTI      string `json:"jti"`
	jwt.RegisteredClaims
}

// JWTConfig holds JWT signing configuration.
type JWTConfig struct {
	Secret      string
	ExpireHours int
}

// GenerateJWT creates and signs a JWT for the given user.
// Returns the signed token string, its Unix expiry timestamp, and any error.
func GenerateJWT(cfg JWTConfig, username, role string) (string, int64, error) {
	if cfg.Secret == "" {
		return "", 0, fmt.Errorf("jwt: secret must not be empty")
	}
	expireHours := cfg.ExpireHours
	if expireHours <= 0 {
		expireHours = 24
	}
	expire := time.Now().Add(time.Duration(expireHours) * time.Hour)
	claims := JWTClaims{
		Username: username,
		Role:     role,
		JTI:      uuid.New().String(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expire),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(cfg.Secret))
	if err != nil {
		return "", 0, fmt.Errorf("jwt: sign: %w", err)
	}
	return signed, expire.Unix(), nil
}

// ParseJWT parses and validates a JWT string.
// Returns an error if the token is invalid, expired, or uses an unexpected algorithm.
func ParseJWT(secret, tokenStr string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &JWTClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("jwt: unexpected signing method %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("jwt: invalid token")
	}
	return claims, nil
}
