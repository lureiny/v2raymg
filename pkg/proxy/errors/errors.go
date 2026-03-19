// Package errors defines unified error types for the proxy refactor.
// Upper layers use these errors exclusively; provider/container-specific
// error messages are wrapped inside them.
package errors

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorCode represents a stable error identifier.
// Format: PRX-MOD-XXX (e.g., PRX-DOM-001)
type ErrorCode string

// Module prefixes for error codes.
const (
	// Domain layer errors
	ModuleDomain ErrorCode = "PRX-DOM"
	// Container layer errors
	ModuleContainer ErrorCode = "PRX-CNT"
	// Forward layer errors
	ModuleForward ErrorCode = "PRX-FWD"
	// UserManager layer errors
	ModuleUser ErrorCode = "PRX-USR"
	// Xray specific errors
	ModuleXray ErrorCode = "PRX-XRY"
	// System errors
	ModuleSys ErrorCode = "PRX-SYS"
)

// Domain layer error codes (PRX-DOM-xxx) — use as ErrorCode(...)
const (
	// ErrInvalidInboundSpec indicates that an InboundSpec failed validation.
	ErrInvalidInboundSpec ErrorCode = ErrorCode(string(ModuleDomain) + "-001")
	// ErrFeatureNotSupported indicates that the requested feature/combination
	// is not supported by the target provider.
	ErrFeatureNotSupported ErrorCode = ErrorCode(string(ModuleDomain) + "-002")
	// ErrInboundNotFound indicates that the requested inbound tag was not found.
	ErrInboundNotFound ErrorCode = ErrorCode(string(ModuleDomain) + "-003")
	// ErrInboundAlreadyExists indicates that an inbound with the same tag already exists.
	ErrInboundAlreadyExists ErrorCode = ErrorCode(string(ModuleDomain) + "-004")
	// ErrDenyListViolation indicates a domain model violates provider deny list.
	ErrDenyListViolation ErrorCode = ErrorCode(string(ModuleDomain) + "-005")
	// ErrTargetRefInvalid indicates target reference parsing/validation failed.
	ErrTargetRefInvalid ErrorCode = ErrorCode(string(ModuleDomain) + "-006")
	// ErrUnmappedFields indicates domain-to-provider mapping left unmapped fields.
	ErrUnmappedFields ErrorCode = ErrorCode(string(ModuleDomain) + "-007")
	// ErrProtocolInvalid indicates protocol is invalid or unsupported.
	ErrProtocolInvalid ErrorCode = ErrorCode(string(ModuleDomain) + "-008")
)

// Container layer error codes (PRX-CNT-xxx)
const (
	ErrContainerNotRunning     ErrorCode = ErrorCode(string(ModuleContainer) + "-001")
	ErrContainerAlreadyRunning ErrorCode = ErrorCode(string(ModuleContainer) + "-002")
	ErrContainerStartFailed    ErrorCode = ErrorCode(string(ModuleContainer) + "-003")
	ErrContainerStopFailed     ErrorCode = ErrorCode(string(ModuleContainer) + "-004")
	ErrContainerRestartFailed  ErrorCode = ErrorCode(string(ModuleContainer) + "-005")
	ErrContainerReloadFailed   ErrorCode = ErrorCode(string(ModuleContainer) + "-006")
	// ErrFastAddInboundFailed indicates FastAddInbound operation failed.
	ErrFastAddInboundFailed ErrorCode = ErrorCode(string(ModuleContainer) + "-007")
	// ErrFastAddInboundProtocolRequired indicates protocol is required for FastAddInbound.
	ErrFastAddInboundProtocolRequired ErrorCode = ErrorCode(string(ModuleContainer) + "-008")
	// ErrFastAddInboundMissingParams indicates missing required parameters for FastAddInbound.
	ErrFastAddInboundMissingParams ErrorCode = ErrorCode(string(ModuleContainer) + "-009")
	// ErrCertNotFound indicates that no certificate was found for the given domain.
	ErrCertNotFound ErrorCode = ErrorCode(string(ModuleContainer) + "-010")
	// ErrCertRequired indicates that a TLS certificate is required but was not provided.
	ErrCertRequired ErrorCode = ErrorCode(string(ModuleContainer) + "-011")
)

