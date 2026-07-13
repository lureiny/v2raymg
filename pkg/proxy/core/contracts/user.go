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

	// AuthToken is the user's unique authentication token, auto-generated on creation.
	// Used for subscription access and Hysteria2 authentication.
	// Not a user-chosen password — decoupled from the login password (LoginPassword).
	AuthToken string `json:"auth_token,omitempty"`

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

	// Role is the user's role for frontend access control: "admin" or "normal".
	// Default: "normal". Not related to proxy protocol.
	Role string `json:"role,omitempty"`

	// LoginPassword is the bcrypt hash of the frontend login password (SHA256+bcrypt).
	// Not used by any proxy protocol. Empty means not yet initialized (pending migration).
	LoginPassword string `json:"-"` // never serialize to avoid leaking

	// DeletionState indicates the deletion state of the user.
	// Empty means active, "deleting" means marked for deletion.
	// When in "deleting" state, user is hidden from normal queries but
	// can be queried for cleanup information.
	DeletionState string `json:"deletion_state,omitempty"`

	// TargetGroup is the cluster group this user belongs to.
	// Used by cluster sync to determine which nodes should have this user.
	// Empty means no group assignment (local-only or default group).
	TargetGroup string `json:"target_group,omitempty"`

	// UpdatedAtUs is the version timestamp in microseconds for cluster sync.
	// Zero means the user has never been cluster-synced.
	UpdatedAtUs int64 `json:"updated_at_us,omitempty"`

	// OriginNode is the node name that produced the current version.
	// Used for version arbitration during cluster sync.
	OriginNode string `json:"origin_node,omitempty"`

	// Hash is the SHA-256 digest of canonical fields for cluster conflict detection.
	Hash string `json:"hash,omitempty"`

}

// Clone returns a deep copy safe to share across goroutines without a lock.
// All scalar fields are value-copied (time.Time and its shared *Location are
// immutable, so a value copy is safe); the two reference fields — BindPorts
// and PortMappings — are duplicated so later mutations of the original never
// race with a reader of the clone. A nil receiver clones to nil.
//
// Emitted UserEvents must carry a Clone, never the live *User held in the
// manager's map: subscribers read event.User off the lock while mutateUser
// writes the same struct under it (2026-07-10 review finding UM-#56).
func (u *UserSpec) Clone() *UserSpec {
	if u == nil {
		return nil
	}
	c := *u
	if u.BindPorts != nil {
		c.BindPorts = make([]uint32, len(u.BindPorts))
		copy(c.BindPorts, u.BindPorts)
	}
	if u.PortMappings != nil {
		c.PortMappings = make(map[uint32]uint32, len(u.PortMappings))
		for k, v := range u.PortMappings {
			c.PortMappings[k] = v
		}
	}
	return &c
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
	return u.Username != ""
}

// Validate performs basic validation on UserSpec.
func (u *UserSpec) Validate() error {
	if u.Username == "" {
		return fmt.Errorf("username is required")
	}
	return nil
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
