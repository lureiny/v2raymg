package forward

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/log"
)

// ForwardManagerOption is a function that configures a ForwardManager.
type ForwardManagerOption func(*DefaultForwardManager)

// WithPortRange sets the allocatable port range for the ForwardManager.
// Default range is 10000-65535 if not specified.
func WithPortRange(minPort, maxPort uint32) ForwardManagerOption {
	return func(m *DefaultForwardManager) {
		cfg := PortAllocatorConfig{
			MinPort: minPort,
			MaxPort: maxPort,
		}
		alloc, err := NewPortAllocator(cfg)
		if err == nil {
			m.allocator = alloc
		}
	}
}

// WithListenStack sets the default IP stack for wildcard forward listeners:
// "dual" (default), "ipv4", or "ipv6". Unrecognized values are ignored.
// Individual rules may override via ForwardRule.ListenStack.
func WithListenStack(stack string) ForwardManagerOption {
	return func(m *DefaultForwardManager) {
		if s := normalizeListenStack(stack); s != "" {
			m.defaultListenStack = s
		}
	}
}

// NewForwardManager creates a new ForwardManager with options.
// If no options are provided, default port range is 10000-65535 with random allocation.
func NewForwardManager(opts ...ForwardManagerOption) (*DefaultForwardManager, error) {
	// Default config: user-facing forward port range with random allocation
	defaultCfg := PortAllocatorConfig{
		MinPort:   10000,
		MaxPort:   65535,
		UseRandom: true,
	}
	alloc, err := NewPortAllocator(defaultCfg)
	if err != nil {
		return nil, fmt.Errorf("forward_manager: %w", err)
	}

	m := &DefaultForwardManager{
		allocator:              alloc,
		traffic:                NewTrafficRegistry(),
		rules:                  make(map[string]*managedRule),
		userBandwidth:          make(map[string]*userBandwidthLimiter),
		userClientLimiters:     make(map[string]ClientLimiter),
		userClientLimitConfigs: make(map[string]ClientLimitConfig),
		defaultListenStack:     ListenStackDual,
	}

	// Apply options
	for _, opt := range opts {
		opt(m)
	}

	return m, nil
}

// managedRule is an active forwarding rule with its relay and traffic counter.
type managedRule struct {
	rule              ForwardRule
	relay             Relay
	counter           *TrafficCounter
	userBandwidthLim *userBandwidthLimiter // shared per-user limiter, nil if no user limit set
	clientLimiter    ClientLimiter         // remote IP based client limiter, nil if no client limit set
}

// DefaultForwardManager is the standard implementation of ForwardManager.
// It manages port allocation, relays, traffic counters, and rate limiters.
//
// Concurrency model:
//   - m.mu guards the rules map and lifecycle transitions (add/remove/close).
//   - Traffic bytes/connections are tracked by TrafficCounter using atomics, so data path
//     counting does not contend on manager lock.
//   - Rule reads (Get*/Stats) use RLock to reduce contention against data-plane operations.
//   - User bandwidth limits use per-user token buckets stored in userBandwidth.
//     Data path (relay) accesses these buckets without holding manager lock.
//   - User-level client limiters (userClientLimiters) are shared across all rules of the same user.
//   - User-level client limit configs (userClientLimitConfigs) store config for rules without active limiter.
type DefaultForwardManager struct {
	mu           sync.RWMutex
	allocator    *PortAllocator
	traffic      *TrafficRegistry
	rules        map[string]*managedRule // key = ruleKey
	closed       bool
	userBandwidth         map[string]*userBandwidthLimiter // key = username
	userClientLimiters    map[string]ClientLimiter       // key = username, active limiter instances
	userClientLimitConfigs map[string]ClientLimitConfig  // key = username, stored config (may have no active limiter)
	// defaultListenStack is the fallback IP stack for wildcard listeners when a
	// rule does not set ForwardRule.ListenStack: "dual" (default), "ipv4", or "ipv6".
	defaultListenStack string
}

