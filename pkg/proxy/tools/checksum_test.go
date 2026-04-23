package tools

import (
	"os"
	"testing"
)

func TestComputeChecksum(t *testing.T) {
	// Create temp file
	tmpFile, err := os.CreateTemp("", "checksum_test")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write exact content (no newline)
	content := []byte("hello world")
	tmpFile.Write(content)
	tmpFile.Close()

	// Compute checksum
	checksum, err := ComputeChecksum(tmpFile.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SHA256 of "hello world"
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if checksum != expected {
		t.Errorf("checksum = %s, want %s", checksum, expected)
	}
}

func TestVerifyChecksum_Valid(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "verify_test")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write exact content
	content := []byte("hello world")
	tmpFile.Write(content)
	tmpFile.Close()

	// Valid SHA256 of "hello world"
	validChecksum := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	err = VerifyChecksum(tmpFile.Name(), validChecksum)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVerifyChecksum_Invalid(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "verify_test")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	content := []byte("hello world")
	tmpFile.Write(content)
	tmpFile.Close()

	// Invalid checksum
	err = VerifyChecksum(tmpFile.Name(), "invalid_checksum")
	if err == nil {
		t.Error("expected error for invalid checksum")
	}
}

// TestVerifyChecksum_RejectsSubstring guards against a prior bug where
// strings.Contains was used as a loose fallback — that would falsely accept
// any expected string that happened to be a substring of the real digest.
func TestVerifyChecksum_RejectsSubstring(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "verify_substring_test")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Write([]byte("hello world"))
	tmpFile.Close()

	// Real SHA256: b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
	// Use the first few chars as the "expected" — old loose match would accept
	// this; strict match must reject.
	err = VerifyChecksum(tmpFile.Name(), "b94d27b9")
	if err == nil {
		t.Error("expected error when expected is a substring of real digest (loose match regression)")
	}
}

// TestVerifyChecksum_CaseInsensitive confirms uppercase / mixed-case hex on
// either side of the compare still succeeds. GNU sha256sum emits lowercase,
// but defensive parsing shouldn't care.
func TestVerifyChecksum_CaseInsensitive(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "verify_case_test")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Write([]byte("hello world"))
	tmpFile.Close()

	err = VerifyChecksum(tmpFile.Name(), "B94D27B9934D3E08A52E52D7DA7DABFAC484EFE37A5380EE9088F7ACE2EFCDE9")
	if err != nil {
		t.Errorf("uppercase expected hex should still match: %v", err)
	}
}