// Forward layer error codes (PRX-FWD-xxx)
const (
	ErrPortInUse           ErrorCode = ErrorCode(string(ModuleForward) + "-001")
	ErrPortAllocationFail  ErrorCode = ErrorCode(string(ModuleForward) + "-002")
	ErrForwardRuleNotFound ErrorCode = ErrorCode(string(ModuleForward) + "-003")
	ErrForwardRuleConflict ErrorCode = ErrorCode(string(ModuleForward) + "-004")
	ErrRateLimitInvalid    ErrorCode = ErrorCode(string(ModuleForward) + "-005")
)

// UserManager layer error codes (PRX-USR-xxx)
const (
	ErrUserNotFound            ErrorCode = ErrorCode(string(ModuleUser) + "-001")
	ErrUserAlreadyExists      ErrorCode = ErrorCode(string(ModuleUser) + "-002")
	ErrUserExpired            ErrorCode = ErrorCode(string(ModuleUser) + "-003")
	ErrInvalidUserSpec        ErrorCode = ErrorCode(string(ModuleUser) + "-004")
	ErrUserPortBindFail       ErrorCode = ErrorCode(string(ModuleUser) + "-005")
	ErrUserForwardPortEmpty   ErrorCode = ErrorCode(string(ModuleUser) + "-006")
	ErrUserManagerRequired    ErrorCode = ErrorCode(string(ModuleUser) + "-007")
	ErrUserDeletionInProgress ErrorCode = ErrorCode(string(ModuleUser) + "-008")
)

// Xray specific error codes (PRX-XRY-xxx)
const (
	ErrNativeRenderFailed ErrorCode = ErrorCode(string(ModuleXray) + "-001")
	ErrGRPCConnection     ErrorCode = ErrorCode(string(ModuleXray) + "-002")
	ErrGRPCRequestFailed  ErrorCode = ErrorCode(string(ModuleXray) + "-003")
	ErrXrayConfigInvalid  ErrorCode = ErrorCode(string(ModuleXray) + "-004")
)

// System error codes (PRX-SYS-xxx)
const (
	ErrInternal     ErrorCode = ErrorCode(string(ModuleSys) + "-001")
	ErrConfigWrite  ErrorCode = ErrorCode(string(ModuleSys) + "-002")
	ErrFileNotFound ErrorCode = ErrorCode(string(ModuleSys) + "-003")
	ErrPermission   ErrorCode = ErrorCode(string(ModuleSys) + "-004")
)

// ProxyError wraps an error with code, message, and optional cause.
// It implements the error interface and supports errors.Is/As.
type ProxyError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *ProxyError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause for errors.Is/As support.
func (e *ProxyError) Unwrap() error {
	return e.Cause
}

// Is compares this error with target for errors.Is support.
func (e *ProxyError) Is(target error) bool {
	// Compare code if target is a ProxyError
	if te, ok := target.(*ProxyError); ok {
		return e.Code == te.Code
	}
	// For backward compatibility, also compare string representation with sentinels
	if se, ok := any(target).(ErrorCode); ok {
		return strings.HasPrefix(string(e.Code), string(se))
	}
	return false
}

// As finds the first error matching target type for errors.As support.
func (e *ProxyError) As(target interface{}) bool {
	if te, ok := target.(**ProxyError); ok {
		if *te == nil {
			*te = e
			return true
		}
		// Allow matching by code
		if (*te).Code == "" || (*te).Code == e.Code {
			*te = e
			return true
		}
	}
	return false
}

// New creates a new ProxyError with code and message.
func New(code ErrorCode, msg string) error {
	return &ProxyError{
		Code:    code,
		Message: msg,
	}
}

// Newf creates a new ProxyError with formatted message.
func Newf(code ErrorCode, format string, args ...interface{}) error {
	return &ProxyError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// Wrap wraps an error with additional context and code.
// The original error becomes the cause.
func Wrap(code ErrorCode, msg string, cause error) error {
	return &ProxyError{
		Code:    code,
		Message: msg,
		Cause:   cause,
	}
}

// Wrapf wraps an error with formatted message and code.
func Wrapf(code ErrorCode, cause error, format string, args ...interface{}) error {
	return &ProxyError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Cause:   cause,
	}
}