// NewDefaultForwardManager creates a new ForwardManager with the given port allocator config.
// Zero values are filled with sensible defaults:
//   - MinPort: 10000
//   - MaxPort: 65535
//   - UseRandom: true (always; random allocation is the only supported mode)
func NewDefaultForwardManager(allocCfg PortAllocatorConfig) (*DefaultForwardManager, error) {
	if allocCfg.MinPort == 0 {
		allocCfg.MinPort = 10000
	}
	if allocCfg.MaxPort == 0 {
		allocCfg.MaxPort = 65535
	}
	allocCfg.UseRandom = true // always random; round-robin is not a supported use-case

	alloc, err := NewPortAllocator(allocCfg)
	if err != nil {
		return nil, fmt.Errorf("forward_manager: %w", err)
	}

	listenStack := normalizeListenStack(allocCfg.ListenStack)
	if listenStack == "" {
		listenStack = ListenStackDual
	}

	return &DefaultForwardManager{
		allocator:              alloc,
		traffic:                NewTrafficRegistry(),
		rules:                  make(map[string]*managedRule),
		userBandwidth:          make(map[string]*userBandwidthLimiter),
		userClientLimiters:     make(map[string]ClientLimiter),
		userClientLimitConfigs: make(map[string]ClientLimitConfig),
		defaultListenStack:     listenStack,
	}, nil
}

