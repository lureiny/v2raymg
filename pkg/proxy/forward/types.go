// Package forward implements a TCP port forwarding layer.
//
// Architecture:
//
//	Client → UserPort (forward layer) → Relay (counter + limiter) → Container InboundPort
//
// Each user of each inbound gets a dedicated listen port. The Relay handles
// bidirectional TCP forwarding with traffic counting and optional rate limiting.
// A single ForwardManager instance is shared process-wide to prevent port conflicts.
package forward

import (
	"fmt"
	"net"
	"strings"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// ForwardRule describes a single TCP forwarding rule (port-to-port).
// This is a basic layer that only handles: listenPort -> targetHost:targetPort
type ForwardRule struct {
	// ID is a unique rule identifier (auto-generated).
	ID string `json:"id"`

	// Username identifies the user this rule belongs to.
	// Used as the rule key for lookup.
	Username string `json:"username"`

	// ContainerType is the container type this rule belongs to (e.g., "xray", "hysteria").
	ContainerType contracts.ContainerType `json:"container_type"`

	// InboundTag is the inbound tag this rule forwards to.
	InboundTag string `json:"inbound_tag"`

	// Protocol is the proxy protocol (e.g., "vless", "vmess", "trojan").
	Protocol contracts.Protocol `json:"protocol"`

	// ListenAddr is a SPECIFIC IP literal the relay binds to (IPv4 or IPv6).
	// When set, exactly one socket is bound to that address and ListenStack is
	// ignored (the address family decides the stack). When empty, a wildcard
	// listener is bound according to ListenStack.
	ListenAddr string `json:"listen_addr"`

	// ListenStack selects which IP stack the WILDCARD listener binds when
	// ListenAddr is empty: "ipv4" (0.0.0.0 only), "ipv6" ([::] only), or
	// "dual" (both 0.0.0.0 and [::]). Empty inherits the ForwardManager's
	// global default (which itself defaults to "dual"). Ignored when
	// ListenAddr is a specific IP literal.
	ListenStack string `json:"listen_stack,omitempty"`

	// ListenPort is the port the relay listens on (assigned by PortAllocator).
	// If 0, a port will be auto-allocated.
	ListenPort uint32 `json:"listen_port"`

	// TargetAddr is the destination address to forward to (e.g. "127.0.0.1:443").
	TargetAddr string `json:"target_addr"`

	// UploadBytesPerSec is the upload rate limit in bytes/sec. 0 = unlimited.
	UploadBytesPerSec int64 `json:"upload_bytes_per_sec"`

	// DownloadBytesPerSec is the download rate limit in bytes/sec. 0 = unlimited.
	DownloadBytesPerSec int64 `json:"download_bytes_per_sec"`

	// MaxConnections is the maximum concurrent connections for this rule. 0 = unlimited.
	MaxConnections int `json:"max_connections"`

	// MaxClients is the maximum number of unique remote IPs allowed (slot limit). 0 = unlimited (no client limit).
	MaxClients int `json:"max_clients"`

	// ClientRecycleDelaySec is the delay in seconds before releasing an idle client slot.
	// Default: 60 seconds. Only used when MaxClients > 0.
	ClientRecycleDelaySec int `json:"client_recycle_delay_sec"`

	// ClientDrainSec is the drain timeout after one direction ends.
	// Default: 2 seconds. Only used when MaxClients > 0.
	ClientDrainSec int `json:"client_drain_sec"`

	// Network selects the transport: "tcp" (default, used when empty) or "udp".
	// TCP rules use a connection-oriented relay; UDP rules demultiplex by
	// client source address.
	Network string `json:"network,omitempty"`

	// UDPSessionIdleSec is the idle timeout for a UDP per-client session.
	// Ignored for TCP. 0 falls back to the UDPRelay default (60 seconds).
	UDPSessionIdleSec int `json:"udp_session_idle_sec,omitempty"`
}

// NetworkKind represents the transport selected by a ForwardRule.
type NetworkKind string

const (
	// NetworkTCP is the default transport.
	NetworkTCP NetworkKind = "tcp"
	// NetworkUDP is the UDP transport.
	NetworkUDP NetworkKind = "udp"
)

// Listen stack selectors for a wildcard forward listener.
const (
	// ListenStackDual binds both the IPv4 (0.0.0.0) and IPv6 ([::]) wildcard.
	// This is the default when nothing is configured.
	ListenStackDual = "dual"
	// ListenStackIPv4 binds only the IPv4 wildcard (0.0.0.0).
	ListenStackIPv4 = "ipv4"
	// ListenStackIPv6 binds only the IPv6 wildcard ([::], IPV6_V6ONLY).
	ListenStackIPv6 = "ipv6"
)

// ResolvedNetwork returns the transport for this rule, mapping the empty
// string to TCP for backward compatibility.
func (r *ForwardRule) ResolvedNetwork() NetworkKind {
	switch r.Network {
	case "", string(NetworkTCP):
		return NetworkTCP
	case string(NetworkUDP):
		return NetworkUDP
	default:
		return NetworkKind(r.Network)
	}
}

// Validate performs basic validation on a ForwardRule.
func (r *ForwardRule) Validate() error {
	if r.Username == "" {
		return fmt.Errorf("forward rule: username must not be empty")
	}
	if r.TargetAddr == "" {
		return fmt.Errorf("forward rule: target_addr must not be empty")
	}
	if r.ListenPort != 0 && (r.ListenPort < 100 || r.ListenPort > 65535) {
		return fmt.Errorf("forward rule: listen_port %d out of range [100, 65535]", r.ListenPort)
	}
	if r.ListenAddr != "" && net.ParseIP(r.ListenAddr) == nil {
		return fmt.Errorf("forward rule: listen_addr %q must be a valid IP literal", r.ListenAddr)
	}
	switch strings.ToLower(strings.TrimSpace(r.ListenStack)) {
	case "", ListenStackDual, ListenStackIPv4, ListenStackIPv6:
		// ok
	default:
		return fmt.Errorf("forward rule: unsupported listen_stack %q (want %q, %q, or %q)",
			r.ListenStack, ListenStackIPv4, ListenStackIPv6, ListenStackDual)
	}
	switch r.Network {
	case "", string(NetworkTCP), string(NetworkUDP):
		// ok
	default:
		return fmt.Errorf("forward rule: unsupported network %q (want \"tcp\" or \"udp\")", r.Network)
	}
	if r.UDPSessionIdleSec < 0 {
		return fmt.Errorf("forward rule: udp_session_idle_sec must be >= 0")
	}
	return nil
}

// RuleKey returns the unique key for this rule: "container_type:inbound_tag:username".
// This allows multiple forward rules per user (one per target port/inbound).
func (r *ForwardRule) RuleKey() string {
	return string(r.ContainerType) + ":" + r.InboundTag + ":" + r.Username
}

// TrafficSnapshot holds a point-in-time traffic measurement.
type TrafficSnapshot struct {
	// Username is the user this traffic belongs to.
	Username string `json:"username"`

	// Upload is the total uploaded bytes (client → container).
	Upload int64 `json:"upload"`

	// Download is the total downloaded bytes (container → client).
	Download int64 `json:"download"`

	// ActiveConnections is the current number of active connections.
	ActiveConnections int64 `json:"active_connections"`
}

// ForwardManagerStats is the aggregated stats returned by ForwardManager.
type ForwardManagerStats struct {
	// ByRule maps rule key (username) to its traffic snapshot.
	ByRule map[string]TrafficSnapshot `json:"by_rule"`

	// TotalRules is the total number of active rules.
	TotalRules int `json:"total_rules"`

	// TotalAllocatedPorts is the number of ports currently allocated.
	TotalAllocatedPorts int `json:"total_allocated_ports"`
}

// ForwardTrafficRecord is the basic traffic record from forward layer.
// It contains all metadata needed for multi-dimensional aggregation.
type ForwardTrafficRecord struct {
	// RuleKey uniquely identifies this traffic record.
	RuleKey string `json:"rule_key"`

	// Username is the username.
	Username string `json:"username"`

	// ContainerType is the container type (e.g., "xray", "hysteria").
	ContainerType contracts.ContainerType `json:"container_type"`

	// InboundTag is the inbound tag.
	InboundTag string `json:"inbound_tag"`

	// Protocol is the proxy protocol (e.g., "vless", "vmess", "trojan").
	Protocol contracts.Protocol `json:"protocol"`

	// ListenPort is the forward listen port.
	ListenPort uint32 `json:"listen_port"`

	// TargetAddr is the target address.
	TargetAddr string `json:"target_addr"`

	// UplinkBytes is the total bytes sent from client to container.
	UplinkBytes int64 `json:"uplink_bytes"`

	// DownlinkBytes is the total bytes received from container to client.
	DownlinkBytes int64 `json:"downlink_bytes"`

	// ActiveConnections is the current number of active connections.
	ActiveConnections int64 `json:"active_connections"`
}

// TrafficQuery defines a query for traffic stats with filters and group-by.
type TrafficQuery struct {
	// GroupBy specifies the dimensions to group by.
	// Valid values: "user", "container", "inbound", "rule".
	// Can be combined with "," (e.g., "user,container").
	GroupBy string

	// Username filters by username (empty = all users).
	Username string

	// ContainerType filters by container type (empty = all types).
	ContainerType contracts.ContainerType

	// InboundTag filters by inbound tag (empty = all inbounds).
	InboundTag string

	// RuleKey filters by rule key (empty = all rules).
	RuleKey string

	// Reset if true, resets the counters after reading.
	Reset bool
}

// BandwidthLimitKind specifies the direction of bandwidth limit.
type BandwidthLimitKind string

const (
	// BandwidthUpload limits upload bandwidth (client → target).
	BandwidthUpload BandwidthLimitKind = "upload"
	// BandwidthDownload limits download bandwidth (target → client).
	BandwidthDownload BandwidthLimitKind = "download"
)

// TrafficQueryResult contains the result of a traffic query.
type TrafficQueryResult struct {
	// Records is the list of matching traffic records.
	Records []ForwardTrafficRecord `json:"records"`

	// AggregatedStats contains aggregated statistics when GroupBy is specified.
	// Key format depends on GroupBy:
	// - "user": "username"
	// - "container": "container_type"
	// - "inbound": "container_type:inbound_tag"
	// - "rule": "rule_key"
	AggregatedStats map[string]ForwardTrafficRecord `json:"aggregated_stats"`

	// TotalUplink is the total uplink bytes across all matching records.
	TotalUplink int64 `json:"total_uplink"`

	// TotalDownlink is the total downlink bytes across all matching records.
	TotalDownlink int64 `json:"total_downlink"`
}

// ValidateBandwidthLimitKind checks if the kind is valid.
func ValidateBandwidthLimitKind(kind BandwidthLimitKind) error {
	switch kind {
	case BandwidthUpload, BandwidthDownload:
		return nil
	default:
		return fmt.Errorf("forward: invalid bandwidth limit kind %q", kind)
	}
}
