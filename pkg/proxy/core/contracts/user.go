package contracts

import (
	"fmt"
	"time"
)

// User is an alias for UserSpec.
type User = UserSpec

// UserSpec represents a generic user in the proxy system.
// Only contains truly implementation-agnostic fields.
// Provider-specific fields (e.g., UUID, AlterID, Flow) should be stored in Extensions.
type UserSpec struct {
	// Username is the unique identifier for the user.
	Username string `json:"username,omitempty"`

	// Password is the user's authentication credential (used by Trojan, Shadowsocks, SOCKS5).
	Password string `json:"password,omitempty"`

	// Level is the user's access level.
	Level uint32 `json:"level"`

	// Protocol is the proxy protocol.
	Protocol Protocol `json:"protocol"`

	// TTL is the time to live (for expiry).
	TTL time.Time `json:"ttl,omitempty"`

	// ExpiryTime is the user's expiry time.
	// Zero means never expires.
	ExpiryTime time.Time `json:"expiry_time,omitempty"`

	// TrafficLimit is the traffic limit in bytes.
	// Zero means no limit.
	TrafficLimit int64 `json:"traffic_limit,omitempty"`

	// UploadLimit is the upload limit in bytes.
	// Zero means no limit.
	UploadLimit int64 `json:"upload_limit,omitempty"`

	// DownloadLimit is the download limit in bytes.
	// Zero means no limit.
	DownloadLimit int64 `json:"download_limit,omitempty"`

	// BandwidthUploadBps is the upload bandwidth limit in bytes per second.
	// Zero means no limit.
	BandwidthUploadBps int64 `json:"bandwidth_upload_bps,omitempty"`

	// BandwidthDownloadBps is the download bandwidth limit in bytes per second.
	// Zero means no limit.
	BandwidthDownloadBps int64 `json:"bandwidth_download_bps,omitempty"`

	// MaxClients is the maximum number of unique remote IPs allowed per user.
	// Zero means no client limit.
	MaxClients int `json:"max_clients,omitempty"`

	// ClientRecycleDelaySec is the delay in seconds before releasing an idle client slot.
	// Default: 60 seconds. Only used when MaxClients > 0.
	ClientRecycleDelaySec int `json:"client_recycle_delay_sec,omitempty"`

	// ClientDrainSec is the drain timeout after one direction ends.
	// Default: 2 seconds. Only used when MaxClients > 0.
	ClientDrainSec int `json:"client_drain_sec,omitempty"`

	// TrafficTotalUplink is the lifetime cumulative uplink bytes, persisted to DB.
	TrafficTotalUplink int64 `json:"traffic_total_uplink,omitempty"`
	// TrafficTotalDownlink is the lifetime cumulative downlink bytes, persisted to DB.
	TrafficTotalDownlink int64 `json:"traffic_total_downlink,omitempty"`

	// BindPorts is the list of ports bound to this user.
	BindPorts []uint32 `json:"bind_ports,omitempty"`

	// PortMappings records dstPort -> forwardPort for deterministic port allocation.
	// Used to restore forward rules with the same port after restart.
	// Key: dstPort (inbound backend port), Value: forwardPort (user-facing port).
	// This ensures port mappings persist even when inbound tags change.
	PortMappings map[uint32]uint32 `json:"port_mappings,omitempty"`

	// DeletionState indicates the deletion state of the user.
	// Empty means active, "deleting" means marked for deletion.
	// When in "deleting" state, user is hidden from normal queries but
	// can be queried for cleanup information.
	DeletionState string `json:"deletion_state,omitempty"`

	// Extensions holds provider-specific configuration.
	// Keys are provider names (e.g., "xray", "sing-box"), values are provider-specific configs.
	// Examples for xray: UUID, AlterID, Flow, Method, etc.
	Extensions map[string]any `json:"extensions,omitempty"`
}

// IsExpired returns true if the user has expired.
func (u *UserSpec) IsExpired() bool {
	if u.ExpiryTime.IsZero() {
		return false
	}
	return time.Now().After(u.ExpiryTime)
}

// IsValid checks if the user spec is valid.
func (u *UserSpec) IsValid() bool {
	if u.Username == "" {
		return false
	}
	// Basic validation - extensions may contain protocol-specific required fields
	return u.Protocol.IsValid()
}

// Validate performs basic validation on UserSpec.
func (u *UserSpec) Validate() error {
	if u.Username == "" {
		return fmt.Errorf("username is required")
	}
	if !u.Protocol.IsValid() {
		return fmt.Errorf("invalid protocol: %s", u.Protocol)
	}
	return nil
}

// GetExtension returns a provider-specific extension value.
func (u *UserSpec) GetExtension(key string) (any, bool) {
	if u.Extensions == nil {
		return nil, false
	}
	val, ok := u.Extensions[key]
	return val, ok
}

// SetExtension sets a provider-specific extension value.
func (u *UserSpec) SetExtension(key string, value any) {
	if u.Extensions == nil {
		u.Extensions = make(map[string]any)
	}
	u.Extensions[key] = value
}

// DeletionState constants
const (
	// DeletionStateActive means the user is active.
	DeletionStateActive = ""
	// DeletionStateDeleting means the user is marked for deletion.
	DeletionStateDeleting = "deleting"
)

// IsDeleting returns true if the user is marked for deletion.
func (u *UserSpec) IsDeleting() bool {
	return u.DeletionState == DeletionStateDeleting
}

// MarkDeleting marks the user for deletion.
func (u *UserSpec) MarkDeleting() {
	u.DeletionState = DeletionStateDeleting
}

// MarkActive marks the user as active (undo deletion).
func (u *UserSpec) MarkActive() {
	u.DeletionState = DeletionStateActive
}
