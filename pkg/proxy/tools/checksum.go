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

// VerifyChecksum verifies a file's SHA256 checksum.
func VerifyChecksum(filePath, expectedChecksum string) error {
	checksum, err := ComputeChecksum(filePath)
	if err != nil {
		return err
	}

	expected := strings.TrimSpace(expectedChecksum)
	if !strings.Contains(checksum, expected) && checksum != expected {
		return fmt.Errorf("checksum mismatch: got %s, want %s", checksum, expected)
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
