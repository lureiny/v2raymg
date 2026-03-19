package usermanager_test

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/usermanager"
	pkgrpcserver "github.com/lureiny/v2raymg/pkg/rpc/server"
)

// TestBandwidthStatsCollectorImplementsInterface verifies compile-time
// compatibility: *usermanager.BandwidthStatsCollector satisfies
// pkgrpcserver.BandwidthStatsCollector.
func TestBandwidthStatsCollectorImplementsInterface(t *testing.T) {
	// compile-time assertion
	var _ pkgrpcserver.BandwidthStatsCollector = (*usermanager.BandwidthStatsCollector)(nil)
}