// AddRule adds a forwarding rule, allocates a port, and starts the relay.
//
// Lifecycle intent:
// - Port allocation happens before relay start to guarantee uniqueness.
// - If relay start fails, allocated port and traffic registry entry are rolled back immediately.
// - Returned rule is a copy; internal state remains manager-owned.
func (m *DefaultForwardManager) AddRule(rule ForwardRule) (*ForwardRule, error) {
	if err := rule.Validate(); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, fmt.Errorf("forward_manager: closed")
	}

	key := rule.RuleKey()
	if _, exists := m.rules[key]; exists {
		return nil, fmt.Errorf("forward_manager: rule %q already exists", key)
	}

	// Allocate port
	var port uint32
	var err error
	if rule.ListenPort != 0 {
		if err = m.allocator.AllocateSpecific(rule.ListenPort); err != nil {
			return nil, fmt.Errorf("forward_manager: %w", err)
		}
		port = rule.ListenPort
	} else {
		port, err = m.allocator.Allocate()
		if err != nil {
			return nil, fmt.Errorf("forward_manager: %w", err)
		}
		rule.ListenPort = port
	}

	// Create traffic counter
	counter := m.traffic.GetOrCreate(key)

	// Build limiters: always ensure a userBandwidthLimiter exists for stable references.
	// This guarantees that SetUserBandwidthLimit called after AddRule can update rates
	// in-place, and existing relays will see the change immediately.
	var limiterUp, limiterDown Limiter
	var userBandwidthLim *userBandwidthLimiter

	if _, ok := m.userBandwidth[rule.Username]; !ok {
		// Create placeholder limiter (rate=0 = passthrough/unlimited).
		// Seed from rule-level limits if present.
		upRate, downRate := int64(0), int64(0)
		upSet, downSet := false, false
		if rule.UploadBytesPerSec > 0 {
			upRate = rule.UploadBytesPerSec
			upSet = true
		}
		if rule.DownloadBytesPerSec > 0 {
			downRate = rule.DownloadBytesPerSec
			downSet = true
		}
		m.userBandwidth[rule.Username] = newUserBandwidthLimiter(upRate, downRate, upSet, downSet)
	}
	userLimiter := m.userBandwidth[rule.Username]
	userBandwidthLim = userLimiter
	limiterUp = userLimiter.UploadLimiter()
	limiterDown = userLimiter.DownloadLimiter()

	// Resolve the set of sockets to bind for this rule. A specific ListenAddr or
	// a single-stack wildcard yields one endpoint; the default dual-stack yields
	// two (IPv4 wildcard + best-effort IPv6 wildcard).
	endpoints, err := resolveListenEndpoints(rule.ListenAddr, rule.ListenStack, m.defaultListenStack, port)
	if err != nil {
		m.allocator.Release(port)
		m.traffic.Remove(key)
		return nil, fmt.Errorf("forward_manager: %w", err)
	}
	listenDesc := describeEndpoints(endpoints)

	// Determine effective client limit config with priority: rule > stored config > default
	var effectiveMaxClients int
	var recycleDelay, drainSec int

	// Priority 1: rule explicitly sets MaxClients
	if rule.MaxClients > 0 {
		effectiveMaxClients = rule.MaxClients
		recycleDelay = rule.ClientRecycleDelaySec
		drainSec = rule.ClientDrainSec
	} else if rule.MaxClients < 0 {
		// rule explicitly disables (e.g., -1 or negative)
		effectiveMaxClients = 0
	} else {
		// rule.MaxClients == 0, check stored config
		if storedConfig, exists := m.userClientLimitConfigs[rule.Username]; exists {
			effectiveMaxClients = storedConfig.MaxClients
			recycleDelay = storedConfig.RecycleDelaySec
			drainSec = storedConfig.SingleDirectionDrainSec
		} else {
			effectiveMaxClients = 0 // no limit
		}
	}

	// Get or create user-level client limiter (shared across all rules of the same user).
	// The limiter is always created — even when effectiveMaxClients <= 0 — so that
	// a later SetUserClientLimitConfig can update the limit in-place on this
	// relay without requiring the rule to be re-added. See clientlimit.go for
	// the passthrough semantics (MaxClients <= 0 = unlimited, no rejection).
	//
	// Apply the recycle/drain defaults when MaxClients > 0 so that callers that
	// only pass a positive MaxClients inherit sane timings. For passthrough
	// entries the zero values are fine — the limiter's own constructor and
	// SetConfig paths backfill defaults anyway.
	if effectiveMaxClients > 0 {
		if recycleDelay <= 0 {
			recycleDelay = 60
		}
		if drainSec <= 0 {
			drainSec = 2
		}
	}
	config := ClientLimitConfig{
		MaxClients:              effectiveMaxClients,
		RecycleDelaySec:        recycleDelay,
		SingleDirectionDrainSec: drainSec,
	}
	// An explicit `rule.MaxClients < 0` signals "this rule wants passthrough"
	// without claiming authority over the user-level policy. We still attach
	// a passthrough limiter to the relay, but we must NOT stomp on any
	// storedConfig an admin set earlier — otherwise simply adding a new rule
	// with MaxClients=-1 would silently erase the per-user limit.
	ruleOwnsUserPolicy := rule.MaxClients > 0

	var clientLimiter ClientLimiter
	if existingLimiter, exists := m.userClientLimiters[rule.Username]; exists {
		if ruleOwnsUserPolicy {
			if limiter, ok := existingLimiter.(*remoteIPClientLimiter); ok {
				limiter.SetConfig(config)
			}
			m.userClientLimitConfigs[rule.Username] = config
		}
		clientLimiter = existingLimiter
	} else {
		clientLimiter = newRemoteIPClientLimiter(config)
		m.userClientLimiters[rule.Username] = clientLimiter
		if ruleOwnsUserPolicy {
			m.userClientLimitConfigs[rule.Username] = config
		}
	}

	// buildRelay constructs a relay for one bind endpoint. All endpoints of the
	// same rule share the counter and user-level limiters, so traffic counting
	// and client limits treat the (possibly dual-stack) rule as one unit.
	buildRelay := func(ep listenEndpoint) Relay {
		switch rule.ResolvedNetwork() {
		case NetworkUDP:
			idle := time.Duration(rule.UDPSessionIdleSec) * time.Second
			return NewUDPRelay(UDPRelayConfig{
				ListenAddr:         ep.address,
				Family:             ep.family,
				TargetAddr:         rule.TargetAddr,
				Counter:            counter,
				LimiterUp:          limiterUp,
				LimiterDown:        limiterDown,
				MaxSessions:        rule.MaxConnections,
				ClientLimiter:      clientLimiter,
				SessionIdleTimeout: idle,
			})
		default:
			return NewTCPRelay(TCPRelayConfig{
				ListenAddr:    ep.address,
				Family:        ep.family,
				TargetAddr:    rule.TargetAddr,
				Counter:       counter,
				LimiterUp:     limiterUp,
				LimiterDown:   limiterDown,
				MaxConns:      rule.MaxConnections,
				ClientLimiter: clientLimiter,
			})
		}
	}

	// A single endpoint uses a plain relay (bind failure is fatal). Multiple
	// endpoints (dual-stack) use a multiRelay so the best-effort IPv6 half can
	// be skipped on IPv6-disabled hosts without failing the rule.
	var relay Relay
	if len(endpoints) == 1 {
		relay = buildRelay(endpoints[0])
	} else {
		children := make([]relayChild, 0, len(endpoints))
		for _, ep := range endpoints {
			children = append(children, relayChild{relay: buildRelay(ep), optional: ep.optional})
		}
		relay = newMultiRelay(children)
	}

	if err := relay.Start(); err != nil {
		m.allocator.Release(port)
		m.traffic.Remove(key)
		log.Debug("[ForwardManager] relay start failed", "key", key, "listen", listenDesc, "target", rule.TargetAddr, "network", rule.ResolvedNetwork(), "err", err)
		return nil, fmt.Errorf("forward_manager: relay start: %w", err)
	}

	m.rules[key] = &managedRule{
		rule:              rule,
		relay:             relay,
		counter:           counter,
		userBandwidthLim:  userBandwidthLim,
		clientLimiter:     clientLimiter,
	}

	log.Info("[ForwardManager] rule added", "key", key, "listen", relay.ListenAddr(), "target", rule.TargetAddr, "user", rule.Username)

	result := rule // copy
	return &result, nil
}

