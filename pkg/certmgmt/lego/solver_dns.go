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
//
// This function also sets LEGO_DISABLE_CNAME_SUPPORT=true to prevent lego from
// following CNAME records during DNS-01 challenge, which can cause unintended
// deletion of non-TXT records (see https://github.com/go-acme/lego/issues/XXXX).
func WithDNSCredentials(creds map[string]string, fn func() error) error {
	dnsGlobalMu.Lock()
	defer dnsGlobalMu.Unlock()

	// Save original values so we can restore them.
	// +1 for LEGO_DISABLE_CNAME_SUPPORT
	originals := make(map[string]string, len(creds)+1)
	for k := range creds {
		originals[k] = os.Getenv(k)
	}
	originals["LEGO_DISABLE_CNAME_SUPPORT"] = os.Getenv("LEGO_DISABLE_CNAME_SUPPORT")

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

	// Disable CNAME following to prevent DNS record deletion bugs.
	_ = os.Setenv("LEGO_DISABLE_CNAME_SUPPORT", "true")

	// Execute the caller's function.
	fnErr := fn()

	// Restore original values regardless of fn's outcome.
	for k, v := range originals {
		_ = os.Setenv(k, v)
	}

	return fnErr
}


