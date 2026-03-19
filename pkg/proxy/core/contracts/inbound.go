// Package contracts defines the core business models for the proxy refactor.
// It contains truly generic, implementation-agnostic types.
package contracts

import "fmt"

// InboundSpec is a minimal generic inbound specification.
// Only contains truly implementation-agnostic fields.
// Provider-specific fields should be stored in Extensions.
type InboundSpec struct {
	// Tag is the inbound tag.
	Tag string `json:"tag"`

	// Port is the listen port.
	Port uint32 `json:"port"`

	// Protocol is the proxy protocol.
	Protocol Protocol `json:"protocol"`

	// Extensions holds provider-specific configuration.
	// Keys are provider names (e.g., "xray", "sing-box"), values are provider-specific configs.
	Extensions map[string]any `json:"extensions,omitempty"`
}

// Validate performs basic validation on InboundSpec.
func (s *InboundSpec) Validate() error {
	if s.Tag == "" {
		return fmt.Errorf("inbound tag must not be empty")
	}
	if s.Port < 100 || s.Port > 65535 {
		return fmt.Errorf("port %d out of range [100, 65535]", s.Port)
	}
	return nil
}
