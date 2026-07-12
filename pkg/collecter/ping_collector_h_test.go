package collecter

import (
	"testing"

	"github.com/lureiny/v2raymg/pkg/collecter/ping"
)

func newTestCollector(m map[string]pingResults) *PingCollector {
	return &PingCollector{pingResultMap: m}
}

// TestCleanupStaleResults_KeepsDomainICMPNode covers finding #2: a domain-
// configured ICMP node's result is keyed by its RESOLVED IP, which never equals
// the configured domain — cleanup must keep it (before the fix it was deleted,
// resetting the 100-sample ring buffer, on every reload).
func TestCleanupStaleResults_KeepsDomainICMPNode(t *testing.T) {
	r := ping.NewPingResult("cn", "ct", "1.2.3.4", "icmp_ping")
	for i := 0; i < 50; i++ {
		r.Update(10) // advance the ring buffer
	}
	pc := newTestCollector(map[string]pingResults{
		"icmp_ping": {"cn_ct_1.2.3.4": r},
	})

	pc.cleanupStaleResults([]*ping.PingNodeInfo{
		{Host: "a.example.com", Geo: "cn", ISP: "ct"}, // domain node
	})

	got, ok := pc.pingResultMap["icmp_ping"]["cn_ct_1.2.3.4"]
	if !ok {
		t.Fatal("domain-node ICMP result was wrongly deleted")
	}
	if got.GetIndex() != 50 {
		t.Fatalf("ring buffer was reset: GetIndex=%d, want 50", got.GetIndex())
	}
}

// TestCleanupStaleResults_KeepsTCPIPNode guards against the port-stripping
// regression: a TCP node's result is keyed "ip:port" and must still match a
// current IP node.
func TestCleanupStaleResults_KeepsTCPIPNode(t *testing.T) {
	pc := newTestCollector(map[string]pingResults{
		"tcp_ping": {"us_att_1.2.3.4:80": ping.NewPingResult("us", "att", "1.2.3.4:80", "tcp_ping")},
	})

	pc.cleanupStaleResults([]*ping.PingNodeInfo{
		{Host: "1.2.3.4", Geo: "us", ISP: "att", Port: 80},
	})

	if _, ok := pc.pingResultMap["tcp_ping"]["us_att_1.2.3.4:80"]; !ok {
		t.Fatal("TCP IP-node result was wrongly deleted (port-stripping regression)")
	}
}

// TestCleanupStaleResults_DeletesRemovedIPNode confirms genuinely removed IP
// nodes are still cleaned up.
func TestCleanupStaleResults_DeletesRemovedIPNode(t *testing.T) {
	pc := newTestCollector(map[string]pingResults{
		"icmp_ping": {"us_att_5.6.7.8": ping.NewPingResult("us", "att", "5.6.7.8", "icmp_ping")},
	})

	// Current node set no longer contains 5.6.7.8, and no domain node in us/att.
	pc.cleanupStaleResults([]*ping.PingNodeInfo{
		{Host: "1.2.3.4", Geo: "cn", ISP: "ct"},
	})

	if _, ok := pc.pingResultMap["icmp_ping"]["us_att_5.6.7.8"]; ok {
		t.Fatal("removed IP-node result should have been cleaned up")
	}
}
