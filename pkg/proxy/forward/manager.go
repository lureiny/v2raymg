package forward

// ForwardManager manages all TCP forwarding rules for the entire process.
// It is intended to be a single shared instance to prevent port conflicts.
//
// This is a basic port-to-port forwarding layer that does not know about
// inbound concepts. Higher layers (like UserManager) handle user/inbound logic.
type ForwardManager interface {
	// AddRule adds a forwarding rule. If ListenPort is 0, one is auto-allocated.
	// Starts the relay immediately. Returns the rule with allocated port filled in.
	AddRule(rule ForwardRule) (*ForwardRule, error)

	// RemoveRule stops the relay and releases the port for the given rule key.
	// Rule key format: "container_type:inbound_tag:user_email".
	RemoveRule(ruleKey string) error

	// RemoveRulesByUser removes all rules for the given username.
	RemoveRulesByUser(username string) error

	// RemoveRulesByInbound removes all rules for the given inbound tag.
	// This is used when an inbound is stopped/destroyed.
	RemoveRulesByInbound(inboundTag string) error

	// GetRule returns the rule for the given key, or nil if not found.
	GetRule(ruleKey string) *ForwardRule

	// GetRulesByUser returns all rules for the given username.
	GetRulesByUser(username string) []*ForwardRule

	// GetAllRules returns all active rules.
	GetAllRules() []*ForwardRule

	// GetTraffic returns traffic stats for the given rule key.
	// If reset is true, counters are reset after reading.
	GetTraffic(ruleKey string, reset bool) (*TrafficSnapshot, error)

	// GetAllTraffic returns traffic stats for all rules.
	// If reset is true, counters are reset after reading.
	GetAllTraffic(reset bool) *ForwardManagerStats

	// GetAllTrafficRecords returns all traffic records with full metadata.
	// If reset is true, counters are reset after reading.
	// This is the primary method for forward-only stats collection.
	GetAllTrafficRecords(reset bool) []ForwardTrafficRecord

	// QueryTrafficStats queries traffic stats with filters and group-by.
	// Supports multi-dimensional aggregation based on GroupBy field.
	QueryTrafficStats(query TrafficQuery) TrafficQueryResult

	// UpdateRateLimit updates the rate limit for an existing rule.
	// The relay applies the new limit to new connections; existing connections
	// continue with the old limit until they close.
	UpdateRateLimit(ruleKey string, uploadBPS, downloadBPS int64) error

	// SetUserBandwidthLimit sets the aggregate bandwidth limit for a user.
	// This limit applies across all rules (inbounds) belonging to the same user.
	// kind specifies "upload" or "download". bytesPerSec <= 0 means unlimited.
	// Setting a value immediately applies to all existing and new rules.
	SetUserBandwidthLimit(username string, kind BandwidthLimitKind, bytesPerSec int64) error

	// GetUserBandwidthLimit returns the current bandwidth limit for a user.
	// Returns ok=false if no limit is set.
	GetUserBandwidthLimit(username string, kind BandwidthLimitKind) (bytesPerSec int64, ok bool)

	// SetUserClientLimitConfig sets the client limit config for a user.
	// This updates the limiter configuration for all existing rules of the user.
	SetUserClientLimitConfig(username string, config ClientLimitConfig) error

	// GetUserClientLimitConfig returns the current client limit config for a user.
	// Returns ok=false if no client limit is set.
	GetUserClientLimitConfig(username string) (config ClientLimitConfig, ok bool)

	// DropUser releases all per-user state (bandwidth limiter, client-limit
	// limiter, stored client-limit config). Callers MUST have removed all of
	// the user's rules first — otherwise active relays would be left holding
	// dangling references to the dropped limiter objects. Returns true if any
	// state was actually released.
	DropUser(username string) bool

	// Close stops all relays and releases all ports. After Close, the manager
	// cannot be used.
	Close() error
}
