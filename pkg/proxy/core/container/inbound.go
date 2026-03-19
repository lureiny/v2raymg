// Package container defines the Container interface and core types.
package container

import (
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
	"github.com/lureiny/v2raymg/pkg/proxy/core/inbound"
)

// Inbound represents a proxy inbound configuration.
// This is a generic abstraction that can be implemented by different container types.
// Note: This is an alias/bridge to core/inbound.Inbound for backward compatibility.
type Inbound = inbound.Inbound

// InboundAdapter adapts contracts.InboundSpec to container.Inbound.
// This is a helper to convert from business-level spec to container-level inbound.
type InboundAdapter struct{}

// NewInboundAdapter creates a new InboundAdapter.
func NewInboundAdapter() *InboundAdapter {
	return &InboundAdapter{}
}

// ToInbound converts a contracts.InboundSpec to container.Inbound.
// Note: ToNative() will return ErrInboundToNativeNotImplemented as this
// is a generic implementation. Container implementations should provide their own.
func (a *InboundAdapter) ToInbound(spec contracts.InboundSpec) inbound.Inbound {
	impl := &inboundAdapterImpl{
		DefaultInbound: *inbound.NewDefaultInbound(spec.Tag, spec.Protocol, spec.Port),
		spec:           spec,
	}
	// Copy extensions from spec
	if spec.Extensions != nil {
		impl.SetExtra(spec.Extensions)
	}
	return impl
}

type inboundAdapterImpl struct {
	inbound.DefaultInbound
	spec contracts.InboundSpec
}

func (a *inboundAdapterImpl) Tag() string {
	if a.spec.Tag != "" {
		return a.spec.Tag
	}
	return a.DefaultInbound.Tag()
}

func (a *inboundAdapterImpl) Protocol() contracts.Protocol {
	if a.spec.Protocol != "" {
		return a.spec.Protocol
	}
	return a.DefaultInbound.Protocol()
}

func (a *inboundAdapterImpl) Port() uint32 {
	if a.spec.Port != 0 {
		return a.spec.Port
	}
	return a.DefaultInbound.Port()
}

// Config returns the generic inbound config.
// Xray-specific fields are stored in Extensions.
func (a *inboundAdapterImpl) Config() *inbound.Config {
	cfg := a.DefaultInbound.Config()
	// Merge spec extensions
	if a.spec.Extensions != nil {
		for k, v := range a.spec.Extensions {
			cfg.Extensions[k] = v
		}
	}
	return cfg
}

// InboundConfig is a generic inbound configuration that can be adapted
// to different container types.
// Deprecated: Use core/inbound.DefaultInbound instead.
type InboundConfig struct {
	Tag_        string
	Protocol_   contracts.Protocol
	Port_       uint32
	ListenAddr_ string
	Extra_      map[string]interface{}
}

// NewInboundConfig creates a new generic InboundConfig.
// Deprecated: Use core/inbound.NewConfig instead.
func NewInboundConfig(tag string, protocol contracts.Protocol, port uint32, listenAddr string) *InboundConfig {
	if listenAddr == "" {
		listenAddr = "0.0.0.0"
	}
	return &InboundConfig{
		Tag_:        tag,
		Protocol_:   protocol,
		Port_:       port,
		ListenAddr_: listenAddr,
		Extra_:      make(map[string]interface{}),
	}
}

// Tag returns the tag.
// Deprecated: Use core/inbound.DefaultInbound instead.
func (c *InboundConfig) Tag() string { return c.Tag_ }

// Protocol returns the protocol.
// Deprecated: Use core/inbound.DefaultInbound instead.
func (c *InboundConfig) Protocol() contracts.Protocol { return c.Protocol_ }

// Port returns the port.
// Deprecated: Use core/inbound.DefaultInbound instead.
func (c *InboundConfig) Port() uint32 { return c.Port_ }

// ListenAddr returns the listen address.
// Deprecated: Use core/inbound.DefaultInbound instead.
func (c *InboundConfig) ListenAddr() string { return c.ListenAddr_ }

// Extra returns the extra config.
// Deprecated: Use core/inbound.DefaultInbound instead.
func (c *InboundConfig) Extra() map[string]interface{} { return c.Extra_ }

// Validate validates the inbound config.
// Deprecated: Use core/inbound.Config.Validate instead.
func (c *InboundConfig) Validate() error {
	if c.Tag_ == "" {
		return ErrInboundTagRequired
	}
	if c.Port_ < 100 || c.Port_ > 65535 {
		return ErrInboundPortOutOfRange
	}
	return nil
}

// ToNative is not implemented for generic config.
// Container implementations should provide their own implementation.
// Deprecated: Use core/inbound.DefaultInbound instead.
func (c *InboundConfig) ToNative() ([]byte, error) {
	return nil, ErrInboundToNativeNotImplemented
}

// ErrInboundTagRequired is returned when inbound tag is empty.
// Deprecated: Use core/inbound.ErrInboundTagRequired instead.
var ErrInboundTagRequired = &InboundError{Message: "inbound tag is required"}

// ErrInboundPortOutOfRange is returned when port is out of valid range.
// Deprecated: Use core/inbound.ErrInboundPortOutOfRange instead.
var ErrInboundPortOutOfRange = &InboundError{Message: "port must be between 100 and 65535"}

// ErrInboundNotFound is returned when inbound is not found.
// Deprecated: Use core/inbound.ErrInboundNotFound instead.
var ErrInboundNotFound = &InboundError{Message: "inbound not found"}

// ErrInboundToNativeNotImplemented is returned when ToNative is not implemented.
// Deprecated: Use core/inbound.ErrInboundToNativeNotImplemented instead.
var ErrInboundToNativeNotImplemented = &InboundError{Message: "ToNative not implemented for generic config"}

// InboundError represents an inbound-related error.
// Deprecated: Use core/inbound.InboundError instead.
type InboundError struct {
	Message string
}

func (e *InboundError) Error() string {
	return e.Message
}
