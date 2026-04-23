// Package tools provides checksum utilities.
package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// VerifyChecksum verifies a file's SHA256 checksum by exact (case-insensitive)
// match against expectedChecksum after TrimSpace. Historically this used
// strings.Contains as a loose fallback, which made short/garbage expected
// strings (e.g. "beef") silently accept any file whose real digest happened to
// contain the substring — unacceptable for a security primitive. Strict
// equality is the only correct SHA256 semantics.
func VerifyChecksum(filePath, expectedChecksum string) error {
	checksum, err := ComputeChecksum(filePath)
	if err != nil {
		return err
	}

	expected := strings.ToLower(strings.TrimSpace(expectedChecksum))
	got := strings.ToLower(checksum)
	if got != expected {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, expected)
	}
	return nil
}

// ComputeChecksum computes SHA256 checksum of a file.
func ComputeChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
