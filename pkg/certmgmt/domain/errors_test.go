package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/lureiny/v2raymg/pkg/certmgmt/domain"
)

func TestErrors_Is(t *testing.T) {
	errs := []error{
		domain.ErrConfigInvalid,
		domain.ErrChallengeSetup,
		domain.ErrPortBind,
		domain.ErrDNSProviderNotFound,
		domain.ErrDNSProviderAuth,
		domain.ErrDNSPropagationTimeout,
		domain.ErrACMERateLimited,
		domain.ErrACMEAccountNotRegistered,
		domain.ErrStorageIO,
		domain.ErrCertResourceMissing,
	}

	for _, target := range errs {
		// Direct match
		if !errors.Is(target, target) {
			t.Errorf("errors.Is(%v, %v) should be true for identity", target, target)
		}

		// Wrapped match
		wrapped := fmt.Errorf("outer: %w", target)
		if !errors.Is(wrapped, target) {
			t.Errorf("errors.Is(wrapped %v, %v) should be true", wrapped, target)
		}

		// Wrapped twice
		doubleWrapped := fmt.Errorf("outer2: %w", wrapped)
		if !errors.Is(doubleWrapped, target) {
			t.Errorf("errors.Is(double-wrapped %v, %v) should be true", doubleWrapped, target)
		}
	}

	// Cross-error: different sentinel values must not match each other
	if errors.Is(domain.ErrConfigInvalid, domain.ErrStorageIO) {
		t.Error("ErrConfigInvalid should not match ErrStorageIO")
	}
}
