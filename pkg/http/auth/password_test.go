package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashLoginPassword_FromSHA256Hex(t *testing.T) {
	// When input is already a 64-char lowercase SHA256 hex string, HashLoginPassword
	// must treat it as pre-hashed and produce a bcrypt hash that verifies against it.
	// SHA256("test") = 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
	hexInput := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	hash, err := HashLoginPassword(hexInput)
	if err != nil {
		t.Fatalf("HashLoginPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if !VerifyLoginPassword(hash, hexInput) {
		t.Fatal("expected VerifyLoginPassword to succeed with SHA256 hex input")
	}
	if !VerifyLoginPassword(hash, "test") {
		t.Fatal("expected VerifyLoginPassword to succeed with original plaintext 'test'")
	}
}

func TestHashLoginPassword_FromPlaintext(t *testing.T) {
	hash, err := HashLoginPassword("mysecret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestHashLoginPassword_DifferentCallsSalt(t *testing.T) {
	h1, err1 := HashLoginPassword("mysecret")
	h2, err2 := HashLoginPassword("mysecret")
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}
	if h1 == h2 {
		t.Fatal("two hashes of the same password should differ (different bcrypt salts)")
	}
}

func TestHashLoginPassword_BCryptCost(t *testing.T) {
	hash, err := HashLoginPassword("test")
	if err != nil {
		t.Fatalf("HashLoginPassword: %v", err)
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if cost < bcrypt.DefaultCost {
		t.Fatalf("bcrypt cost %d is below DefaultCost %d", cost, bcrypt.DefaultCost)
	}
}

func TestVerifyLoginPassword_PlaintextMatch(t *testing.T) {
	hash, _ := HashLoginPassword("mypassword")
	if !VerifyLoginPassword(hash, "mypassword") {
		t.Fatal("expected verification to succeed with correct plaintext")
	}
}

func TestVerifyLoginPassword_SHA256HexMatch(t *testing.T) {
	// SHA256("test") = 9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08
	hexInput := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	hash, _ := HashLoginPassword(hexInput)
	if !VerifyLoginPassword(hash, hexInput) {
		t.Fatal("expected verification with SHA256 hex input to succeed")
	}
	// Plaintext whose SHA256 matches the hex should also verify
	if !VerifyLoginPassword(hash, "test") {
		t.Fatal("expected verification with plaintext 'test' to succeed (same SHA256 result)")
	}
}

func TestVerifyLoginPassword_WrongPassword(t *testing.T) {
	hash, _ := HashLoginPassword("correct")
	if VerifyLoginPassword(hash, "wrong") {
		t.Fatal("expected verification to fail with wrong password")
	}
}

func TestVerifyLoginPassword_EmptyStored(t *testing.T) {
	if VerifyLoginPassword("", "anypassword") {
		t.Fatal("expected false when stored hash is empty")
	}
}

func TestIsSHA256Hex_Valid(t *testing.T) {
	if !isSHA256Hex("9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08") {
		t.Fatal("expected valid 64-char lowercase hex to return true")
	}
}

func TestIsSHA256Hex_TooShort(t *testing.T) {
	short := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a0" // 63 chars
	if isSHA256Hex(short) {
		t.Fatal("expected 63-char string to return false")
	}
}

func TestIsSHA256Hex_Uppercase(t *testing.T) {
	upper := "9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08"
	if isSHA256Hex(upper) {
		t.Fatal("expected uppercase hex to return false")
	}
}

func TestIsSHA256Hex_NonHex(t *testing.T) {
	nonhex := "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz" // 64 chars, not hex
	if isSHA256Hex(nonhex) {
		t.Fatal("expected non-hex 64-char string to return false")
	}
}