// RemoveRule stops the relay and releases the port for the given rule key.
func (m *DefaultForwardManager) RemoveRule(ruleKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mr, ok := m.rules[ruleKey]
	if !ok {
		return fmt.Errorf("forward_manager: rule %q not found", ruleKey)
	}

	log.Info("[ForwardManager] rule removed", "key", ruleKey, "port", mr.rule.ListenPort, "user", mr.rule.Username)
	mr.relay.Stop()
	m.allocator.Release(mr.rule.ListenPort)
	m.traffic.Remove(ruleKey)
	delete(m.rules, ruleKey)

	return nil
}

// RemoveRulesByUser removes all rules for the given username.
func (m *DefaultForwardManager) RemoveRulesByUser(username string) error {
	m.mu.Lock()
	keysToRemove := make([]string, 0)
	for key, mr := range m.rules {
		if mr.rule.Username == username {
			keysToRemove = append(keysToRemove, key)
		}
	}
	m.mu.Unlock()

	for _, key := range keysToRemove {
		if err := m.RemoveRule(key); err != nil {
			return err
		}
	}
	return nil
}

// RemoveRulesByInbound removes all rules for the given inbound tag.
// This is used when an inbound is stopped/destroyed.
func (m *DefaultForwardManager) RemoveRulesByInbound(inboundTag string) error {
	m.mu.Lock()
	keysToRemove := make([]string, 0)
	for key, mr := range m.rules {
		if mr.rule.InboundTag == inboundTag {
			keysToRemove = append(keysToRemove, key)
		}
	}
	m.mu.Unlock()

	for _, key := range keysToRemove {
		if err := m.RemoveRule(key); err != nil {
			return err
		}
	}
	return nil
}

// GetRule returns the rule for the given key, or nil if not found.
func (m *DefaultForwardManager) GetRule(ruleKey string) *ForwardRule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if mr, ok := m.rules[ruleKey]; ok {
		r := mr.rule // copy
		return &r
	}
	return nil
}

// GetRulesByUser returns all rules for the given username.
func (m *DefaultForwardManager) GetRulesByUser(username string) []*ForwardRule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*ForwardRule, 0)
	for _, mr := range m.rules {
		if mr.rule.Username == username {
			r := mr.rule // copy
			result = append(result, &r)
		}
	}
	return result
}

// GetAllRules returns all active rules.
func (m *DefaultForwardManager) GetAllRules() []*ForwardRule {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*ForwardRule, 0, len(m.rules))
	for _, mr := range m.rules {
		r := mr.rule // copy
		result = append(result, &r)
	}
	return result
}

// GetTraffic returns traffic stats for the given rule key.
func (m *DefaultForwardManager) GetTraffic(ruleKey string, reset bool) (*TrafficSnapshot, error) {
	m.mu.RLock()
	mr, ok := m.rules[ruleKey]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("forward_manager: rule %q not found", ruleKey)
	}

	upload, download, active := mr.counter.Snapshot(reset)
	return &TrafficSnapshot{
		Username:          mr.rule.Username,
		Upload:            upload,
		Download:          download,
		ActiveConnections: active,
	}, nil
}

