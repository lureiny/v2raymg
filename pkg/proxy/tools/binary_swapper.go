// Package tools provides binary swapping utilities.
package tools

import (
	"fmt"
	"os"
)

// BinarySwapper performs atomic binary swaps.
type BinarySwapper struct{}

// NewBinarySwapper creates a new swapper.
func NewBinarySwapper() *BinarySwapper {
	return &BinarySwapper{}
}

// SwapAtomic atomically swaps old binary with new.
func (s *BinarySwapper) SwapAtomic(binaryPath, newBinaryPath string) (backupPath string, err error) {
	backupPath = binaryPath + ".bak"

	// Remove old backup if exists
	os.Remove(backupPath)

	// Backup current binary
	if err := os.Rename(binaryPath, backupPath); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("backup failed: %w", err)
	}

	// Rename new binary to target path
	if err := os.Rename(newBinaryPath, binaryPath); err != nil {
		// Try to restore backup
		os.Rename(backupPath, binaryPath)
		return "", fmt.Errorf("swap failed: %w", err)
	}

	return backupPath, nil
}

// Rollback restores the backup binary.
func (s *BinarySwapper) Rollback(binaryPath, backupPath string) error {
	if backupPath == "" {
		return fmt.Errorf("no backup path")
	}

	currentPath := binaryPath + ".new"
	os.Rename(binaryPath, currentPath)
	return os.Rename(backupPath, binaryPath)
}

// BinarySwapperImpl is the implementation type.
type BinarySwapperImpl struct{}

// SwapAtomic is the implementation method.
func (BinarySwapperImpl) SwapAtomic(binaryPath, newBinaryPath string) (string, error) {
	swapper := &BinarySwapper{}
	return swapper.SwapAtomic(binaryPath, newBinaryPath)
}

// Rollback is the implementation method.
func (BinarySwapperImpl) Rollback(binaryPath, backupPath string) error {
	swapper := &BinarySwapper{}
	return swapper.Rollback(binaryPath, backupPath)
}
