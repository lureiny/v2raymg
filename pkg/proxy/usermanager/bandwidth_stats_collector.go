package usermanager

import (
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// BandwidthStatsCollector wraps UserManager to implement the
// pkg/rpc/server.BandwidthStatsCollector interface.
// Each DrainStats() call returns all users' delta traffic since the last call
// and resets the delta counters (reset=true), matching the Prometheus scrape pattern.
type BandwidthStatsCollector struct {
	mgr *UserManager
}

// NewBandwidthStatsCollector creates a collector backed by the given UserManager.
func NewBandwidthStatsCollector(mgr *UserManager) *BandwidthStatsCollector {
	return &BandwidthStatsCollector{mgr: mgr}
}

// DrainStats returns all users' delta traffic as proto.Stats and resets the deltas.
// Users with no traffic (zero delta) are included with 0 values.
// Satisfies pkg/rpc/server.BandwidthStatsCollector.
func (c *BandwidthStatsCollector) DrainStats() []*proto.Stats {
	if c.mgr == nil {
		return nil
	}

	// Get all active users
	users := c.mgr.ListUsers()
	if len(users) == 0 {
		return nil
	}

	// Get delta traffic per user-inbound
	deltas := c.mgr.GetAllDeltaTraffic(true /* reset */)

	// Build lookup: username -> []UserTrafficStats
	deltaMap := make(map[string][]UserTrafficStats)
	for _, d := range deltas {
		deltaMap[d.Username] = append(deltaMap[d.Username], d)
	}

	// Build result: all users, including those with zero traffic
	stats := make([]*proto.Stats, 0, len(users))
	for _, u := range users {
		userDeltas, hasDelta := deltaMap[u.Username]
		if !hasDelta {
			// User has no traffic at all, return a single 0-value entry
			stats = append(stats, &proto.Stats{
				Name:      u.Username,
				Type:      "user",
				Downlink:  0,
				Uplink:    0,
				Source:    "",
				Proxy:     "",
				Protocol:  "",
				Container: "",
			})
			continue
		}
		// User has traffic entries, return each inbound separately
		for _, d := range userDeltas {
			stats = append(stats, &proto.Stats{
				Name:      d.Username,
				Type:      "user",
				Downlink:  d.DeltaDownlink,
				Uplink:    d.DeltaUplink,
				Source:    string(d.ContainerType) + ":" + d.InboundTag,
				Proxy:     d.InboundTag,
				Protocol:  string(d.Protocol),
				Container: string(d.ContainerType),
			})
		}
	}
	return stats
}
