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
