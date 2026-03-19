package errors

import (
	"strings"
	"testing"
)

func TestSentinelErrors_Are_Distinct(t *testing.T) {
	sentinels := []ErrorCode{
		ErrInvalidInboundSpec,
		ErrFeatureNotSupported,
		ErrNativeRenderFailed,
		ErrContainerNotRunning,
		ErrContainerAlreadyRunning,
		ErrInboundNotFound,
		ErrInboundAlreadyExists,
		ErrPortInUse,
		ErrUserNotFound,
		ErrUserAlreadyExists,
		ErrGRPCConnection,
		ErrConfigWrite,
	}

	// Each sentinel should match itself via ToError
	for _, s := range sentinels {
		err := ToError(s)
		if !Is(err, err) {
			t.Errorf("Is(%v, %v) should be true", err, err)
		}
	}

	// Distinct sentinels should not match each other
	for i := 0; i < len(sentinels); i++ {
		for j := i + 1; j < len(sentinels); j++ {
			errI := ToError(sentinels[i])
			errJ := ToError(sentinels[j])
			if Is(errI, errJ) {
				t.Errorf("Is(%v, %v) should be false", errI, errJ)
			}
		}
	}
}

func TestNew(t *testing.T) {
	err := New(ErrInvalidInboundSpec, "port out of range")

	if !HasCode(err, ErrInvalidInboundSpec) {
		t.Error("error should have correct code")
	}

	// Error message format: [PRX-DOM-001] port out of range
	if !strings.Contains(err.Error(), "PRX-DOM-001") {
		t.Errorf("error message should contain code, got: %v", err)
	}
	if !strings.Contains(err.Error(), "port out of range") {
		t.Errorf("error message should contain context, got: %v", err)
	}
}

func TestNewf(t *testing.T) {
	err := Newf(ErrPortInUse, "port %d", 443)

	if !HasCode(err, ErrPortInUse) {
		t.Error("error should have correct code")
	}

	if !strings.Contains(err.Error(), "443") {
		t.Errorf("error message should contain formatted args, got: %v", err)
	}
}

func TestWrap(t *testing.T) {
	orig := &ProxyError{Code: ErrInternal, Message: "original error"}
	err := Wrap(ErrInvalidInboundSpec, "port out of range", orig)

	if !HasCode(err, ErrInvalidInboundSpec) {
		t.Error("error should have correct code")
	}

	// Error message format: [PRX-DOM-001] port out of range: [PRX-SYS-001] original error
	if !strings.Contains(err.Error(), "PRX-DOM-001") {
		t.Errorf("error message should contain code, got: %v", err)
	}
	if !strings.Contains(err.Error(), "port out of range") {
		t.Errorf("error message should contain context, got: %v", err)
	}
}

func TestIs_NilError(t *testing.T) {
	if Is(nil, ToError(ErrInvalidInboundSpec)) {
		t.Error("Is(nil, err) should be false")
	}
}

func TestCode(t *testing.T) {
	// Test extracting code from ProxyError
	err := New(ErrInvalidInboundSpec, "test error")
	if Code(err) != ErrInvalidInboundSpec {
		t.Errorf("expected code %v, got %v", ErrInvalidInboundSpec, Code(err))
	}

	// Test extracting code from wrapped error
	wrapped := Wrap(ErrInboundNotFound, "wrapped", err)
	if Code(wrapped) != ErrInboundNotFound {
		t.Errorf("expected code %v, got %v", ErrInboundNotFound, Code(wrapped))
	}

	// Test nil returns empty
	if Code(nil) != "" {
		t.Error("Code(nil) should return empty string")
	}
}

func TestHasCode(t *testing.T) {
	err := New(ErrInvalidInboundSpec, "test")

	if !HasCode(err, ErrInvalidInboundSpec) {
		t.Error("HasCode should return true for matching code")
	}

	if HasCode(err, ErrUserNotFound) {
		t.Error("HasCode should return false for non-matching code")
	}
}

func TestToError(t *testing.T) {
	err := ToError(ErrInvalidInboundSpec)

	if !Is(err, err) {
		t.Error("ToError should create error that matches via Is")
	}

	if err.Error() != "invalid inbound spec" {
		t.Errorf("expected 'invalid inbound spec', got '%s'", err.Error())
	}
}

func TestErrorCodeIs(t *testing.T) {
	// Test ErrorCode.Is with ProxyError
	err := New(ErrInvalidInboundSpec, "test")
	if !ErrInvalidInboundSpec.Is(err) {
		t.Error("ErrorCode.Is should match ProxyError")
	}

	// Test with different error
	differentErr := New(ErrUserNotFound, "different")
	if ErrInvalidInboundSpec.Is(differentErr) {
		t.Error("ErrorCode.Is should not match different error")
	}
}