// GetAllTraffic returns traffic stats for all rules.
func (m *DefaultForwardManager) GetAllTraffic(reset bool) *ForwardManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &ForwardManagerStats{
		ByRule:              make(map[string]TrafficSnapshot, len(m.rules)),
		TotalRules:          len(m.rules),
		TotalAllocatedPorts: m.allocator.AllocatedCount(),
	}

	for key, mr := range m.rules {
		upload, download, active := mr.counter.Snapshot(reset)
		stats.ByRule[key] = TrafficSnapshot{
			Username:          mr.rule.Username,
			Upload:            upload,
			Download:          download,
			ActiveConnections: active,
		}
	}

	return stats
}

// GetAllTrafficRecords returns all traffic records with full metadata.
func (m *DefaultForwardManager) GetAllTrafficRecords(reset bool) []ForwardTrafficRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	records := make([]ForwardTrafficRecord, 0, len(m.rules))
	for key, mr := range m.rules {
		upload, download, active := mr.counter.Snapshot(reset)
		records = append(records, ForwardTrafficRecord{
			RuleKey:            key,
			Username:           mr.rule.Username,
			ContainerType:      mr.rule.ContainerType,
			InboundTag:         mr.rule.InboundTag,
			Protocol:           mr.rule.Protocol,
			ListenPort:         mr.rule.ListenPort,
			TargetAddr:         mr.rule.TargetAddr,
			UplinkBytes:        upload,
			DownlinkBytes:      download,
			ActiveConnections:  active,
		})
	}

	return records
}

// QueryTrafficStats queries traffic stats with filters and group-by.
func (m *DefaultForwardManager) QueryTrafficStats(query TrafficQuery) TrafficQueryResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Collect all matching records
	var matchingRecords []ForwardTrafficRecord
	var totalUplink, totalDownlink int64

	for key, mr := range m.rules {
		upload, download, active := mr.counter.Snapshot(query.Reset)

		record := ForwardTrafficRecord{
			RuleKey:            key,
			Username:           mr.rule.Username,
			ContainerType:      mr.rule.ContainerType,
			InboundTag:         mr.rule.InboundTag,
			ListenPort:         mr.rule.ListenPort,
			TargetAddr:         mr.rule.TargetAddr,
			UplinkBytes:        upload,
			DownlinkBytes:      download,
			ActiveConnections:  active,
		}

		// Apply filters
		if query.Username != "" && record.Username != query.Username {
			continue
		}
		if query.ContainerType != "" && record.ContainerType != query.ContainerType {
			continue
		}
		if query.InboundTag != "" && record.InboundTag != query.InboundTag {
			continue
		}
		if query.RuleKey != "" && record.RuleKey != query.RuleKey {
			continue
		}

		matchingRecords = append(matchingRecords, record)
		totalUplink += upload
		totalDownlink += download
	}

	// Build result
	result := TrafficQueryResult{
		Records:         matchingRecords,
		AggregatedStats: make(map[string]ForwardTrafficRecord),
		TotalUplink:     totalUplink,
		TotalDownlink:   totalDownlink,
	}

	// Apply group-by aggregation
	if query.GroupBy != "" {
		aggregated := make(map[string]ForwardTrafficRecord)

		for _, record := range matchingRecords {
			var groupKey string
			switch query.GroupBy {
			case "user":
				groupKey = record.Username
			case "container":
				groupKey = string(record.ContainerType)
			case "inbound":
				groupKey = string(record.ContainerType) + ":" + record.InboundTag
			case "rule":
				groupKey = record.RuleKey
			default:
				// Multiple group-by: "user,container" etc.
				groupKey = buildGroupKey(record, query.GroupBy)
			}

			if existing, ok := aggregated[groupKey]; ok {
				existing.UplinkBytes += record.UplinkBytes
				existing.DownlinkBytes += record.DownlinkBytes
				existing.ActiveConnections += record.ActiveConnections
				aggregated[groupKey] = existing
			} else {
				aggregated[groupKey] = record
			}
		}

		result.AggregatedStats = aggregated
	}

	return result
}

// buildGroupKey builds a group key from multiple dimensions.
func buildGroupKey(record ForwardTrafficRecord, groupBy string) string {
	dimensions := splitAndTrim(groupBy, ",")
	var parts []string
	for _, dim := range dimensions {
		switch dim {
		case "user":
			parts = append(parts, record.Username)
		case "container":
			parts = append(parts, string(record.ContainerType))
		case "inbound":
			parts = append(parts, record.InboundTag)
		case "rule":
			parts = append(parts, record.RuleKey)
		}
	}
	return joinWithSeparator(parts, "|")
}

