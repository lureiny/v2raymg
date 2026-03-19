package server

import (
	io_prometheus_client "github.com/prometheus/client_model/go"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

// BandwidthStatsCollector provides access to per-user bandwidth statistics.
// Implementations are expected to drain the stats (clear after read) to avoid double-counting.
type BandwidthStatsCollector interface {
	// DrainStats returns all accumulated bandwidth stats and resets the internal store.
	DrainStats() []*proto.Stats
}

// PingCollector provides latency metrics for peer nodes.
type PingCollector interface {
	// GetPingResults returns the latest ping results keyed by node name.
	GetPingResults() []*proto.PingResult
	// StopPing disables periodic ping checks.
	StopPing()
}

// NodeMetricCollector collects system-level Prometheus metrics.
type NodeMetricCollector interface {
	// Collect gathers and returns all metric families.
	Collect() ([]*io_prometheus_client.MetricFamily, error)
}
