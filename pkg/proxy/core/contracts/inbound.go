// Package contracts defines the core business models for the proxy refactor.
// It contains truly generic, implementation-agnostic types.
package contracts

import (
	"fmt"
	"sort"
	"strings"
)

// InboundType identifies the outbound routing mode of an inbound.
type InboundType string

const (
	// InboundTypeDirect routes inbound traffic directly to the internet (default).
	InboundTypeDirect InboundType = "direct"
	// InboundTypeChain binds the inbound 1:1 to a dedicated chained outbound proxy.
	InboundTypeChain InboundType = "chain"
)

// InboundMeta holds chain-proxy and visibility metadata for an inbound.
type InboundMeta struct {
	InboundType      InboundType `json:"inbound_type,omitempty"`
	AccessTags       []string    `json:"access_tags,omitempty"`
	ChainOutboundURI string      `json:"chain_outbound_uri,omitempty"`
	Remark           string      `json:"remark,omitempty"`
}

// NormalizeInboundType validates and normalizes a raw inbound type string.
// An empty string defaults to InboundTypeDirect.
func NormalizeInboundType(raw string) (InboundType, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(InboundTypeDirect):
		return InboundTypeDirect, nil
	case string(InboundTypeChain):
		return InboundTypeChain, nil
	default:
		return "", fmt.Errorf("invalid inbound_type %q: must be %q or %q", raw, InboundTypeDirect, InboundTypeChain)
	}
}

func isValidTagChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '.' || b == '_' || b == '-'
}

func validateTagToken(token string) error {
	if len(token) == 0 || len(token) > 64 {
		return fmt.Errorf("tag %q must be 1..64 characters", token)
	}
	for i := 0; i < len(token); i++ {
		if !isValidTagChar(token[i]) {
			return fmt.Errorf("tag %q contains invalid character %q (allowed: a-zA-Z0-9._-)", token, token[i])
		}
	}
	return nil
}

// ValidateInboundTag validates an inbound tag against ^[a-zA-Z0-9._-]{1,64}$
// and rejects the reserved "__" prefix used for internal chain resources.
func ValidateInboundTag(tag string) error {
	if strings.HasPrefix(tag, "__") {
		return fmt.Errorf("inbound tag %q must not start with reserved prefix \"__\"", tag)
	}
	if err := validateTagToken(tag); err != nil {
		return fmt.Errorf("invalid inbound %w", err)
	}
	return nil
}

// NormalizeAccessTags trims whitespace, skips empty strings, validates each tag
// against ^[a-zA-Z0-9._-]{1,64}$, deduplicates, and sorts ascending.
func NormalizeAccessTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		tag := strings.TrimSpace(raw)
		if tag == "" {
			continue
		}
		if err := validateTagToken(tag); err != nil {
			return nil, fmt.Errorf("invalid access %w", err)
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	if len(out) == 0 {
		return nil, nil
	}
	sort.Strings(out)
	return out, nil
}

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
