package certmgmtlego

import (
	"fmt"
	"os"
	"sync"
)

// dnsGlobalMu serializes all DNS challenge operations in this process.
// DNS providers read credentials from environment variables; concurrent mutations
// of the same env vars from multiple goroutines would produce a data race.
var dnsGlobalMu sync.Mutex

// WithDNSCredentials acquires the global DNS lock, temporarily sets the provided
// environment variables, calls fn, and then restores the original values.
// All DNS provider construction and certificate operations that depend on the
// env-var credentials must be performed inside fn.
func WithDNSCredentials(creds map[string]string, fn func() error) error {
	dnsGlobalMu.Lock()
	defer dnsGlobalMu.Unlock()

	// Save original values so we can restore them.
	originals := make(map[string]string, len(creds))
	for k := range creds {
		originals[k] = os.Getenv(k)
	}

	// Set credentials.
	for k, v := range creds {
		if err := os.Setenv(k, v); err != nil {
			// Restore any already-set variables before returning.
			for rk, rv := range originals {
				_ = os.Setenv(rk, rv)
			}
			return fmt.Errorf("setenv %q: %w", k, err)
		}
	}

	// Execute the caller's function.
	fnErr := fn()

	// Restore original values regardless of fn's outcome.
	for k, v := range originals {
		_ = os.Setenv(k, v)
	}

	return fnErr
}


