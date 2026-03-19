package contracts

// ContainerModel is the full desired state for a single proxy container.
// It aggregates all inbounds, users, and metadata that a container should run with.
type ContainerModel struct {
	// ID is the container identifier.
	ID string `json:"id"`

	// Type is the container type (xray/v2ray/hysteria).
	Type ContainerType `json:"type"`

	// Version is the desired binary version (empty = latest).
	Version string `json:"version,omitempty"`

	// ConfigFile is the path to the generated config file.
	ConfigFile string `json:"config_file"`

	// BinaryPath is the path to the proxy binary.
	BinaryPath string `json:"binary_path"`

	// APIPort is the gRPC API port for runtime operations.
	APIPort int `json:"api_port"`

	// Inbounds are the desired inbound specs.
	Inbounds []InboundSpec `json:"inbounds"`

	// UserBindings maps inbound tag -> list of users bound to that inbound.
	UserBindings map[string][]UserSpec `json:"user_bindings,omitempty"`
}

// Stats holds traffic statistics for a user or inbound.
type Stats struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "user" / "inbound" / "outbound"
	Uplink   int64  `json:"uplink"`
	Downlink int64  `json:"downlink"`
	Proxy    string `json:"proxy,omitempty"` // "xray" / "v2ray" / "hysteria"
}
