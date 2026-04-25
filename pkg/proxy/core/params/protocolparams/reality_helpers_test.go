package protocolparams

// Shared test fixtures + focused unit tests for the xray-parity Reality
// helpers (plan T-M-P0-03 follow-up). The fixtures are re-used by
// params_vless_test.go / transport_security_test.go to exercise the
// "caller supplied a valid key pair" path without re-deriving per test.

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Package-wide Reality test keypair. Generated once at test binary start
// so every test sees a deterministic pair for the lifetime of the process,
// but never reuses across builds (rules out a leaked key being pasted
// into production by mistake).
var (
	testRealityPriv string
	testRealityPub  string
)

// Canonical valid 16-hex shortId fixtures. Pick specific values rather
// than random so failing assertions name an actual string the reader can
// diff.
const (
	testRealityShortID1 = "aabbccddeeff0011"
	testRealityShortID2 = "1122334455667788"
)

func init() {
	priv, pub, err := GenerateRealityKeyPair()
	if err != nil {
		panic("reality test init: " + err.Error())
	}
	testRealityPriv = priv
	testRealityPub = pub
}

func TestGenerateRealityShortID_Shape(t *testing.T) {
	sid, err := GenerateRealityShortID()
	if err != nil {
		t.Fatalf("GenerateRealityShortID: %v", err)
	}
	if err := ValidateRealityShortID(sid); err != nil {
		t.Errorf("generated shortId failed validation: %v (%q)", err, sid)
	}
	if len(sid) != realityShortIDHexLen {
		t.Errorf("length = %d, want %d", len(sid), realityShortIDHexLen)
	}
}

func TestGenerateRealityShortID_Unique(t *testing.T) {
	// 1 in 2^64 collision probability; the test is stable.
	seen := make(map[string]struct{}, 16)
	for i := 0; i < 16; i++ {
		sid, err := GenerateRealityShortID()
		if err != nil {
			t.Fatalf("GenerateRealityShortID: %v", err)
		}
		if _, dup := seen[sid]; dup {
			t.Fatalf("duplicate shortId %q in 16 draws", sid)
		}
		seen[sid] = struct{}{}
	}
}

func TestGenerateRealityKeyPair_Shape(t *testing.T) {
	priv, pub, err := GenerateRealityKeyPair()
	if err != nil {
		t.Fatalf("GenerateRealityKeyPair: %v", err)
	}
	if err := ValidateRealityBase64Key(priv); err != nil {
		t.Errorf("generated private key fails validation: %v (%q)", err, priv)
	}
	if err := ValidateRealityBase64Key(pub); err != nil {
		t.Errorf("generated public key fails validation: %v (%q)", err, pub)
	}

	// Private key must have X25519 clamping applied (see RFC 7748 §5).
	// Decode and check bit patterns.
	decoded, err := base64.RawURLEncoding.DecodeString(priv)
	if err != nil {
		t.Fatalf("decode priv: %v", err)
	}
	if len(decoded) != realityKeyByteLen {
		t.Fatalf("priv len = %d, want %d", len(decoded), realityKeyByteLen)
	}
	if decoded[0]&7 != 0 {
		t.Errorf("low 3 bits of byte 0 not cleared (clamping): %#x", decoded[0])
	}
	if decoded[31]&0x80 != 0 {
		t.Errorf("top bit of byte 31 not cleared (clamping): %#x", decoded[31])
	}
	if decoded[31]&0x40 == 0 {
		t.Errorf("bit 6 of byte 31 not set (clamping): %#x", decoded[31])
	}
}

func TestValidateRealityShortID(t *testing.T) {
	good := []string{
		"0123456789abcdef",
		"ABCDEF0123456789",
		"fedcba9876543210",
	}
	for _, s := range good {
		if err := ValidateRealityShortID(s); err != nil {
			t.Errorf("ValidateRealityShortID(%q) err = %v", s, err)
		}
	}

	bad := []string{
		"",                  // empty
		"aa",                // too short
		"aabbccddeeff00110", // 17 chars
		"xxbbccddeeff0011",  // non-hex
		"0123456789abcdeG",  // non-hex G
	}
	for _, s := range bad {
		if err := ValidateRealityShortID(s); err == nil {
			t.Errorf("ValidateRealityShortID(%q) accepted invalid", s)
		}
	}
}

func TestValidateRealityBase64Key(t *testing.T) {
	// Empty is allowed.
	if err := ValidateRealityBase64Key(""); err != nil {
		t.Errorf("empty key rejected: %v", err)
	}

	// Real pair from the init()-generated fixture.
	if err := ValidateRealityBase64Key(testRealityPriv); err != nil {
		t.Errorf("valid priv rejected: %v", err)
	}
	if err := ValidateRealityBase64Key(testRealityPub); err != nil {
		t.Errorf("valid pub rejected: %v", err)
	}

	// Gibberish / wrong length.
	bad := []string{
		"not$base64!!",          // invalid characters in any base64 alphabet
		"short",                 // too short to decode to 32 bytes
		strings.Repeat("!", 43), // 43 chars, none are valid base64
	}
	for _, s := range bad {
		if err := ValidateRealityBase64Key(s); err == nil {
			t.Errorf("ValidateRealityBase64Key(%q) accepted invalid", s)
		}
	}
}
