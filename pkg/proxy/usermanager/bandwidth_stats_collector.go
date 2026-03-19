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
// Satisfies pkg/rpc/server.BandwidthStatsCollector.
func (c *BandwidthStatsCollector) DrainStats() []*proto.Stats {
	if c.mgr == nil {
		return nil
	}
	deltas := c.mgr.GetAllDeltaTraffic(true /* reset */)
	stats := make([]*proto.Stats, 0, len(deltas))
	for _, d := range deltas {
		if d.DeltaUplink == 0 && d.DeltaDownlink == 0 {
			continue
		}
		stats = append(stats, &proto.Stats{
			Name:     d.Username,
			Type:     "user",
			Downlink: d.DeltaDownlink,
			Uplink:   d.DeltaUplink,
			Source:   string(d.ContainerType) + ":" + d.InboundTag,
			Proxy:    d.InboundTag,
		})
	}
	return stats
}
