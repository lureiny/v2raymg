package certmgmtlego_test

import (
	"errors"
	"os"
	"testing"

	certmgmtlego "github.com/lureiny/v2raymg/pkg/certmgmt/lego"
)

func TestWithDNSCredentials_SetsAndRestoresEnv(t *testing.T) {
	const key = "TEST_DNS_CRED_KEY_UNIQUE_12345"
	const origVal = "original"
	const newVal = "injected"

	// Establish a known original value.
	if err := os.Setenv(key, origVal); err != nil {
		t.Fatalf("Setenv original: %v", err)
	}
	defer os.Unsetenv(key)

	creds := map[string]string{key: newVal}

	var sawInside string
	err := certmgmtlego.WithDNSCredentials(creds, func() error {
		sawInside = os.Getenv(key)
		return nil
	})
	if err != nil {
		t.Fatalf("WithDNSCredentials: %v", err)
	}

	if sawInside != newVal {
		t.Errorf("inside fn: got %q, want %q", sawInside, newVal)
	}

	restored := os.Getenv(key)
	if restored != origVal {
		t.Errorf("after fn: got %q, want %q (original)", restored, origVal)
	}
}

func TestWithDNSCredentials_RestoresOnError(t *testing.T) {
	const key = "TEST_DNS_CRED_ERROR_KEY_UNIQUE_99"
	const origVal = "original_err"

	if err := os.Setenv(key, origVal); err != nil {
		t.Fatalf("Setenv original: %v", err)
	}
	defer os.Unsetenv(key)

	creds := map[string]string{key: "new_val"}
	sentinel := errors.New("fn error")

	err := certmgmtlego.WithDNSCredentials(creds, func() error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got: %v", err)
	}

	// Env must be restored even after error.
	restored := os.Getenv(key)
	if restored != origVal {
		t.Errorf("after error fn: got %q, want %q (original)", restored, origVal)
	}
}

func TestWithDNSCredentials_SetsUnsetKey(t *testing.T) {
	const key = "TEST_DNS_CRED_UNSET_KEY_77777"
	// Ensure the key is not set.
	os.Unsetenv(key)
	defer os.Unsetenv(key)

	creds := map[string]string{key: "value"}
	var sawInside string
	_ = certmgmtlego.WithDNSCredentials(creds, func() error {
		sawInside = os.Getenv(key)
		return nil
	})

	if sawInside != "value" {
		t.Errorf("inside fn: got %q, want %q", sawInside, "value")
	}

	// After the fn the key should be unset again.
	if v := os.Getenv(key); v != "" {
		t.Errorf("after fn: key should be unset, got %q", v)
	}
}