// Code extracts the error code from an error.
// Returns empty string if not a ProxyError.
func Code(err error) ErrorCode {
	if err == nil {
		return ""
	}
	// Try unwrapping to find ProxyError
	var pe *ProxyError
	if errors.As(err, &pe) {
		return pe.Code
	}
	// Try to extract from wrapped error
	if unwrap := errors.Unwrap(err); unwrap != nil {
		return Code(unwrap)
	}
	return ""
}

// HasCode checks if the error or any of its causes has the specified code.
func HasCode(err error, code ErrorCode) bool {
	return Code(err) == code
}

// ToError converts an ErrorCode to a sentinel error for backward compatibility.
// The returned error can be used with errors.Is().
func ToError(code ErrorCode) error {
	return errors.New(code.Message())
}

// Message returns the human-readable message for an ErrorCode.
func (c ErrorCode) Message() string {
	switch c {
	case ErrInvalidInboundSpec:
		return "invalid inbound spec"
	case ErrFeatureNotSupported:
		return "feature not supported"
	case ErrNativeRenderFailed:
		return "native render failed"
	case ErrContainerNotRunning:
		return "container not running"
	case ErrContainerAlreadyRunning:
		return "container already running"
	case ErrInboundNotFound:
		return "inbound not found"
	case ErrInboundAlreadyExists:
		return "inbound already exists"
	case ErrPortInUse:
		return "port already in use"
	case ErrUserNotFound:
		return "user not found"
	case ErrUserAlreadyExists:
		return "user already exists"
	case ErrUserExpired:
		return "user expired"
	case ErrInvalidUserSpec:
		return "invalid user spec"
	case ErrUserForwardPortEmpty:
		return "user forward port not configured"
	case ErrUserManagerRequired:
		return "usermanager is required for this operation"
	case ErrUserDeletionInProgress:
		return "user deletion in progress"
	case ErrInternal:
		return "internal error"
	case ErrGRPCConnection:
		return "grpc connection failed"
	case ErrConfigWrite:
		return "config write failed"
	case ErrDenyListViolation:
		return "deny list violation"
	case ErrTargetRefInvalid:
		return "invalid target reference"
	case ErrUnmappedFields:
		return "unmapped fields"
	case ErrProtocolInvalid:
		return "invalid protocol"
	case ErrPortAllocationFail:
		return "port allocation failed"
	case ErrForwardRuleNotFound:
		return "forward rule not found"
	case ErrRateLimitInvalid:
		return "invalid rate limit"
	case ErrXrayConfigInvalid:
		return "invalid xray config"
	case ErrFastAddInboundFailed:
		return "fast add inbound failed"
	case ErrFastAddInboundProtocolRequired:
		return "protocol is required for fast add inbound"
	case ErrFastAddInboundMissingParams:
		return "missing required parameters for fast add inbound"
	case ErrCertNotFound:
		return "certificate not found for domain"
	case ErrCertRequired:
		return "TLS certificate is required; provide certificateFile/keyFile, domain, or set self_signed=true"
	default:
		return string(c)
	}
}

// Is checks if the error matches the given ErrorCode.
// Implements errors.Is() interface for backward compatibility.
func (c ErrorCode) Is(err error) bool {
	// Compare using Code extraction
	if code := Code(err); code == c {
		return true
	}
	// Also match by string comparison for wrapped errors
	return strings.Contains(err.Error(), c.Message())
}

// Is is a convenience re-export of errors.Is.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As is a convenience re-export of errors.As.
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}

// NewErr creates a new error from a sentinel with formatted message.
// Usage: NewErr(ErrInvalidInboundSpec, "port %d out of range", port)
func NewErr(sentinel error, format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%w: %s", sentinel, msg)
}

// NewErrAt creates a new error from a sentinel with formatted message and original error.
// Usage: NewErrAt(ErrInvalidInboundSpec, err, "parsing failed")
func NewErrAt(sentinel, orig error, format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	return fmt.Errorf("%w: %s: %w", sentinel, msg, orig)
}
