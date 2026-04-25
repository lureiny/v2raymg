// Package contracts defines the core business models for the proxy refactor.
// It contains truly generic, implementation-agnostic types.
//
// Key types:
//   - Protocol, Transport, Security: enumeration types
//   - InboundSpec: generic inbound specification
//   - ContainerModel: full desired state for a container
//   - UserSpec: user specification
//   - Stats: traffic statistics
package contracts

// Protocol represents the proxy protocol type.
type Protocol string

const (
	ProtocolVMess       Protocol = "vmess"
	ProtocolVLess       Protocol = "vless"
	ProtocolTrojan      Protocol = "trojan"
	ProtocolShadowsocks Protocol = "shadowsocks"
	ProtocolSOCKS5      Protocol = "socks5"
	ProtocolHTTP        Protocol = "http"
	ProtocolSnell       Protocol = "snell"
	ProtocolHysteria2   Protocol = "hysteria2"
	ProtocolTUIC        Protocol = "tuic"
	ProtocolAnyTLS      Protocol = "anytls"
)

// AllProtocols returns every protocol the system recognises. Kept in lockstep
// with IsValid — TestProtocolAllProtocolsMatchesIsValid enforces that both
// sides define the same set, so adding a new protocol constant that forgets
// either site fails at test time.
func AllProtocols() []Protocol {
	return []Protocol{
		ProtocolVMess, ProtocolVLess, ProtocolTrojan, ProtocolShadowsocks,
		ProtocolSOCKS5, ProtocolHTTP, ProtocolSnell, ProtocolHysteria2,
		ProtocolTUIC, ProtocolAnyTLS,
	}
}

// IsValid checks whether the protocol is a known value.
func (p Protocol) IsValid() bool {
	switch p {
	case ProtocolVMess, ProtocolVLess, ProtocolTrojan, ProtocolShadowsocks,
		ProtocolSOCKS5, ProtocolHTTP, ProtocolSnell, ProtocolHysteria2,
		ProtocolTUIC, ProtocolAnyTLS:
		return true
	}
	return false
}

func (p Protocol) String() string { return string(p) }

// Transport represents the network transport type.
type Transport string

const (
	TransportTCP         Transport = "tcp"
	TransportWS          Transport = "ws"
	TransportGRPC        Transport = "grpc"
	TransportHTTPUpgrade Transport = "httpupgrade"
	TransportXHTTP       Transport = "xhttp"
	TransportSplitHTTP   Transport = "splithttp"
	TransportMKCP        Transport = "mkcp"  // legacy, not supported in FastAdd
	TransportQUIC        Transport = "quic"  // legacy, not supported in FastAdd
	TransportHTTP        Transport = "http"  // legacy, removed from xray-core
)

// AllTransports returns all supported transports.
func AllTransports() []Transport {
	return []Transport{
		TransportTCP, TransportWS, TransportGRPC,
		TransportHTTPUpgrade, TransportXHTTP, TransportSplitHTTP,
		TransportMKCP, TransportQUIC, TransportHTTP,
	}
}

// IsValid checks whether the transport is a known value.
func (t Transport) IsValid() bool {
	switch t {
	case TransportTCP, TransportWS, TransportGRPC,
		TransportHTTPUpgrade, TransportXHTTP, TransportSplitHTTP,
		TransportMKCP, TransportQUIC, TransportHTTP:
		return true
	}
	return false
}

func (t Transport) String() string { return string(t) }

// Security represents the TLS/XTLS/Reality security mode.
type Security string

const (
	SecurityNone    Security = "none"
	SecurityTLS     Security = "tls"
	SecurityReality Security = "reality"
)

// AllSecurityModes returns all supported security modes.
func AllSecurityModes() []Security {
	return []Security{SecurityNone, SecurityTLS, SecurityReality}
}

// IsValid checks whether the security mode is a known value.
func (s Security) IsValid() bool {
	switch s {
	case SecurityNone, SecurityTLS, SecurityReality:
		return true
	}
	return false
}

func (s Security) String() string { return string(s) }

// ContainerType represents the proxy software flavour.
type ContainerType string

const (
	ContainerXray     ContainerType = "xray"
	ContainerV2ray    ContainerType = "v2ray"
	ContainerHysteria ContainerType = "hysteria"
	ContainerSnell    ContainerType = "snell"
	ContainerMihomo   ContainerType = "mihomo"
)

func (c ContainerType) IsValid() bool {
	switch c {
	case ContainerXray, ContainerV2ray, ContainerHysteria, ContainerSnell, ContainerMihomo:
		return true
	}
	return false
}

func (c ContainerType) String() string { return string(c) }

// ValidateCombination checks whether a (Protocol, Transport, Security) triple
// is generally valid. Provider-specific restrictions are handled by InboundAdapter.
func ValidateCombination(p Protocol, t Transport, s Security) error {
	if !p.IsValid() {
		return &ValidationError{Field: "protocol", Msg: "unknown protocol: " + string(p)}
	}
	if !t.IsValid() {
		return &ValidationError{Field: "transport", Msg: "unknown transport: " + string(t)}
	}
	if !s.IsValid() {
		return &ValidationError{Field: "security", Msg: "unknown security: " + string(s)}
	}
	return nil
}

// ValidationError represents a validation error.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Msg
}
