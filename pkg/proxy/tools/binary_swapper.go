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

// SwapAtomic atomically swaps old binary with new. It returns the backup path
// only when a backup was actually created; if the target binary did not exist
// (first install), it returns an empty backupPath so a later Rollback knows
// there is nothing to roll back to and must not destroy the freshly-installed
// binary.
func (s *BinarySwapper) SwapAtomic(binaryPath, newBinaryPath string) (backupPath string, err error) {
	candidate := binaryPath + ".bak"

	// Remove old backup if exists
	os.Remove(candidate)

	// Backup current binary, if there is one.
	backedUp := false
	if err := os.Rename(binaryPath, candidate); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("backup failed: %w", err)
		}
		// Target does not exist (first install): nothing to back up.
	} else {
		backedUp = true
	}

	// Rename new binary to target path
	if err := os.Rename(newBinaryPath, binaryPath); err != nil {
		if backedUp {
			os.Rename(candidate, binaryPath) // restore only a real backup
		}
		return "", fmt.Errorf("swap failed: %w", err)
	}

	if backedUp {
		return candidate, nil
	}
	return "", nil // no backup exists to roll back to
}

// Rollback restores the backup binary. With no backup path it refuses to touch
// binaryPath (preserving the just-installed binary rather than stranding it).
func (s *BinarySwapper) Rollback(binaryPath, backupPath string) error {
	if backupPath == "" {
		return fmt.Errorf("no backup available to roll back to")
	}
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup %s missing, refusing to roll back: %w", backupPath, err)
	}

	currentPath := binaryPath + ".new"
	os.Remove(currentPath)
	if err := os.Rename(binaryPath, currentPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stash current failed: %w", err)
	}
	if err := os.Rename(backupPath, binaryPath); err != nil {
		os.Rename(currentPath, binaryPath) // put the current binary back; never leave it empty
		return fmt.Errorf("restore backup failed: %w", err)
	}
	os.Remove(currentPath)
	return nil
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