// splitAndTrim splits a string by separator and trims whitespace.
func splitAndTrim(s, sep string) []string {
	var result []string
	for _, part := range strings.Split(s, sep) {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// joinWithSeparator joins strings with a separator.
func joinWithSeparator(parts []string, sep string) string {
	return strings.Join(parts, sep)
}

// UpdateRateLimit updates the rate limit for an existing rule.
// New connections will use the new limits; existing connections keep the old ones.
func (m *DefaultForwardManager) UpdateRateLimit(ruleKey string, uploadBPS, downloadBPS int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	mr, ok := m.rules[ruleKey]
	if !ok {
		return fmt.Errorf("forward_manager: rule %q not found", ruleKey)
	}

	// Update the rule config (for GetRule consistency)
	mr.rule.UploadBytesPerSec = uploadBPS
	mr.rule.DownloadBytesPerSec = downloadBPS

	// Note: existing relay connections keep the old limiter.
	// To apply new limits, the relay would need to be restarted.
	// This is a trade-off: we don't interrupt existing connections.
	// New connections will pick up the new rate when the relay is re-created.

	return nil
}

// SetUserBandwidthLimit sets the aggregate bandwidth limit for a user.
// This limit applies across all rules (inbounds) belonging to the same user.
// kind specifies "upload" or "download". bytesPerSec = 0 means unlimited (clears the limit).
// Setting a value immediately applies to all existing and new rules for this user.
func (m *DefaultForwardManager) SetUserBandwidthLimit(username string, kind BandwidthLimitKind, bytesPerSec int64) error {
	if username == "" {
		return fmt.Errorf("forward_manager: username must not be empty")
	}
	if err := ValidateBandwidthLimitKind(kind); err != nil {
		return err
	}
	if bytesPerSec < 0 {
		return fmt.Errorf("forward_manager: bandwidth limit must not be negative")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// If setting to 0 (unlimited), we need to mark as not set
	if bytesPerSec == 0 {
		lim, exists := m.userBandwidth[username]
		if !exists || lim == nil {
			// Nothing to clear, already doesn't exist
			return nil
		}
		// Update the "set" flag to false for this direction
		if kind == BandwidthUpload {
			lim.UpdateRates(0, lim.DownloadRate(), false, lim.IsDownloadSet())
		} else {
			lim.UpdateRates(lim.UploadRate(), 0, lim.IsUploadSet(), false)
		}
		return nil
	}

	// Create or update the limiter with bytesPerSec > 0
	lim, exists := m.userBandwidth[username]
	if !exists || lim == nil {
		// Create new limiter
		var uploadSet, downloadSet bool = false, false
		var uploadRate, downloadRate int64 = 0, 0
		if kind == BandwidthUpload {
			uploadRate = bytesPerSec
			uploadSet = true
		} else {
			downloadRate = bytesPerSec
			downloadSet = true
		}
		lim = newUserBandwidthLimiter(uploadRate, downloadRate, uploadSet, downloadSet)
		m.userBandwidth[username] = lim
	} else {
		// Update existing limiter
		if kind == BandwidthUpload {
			lim.UpdateRates(bytesPerSec, lim.DownloadRate(), true, lim.IsDownloadSet())
		} else {
			lim.UpdateRates(lim.UploadRate(), bytesPerSec, lim.IsUploadSet(), true)
		}
	}

	return nil
}

// GetUserBandwidthLimit returns the current bandwidth limit for a user.
// Returns ok=false if no limit has been explicitly set for this direction.
// Returns 0 with ok=true if the limit is explicitly set to unlimited (bytesPerSec == 0).
func (m *DefaultForwardManager) GetUserBandwidthLimit(username string, kind BandwidthLimitKind) (int64, bool) {
	if err := ValidateBandwidthLimitKind(kind); err != nil {
		return 0, false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	lim, ok := m.userBandwidth[username]
	if !ok || lim == nil {
		return 0, false
	}

	if kind == BandwidthUpload {
		if !lim.IsUploadSet() {
			return 0, false
		}
		return lim.UploadRate(), true
	}
	if !lim.IsDownloadSet() {
		return 0, false
	}
	return lim.DownloadRate(), true
}

// SetUserClientLimitConfig sets the client limit config for a user and, if
// the user already has a shared limiter attached to active rules, applies it
// in-place. Passing MaxClients <= 0 puts that limiter into passthrough mode
// without dropping the shared instance — the "stable reference" invariant —
// so a later call with MaxClients > 0 re-enables the limit without requiring
// rules to be re-added.
//
// When the user has no active rule yet, the limiter is NOT created here:
// AddRule creates it on demand, seeded from the stored config. This keeps
// the limiter object lifecycle tied to the presence of rules and avoids
// leaking standalone limiters for users that were synced in from the
// cluster but never had a local inbound.
func (m *DefaultForwardManager) SetUserClientLimitConfig(username string, config ClientLimitConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store the canonical config (used as the seed when AddRule later creates
	// a relay for this user).
	m.userClientLimitConfigs[username] = config

	if existingLimiter, exists := m.userClientLimiters[username]; exists {
		if limiter, ok := existingLimiter.(*remoteIPClientLimiter); ok {
			limiter.SetConfig(config)
		}
	}
	return nil
}

// DropUser releases all user-level resources attached to the given username:
// bandwidth limiter, client-limit limiter, and stored client-limit config.
//
// Usage contract: call this from a user-lifecycle terminator (RemoveUser /
// cluster tombstone) so that per-user maps don't accumulate across user
// churn. The typical flow is asynchronous — the rule teardown is kicked off
// by emitting UserEventRemove, and DropUser runs immediately after without
// waiting for container subscribers to finish. That is safe in practice:
//   - Active relays keep a direct pointer to the limiter objects, so dropping
//     them from the manager's maps does NOT cause use-after-free (GC holds
//     the objects as long as relays reference them). The limiters simply
//     become untracked, and Stop() tears them down shortly after.
//   - New rules for the same user will build fresh limiter instances on the
//     next AddRule. If a re-created user races rule teardown of the old
//     lifetime, there is a narrow window where old relays still rate-limit
//     against the old limiter and new relays use the new one (transient
//     "2x quota"). Accepted as the cost of running DropUser and rule
//     teardown in parallel rather than serialising them.
//
// Returns true if any state was released, false if no per-user state existed.
// Safe to call on unknown usernames.
func (m *DefaultForwardManager) DropUser(username string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, hadBW := m.userBandwidth[username]
	_, hadCL := m.userClientLimiters[username]
	_, hadCfg := m.userClientLimitConfigs[username]

	delete(m.userBandwidth, username)
	delete(m.userClientLimiters, username)
	delete(m.userClientLimitConfigs, username)

	return hadBW || hadCL || hadCfg
}

// GetUserClientLimitConfig returns the current client limit config for a user.
// Returns ok=false if no client limit is set.
func (m *DefaultForwardManager) GetUserClientLimitConfig(username string) (ClientLimitConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// First check stored config
	if config, ok := m.userClientLimitConfigs[username]; ok {
		return config, true
	}

	// Fallback to limiter's config if exists
	limiter, ok := m.userClientLimiters[username]
	if !ok {
		return ClientLimitConfig{}, false
	}

	if impl, ok := limiter.(*remoteIPClientLimiter); ok {
		impl.mu.Lock()
		config := impl.config
		impl.mu.Unlock()
		return config, true
	}

	return ClientLimitConfig{}, false
}

// AllocatePort implements ForwardManager.
//
// This delegates to the underlying port allocator. It DOES NOT create a
// relay, a ForwardRule, or any traffic-plane state — callers that need
// traffic forwarding must still call AddRule separately.
func (m *DefaultForwardManager) AllocatePort() (uint32, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, fmt.Errorf("forward_manager: closed")
	}
	return m.allocator.Allocate()
}

// ReleasePort implements ForwardManager.
//
// Companion to AllocatePort. Idempotent; does not affect relays or rules.
func (m *DefaultForwardManager) ReleasePort(port uint32) {
	m.allocator.Release(port)
}

// Close stops all relays and releases all ports.
func (m *DefaultForwardManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	for key, mr := range m.rules {
		mr.relay.Stop()
		m.allocator.Release(mr.rule.ListenPort)
		m.traffic.Remove(key)
	}
	m.rules = make(map[string]*managedRule)
	m.closed = true

	return nil
}

// Verify interface compliance at compile time.
var _ ForwardManager = (*DefaultForwardManager)(nil)
