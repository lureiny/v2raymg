// Package inbound defines the generic inbound abstraction layer.
package inbound

import (
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// DefaultInbound is a basic implementation of Inbound.
// It provides common functionality that can be embedded or extended by concrete implementations.
// Note: Security and Transport fields are kept internally for implementations that need them,
// but are stored in Extensions rather than in the base Config.
type DefaultInbound struct {
	tag_        string
	protocol_   contracts.Protocol
	port_       uint32
	listenAddr_ string
	extra_      map[string]any
	config_     *Config
}

// NewDefaultInbound creates a new DefaultInbound with default values.
//
// A zero port is NOT substituted. It used to become 10000, which meant every
// caller that omitted a port silently landed on the same listener address —
// the first one bound it and every later one failed, and because the proxy
// cores report a bind failure only in their own logs, the failure was
// invisible. Port selection belongs to the shared allocator
// (container.ClaimInboundPort); a zero here now falls through to Validate,
// which rejects it, so the mistake surfaces at the caller instead.
func NewDefaultInbound(tag string, protocol contracts.Protocol, port uint32) *DefaultInbound {
	if tag == "" {
		tag = "default-inbound"
	}
	return &DefaultInbound{
		tag_:        tag,
		protocol_:   protocol,
		port_:       port,
		listenAddr_: "0.0.0.0",
		extra_:      make(map[string]any),
		config_: &Config{
			Tag:        tag,
			ListenAddr: "0.0.0.0",
			Port:       port,
			Protocol:   protocol,
			Extensions: make(map[string]any),
		},
	}
}

// Tag returns the tag.
func (i *DefaultInbound) Tag() string { return i.tag_ }

// Protocol returns the protocol.
func (i *DefaultInbound) Protocol() contracts.Protocol { return i.protocol_ }

// Port returns the port.
func (i *DefaultInbound) Port() uint32 { return i.port_ }

// ListenAddr returns the listen address.
func (i *DefaultInbound) ListenAddr() string { return i.listenAddr_ }

// Config returns the generic config.
func (i *DefaultInbound) Config() *Config { return i.config_ }

// Extra returns the extra config.
func (i *DefaultInbound) Extra() map[string]any { return i.extra_ }

// SetTag sets the tag.
func (i *DefaultInbound) SetTag(tag string) { i.tag_ = tag }

// SetProtocol sets the protocol.
func (i *DefaultInbound) SetProtocol(protocol contracts.Protocol) { i.protocol_ = protocol }

// SetPort sets the port.
func (i *DefaultInbound) SetPort(port uint32) { i.port_ = port }

// SetListenAddr sets the listen address.
func (i *DefaultInbound) SetListenAddr(addr string) {
	if addr == "" {
		addr = "0.0.0.0"
	}
	i.listenAddr_ = addr
}

// SetExtra sets the extra config.
func (i *DefaultInbound) SetExtra(extra map[string]any) { i.extra_ = extra }

// ToNative is not implemented for default inbound.
// Container implementations should override this.
func (i *DefaultInbound) ToNative() ([]byte, error) {
	return nil, ErrInboundToNativeNotImplemented
}

// ErrInboundToNativeNotImplemented is returned when ToNative is not implemented.
var ErrInboundToNativeNotImplemented = &InboundError{
	Code:    "INBOUND_TO_NATIVE_NOT_IMPLEMENTED",
	Message: "ToNative not implemented for default inbound",
}

// Validate performs basic validation.
func (i *DefaultInbound) Validate() error {
	if i.tag_ == "" {
		return ErrInboundTagRequired
	}
	if i.port_ < 100 || i.port_ > 65535 {
		return ErrInboundPortOutOfRange
	}
	return nil
}
