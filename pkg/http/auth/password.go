package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"

	"golang.org/x/crypto/bcrypt"
)

var sha256HexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func isSHA256Hex(s string) bool {
	return sha256HexRe.MatchString(s)
}

func toSHA256Hex(input string) string {
	if isSHA256Hex(input) {
		return input
	}
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}

// HashLoginPassword hashes input for storage as login_password.
// input may be plaintext or a SHA256 hex string (64 lowercase hex chars).
func HashLoginPassword(input string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(toSHA256Hex(input)), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyLoginPassword checks input against a stored bcrypt hash.
// input may be plaintext or a SHA256 hex string.
// Returns false if stored is empty.
func VerifyLoginPassword(stored, input string) bool {
	if stored == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(stored), []byte(toSHA256Hex(input))) == nil
}
