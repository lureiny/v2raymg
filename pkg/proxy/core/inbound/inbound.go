// Package inbound defines the generic inbound abstraction layer.
// It provides protocol-agnostic interfaces and configurations for proxy inbounds.
package inbound

import (
	"fmt"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// Config represents a minimal generic inbound configuration.
// Only contains fields that are truly implementation-agnostic.
type Config struct {
	// Tag is the unique identifier for this inbound.
	Tag string `json:"tag"`

	// ListenAddr is the listen address (default "0.0.0.0").
	ListenAddr string `json:"listen_addr"`

	// Port is the listen port (100-65535).
	Port uint32 `json:"port"`

	// Protocol is the proxy protocol.
	Protocol contracts.Protocol `json:"protocol"`

	// Extensions holds provider-specific configuration.
	// Keys are provider names (e.g., "xray", "sing-box"), values are provider-specific configs.
	// This allows implementations to store their specific fields without coupling to this layer.
	Extensions map[string]any `json:"extensions,omitempty"`
}

// NewConfig creates a new generic inbound Config with default values.
func NewConfig(tag string, protocol contracts.Protocol, port uint32) *Config {
	return &Config{
		Tag:        tag,
		Protocol:   protocol,
		Port:       port,
		ListenAddr: "0.0.0.0",
		Extensions: make(map[string]any),
	}
}

// Validate performs basic validation on the Config.
func (c *Config) Validate() error {
	if c.Tag == "" {
		return ErrInboundTagRequired
	}
	if c.Port < 100 || c.Port > 65535 {
		return ErrInboundPortOutOfRange
	}
	return nil
}

// Inbound represents a generic proxy inbound.
// This is a minimal interface that should be implemented by different proxy containers.
type Inbound interface {
	// Tag returns the unique tag for this inbound.
	Tag() string

	// Protocol returns the proxy protocol.
	Protocol() contracts.Protocol

	// Port returns the listen port.
	Port() uint32

	// ListenAddr returns the listen address.
	ListenAddr() string

	// Config returns the generic inbound configuration.
	Config() *Config

	// Extra returns implementation-specific extra configuration.
	// This is where implementations can store their specific fields.
	Extra() map[string]any

	// ToNative converts the generic Inbound to container-specific native config.
	// Returns the native configuration bytes (e.g., JSON for xray).
	ToNative() ([]byte, error)

	// Validate validates the inbound configuration.
	Validate() error
}

// InboundValidator validates inbound configurations.
type InboundValidator interface {
	ValidateInbound(*Config) error
}

// ErrInboundTagRequired is returned when inbound tag is empty.
var ErrInboundTagRequired = &InboundError{Code: "INBOUND_TAG_REQUIRED", Message: "inbound tag is required"}

// ErrInboundPortOutOfRange is returned when port is out of valid range.
var ErrInboundPortOutOfRange = &InboundError{Code: "INBOUND_PORT_OUT_OF_RANGE", Message: "port must be between 100 and 65535"}

// ErrInvalidConfig is returned when configuration is invalid.
var ErrInvalidConfig = &InboundError{Code: "INBOUND_INVALID_CONFIG", Message: "invalid inbound configuration"}

// InboundError represents an inbound-related error.
type InboundError struct {
	Code    string
	Message string
	Cause   error
}

func (e *InboundError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (cause: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *InboundError) Unwrap() error {
	return e.Cause
}

// WithCause returns a new InboundError with the given cause.
func (e *InboundError) WithCause(err error) *InboundError {
	return &InboundError{
		Code:    e.Code,
		Message: e.Message,
		Cause:   err,
	}
}
