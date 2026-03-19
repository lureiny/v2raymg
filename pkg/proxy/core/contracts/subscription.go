package contracts

// SubscriptionSpec represents a generic subscription specification.
// Contains implementation-agnostic fields for subscription link generation.
// Provider-specific details (transport, TLS, etc.) go in Extensions.
type SubscriptionSpec struct {
	// Protocol is the proxy protocol (vless, vmess, trojan, shadowsocks).
	Protocol Protocol `json:"protocol"`

	// Host is the server hostname or IP address.
	Host string `json:"host"`

	// Port is the connection port (may differ from inbound listen port if forwarded).
	Port uint32 `json:"port"`

	// Password is the user credential (UUID for VMess/VLESS, password for Trojan/SS).
	Password string `json:"password"`

	// NodeName is the display name for this subscription entry.
	NodeName string `json:"node_name,omitempty"`

	// InboundTag identifies which inbound this subscription is for.
	InboundTag string `json:"inbound_tag"`

	// Username identifies which user this subscription is for.
	Username string `json:"username"`

	// URI is the generated subscription link (e.g., vless://..., vmess://...).
	// Populated by the provider-specific generator.
	URI string `json:"uri,omitempty"`

	// Extensions holds provider-specific fields needed for URI generation.
	// Examples: transport type, security mode, TLS settings, flow, etc.
	Extensions map[string]any `json:"extensions,omitempty"`
}

// SubscriptionRequest holds parameters for generating user subscriptions.
type SubscriptionRequest struct {
	// User is the user spec with credentials in Extensions.
	User UserSpec `json:"user"`

	// Host is the external server hostname/IP.
	Host string `json:"host"`

	// Port overrides the inbound port (0 = use inbound's port).
	Port uint32 `json:"port,omitempty"`

	// NodeName is the display name prefix for subscription entries.
	NodeName string `json:"node_name,omitempty"`

	// ExcludeProtocols lists protocol names to exclude from the subscription output.
	// e.g. ["vmess", "trojan"]
	ExcludeProtocols []string `json:"exclude_protocols,omitempty"`
}
