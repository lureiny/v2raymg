package protocolparams

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// Reality helper functions shared across containers. The algorithms here
// are byte-for-byte equivalent to xray's implementations in
// pkg/proxy/containers/xray/{profilegen/vless_profiles.go, inbound_adapter.go}
// — see plan docs/cosmic-leaping-moon.md "xray-parity defaults".
//
// Kept in core so mihomo (and future containers) can reuse them without
// dragging a container-specific dependency. xray's own exported wrappers
// continue to live in its package for compatibility; any future cleanup
// can delegate those down to these helpers.

// realityShortIDByteLen is the byte length of a Reality shortId before
// hex encoding. 8 bytes = 16 hex characters — xray's fixed format.
const realityShortIDByteLen = 8

// realityShortIDHexLen is the exact hex length of a valid Reality shortId.
const realityShortIDHexLen = 16

// realityKeyByteLen is the X25519 key byte length (both private and public).
const realityKeyByteLen = 32

// realityKeyB64RawURLLen is the Base64URL-no-padding length of a 32-byte key.
const realityKeyB64RawURLLen = 43

// GenerateRealityShortID produces a fresh Reality shortId: 8 random bytes
// encoded as a 16-character hex string. Mirrors xray's
// generateRandomRealityShortIDHex.
func GenerateRealityShortID() (string, error) {
	b := make([]byte, realityShortIDByteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("reality: random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GenerateRealityKeyPair produces a fresh X25519 key pair suitable for
// Reality, returned as Base64URL-no-padding strings. Mirrors xray's
// profilegen.GenerateRealityKeyPairWithPublic (same algorithm: crypto/rand
// 32 bytes → X25519 clamping → curve25519.ScalarBaseMult → base64url).
func GenerateRealityKeyPair() (privateKey, publicKey string, err error) {
	var priv [realityKeyByteLen]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return "", "", fmt.Errorf("reality: random scalar: %w", err)
	}
	// X25519 clamping — required; an unclamped scalar is still a valid
	// key mathematically but fails interop with Reality clients that
	// assume the RFC 7748 clamping.
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	var pub [realityKeyByteLen]byte
	curve25519.ScalarBaseMult(&pub, &priv)

	return base64.RawURLEncoding.EncodeToString(priv[:]),
		base64.RawURLEncoding.EncodeToString(pub[:]),
		nil
}

// ValidateRealityShortID enforces the 16-hex-char shape Reality expects.
// Empty strings are rejected — callers who want "no shortId at all"
// should omit the entry rather than pass "".
func ValidateRealityShortID(sid string) error {
	if len(sid) != realityShortIDHexLen {
		return fmt.Errorf("reality shortId must be %d hex characters, got %d (%q)",
			realityShortIDHexLen, len(sid), sid)
	}
	for i, c := range sid {
		if !isHexChar(c) {
			return fmt.Errorf("reality shortId: non-hex character %q at position %d (%q)", c, i, sid)
		}
	}
	return nil
}

// ValidateRealityBase64Key checks whether key is a valid Base64 (URL-safe
// or standard) encoding of a 32-byte scalar. Empty strings are considered
// valid (absence is handled separately).
//
// Strict-by-design note: this is slightly stricter than xray's
// validateBase64Key (pkg/proxy/containers/xray/inbound_adapter.go:899).
// xray's variant accepts any base64-decodable string in the standard-
// encoding branch, including non-32-byte payloads; that is a minor
// xray-side quirk. We enforce len(decoded)==32 here so a mistyped key
// fails loudly at parse time instead of silently at reality handshake.
// Memory reference_xray_parity_defaults — when xray gets patched, this
// note and any parity shim can go.
func ValidateRealityBase64Key(key string) error {
	if key == "" {
		return nil
	}
	// Try base64url-no-padding first (xray's canonical form).
	if len(key) == realityKeyB64RawURLLen {
		if _, err := base64.RawURLEncoding.DecodeString(key); err == nil {
			return nil
		}
	}
	// Fall back to standard base64 (with padding). Length must still
	// decode to 32 bytes.
	if dec, err := base64.StdEncoding.DecodeString(key); err == nil && len(dec) == realityKeyByteLen {
		return nil
	}
	return fmt.Errorf("reality key must be base64url or base64 encoded 32-byte scalar")
}

func isHexChar(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
