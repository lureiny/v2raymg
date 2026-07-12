package collecter

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/collecter/ping"
	"github.com/lureiny/v2raymg/pkg/proxy/appconfig"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type pingResults map[string]*ping.PingResult // key: node name

// PingCollector manages PingCheckers and aggregates their results.
// It implements the PingCollector interface expected by pkg/rpc/server.
type PingCollector struct {
	host              string
	pingCheckers      map[string]ping.PingChecker
	pingResultMap     map[string]pingResults
	mu                sync.RWMutex
	enableICMPPing    bool
	enableTCPPing     bool
	icmpPingInterval  int
	icmpPingTimeout   int
	tcpPingInterval   int
	tcpPingTimeout    int
	nodeManager       ping.NodeManager
}

// PingCollectorConfig configures which checkers to enable and the local host.
type PingCollectorConfig struct {
	Host             string
	EnableICMPPing   bool
	EnableTCPPing    bool
	ICMPPingInterval int // seconds, default 5
	ICMPPingTimeout  int // seconds, default 1
	TCPPingInterval  int // seconds, default 5
	TCPPingTimeout   int // seconds, default 1
	NodeSources      []appconfig.PingNodeSource
}

// NewPingCollector creates and starts a PingCollector.
func NewPingCollector(cfg PingCollectorConfig) *PingCollector {
	pc := &PingCollector{
		host:             cfg.Host,
		pingCheckers:     make(map[string]ping.PingChecker),
		pingResultMap:    make(map[string]pingResults),
		enableICMPPing:   cfg.EnableICMPPing,
		enableTCPPing:    cfg.EnableTCPPing,
		icmpPingInterval: cfg.ICMPPingInterval,
		icmpPingTimeout:  cfg.ICMPPingTimeout,
		tcpPingInterval:  cfg.TCPPingInterval,
		tcpPingTimeout:   cfg.TCPPingTimeout,
	}

	// Initialize NodeManager
	nm := ping.NewNodeManager()
	if err := nm.LoadFromSources(cfg.NodeSources); err != nil {
		log.Error("load ping nodes failed", "err", err)
	}

	// Register callback for node changes (cleanup stale results)
	nm.OnChange(func() {
		pc.cleanupStaleResults(nm.ListAll())
	})

	pc.nodeManager = nm
	pc.init()
	return pc
}

func (pc *PingCollector) init() {
	if pc.enableICMPPing {
		checker := ping.NewIcmpPingChecker()
		pc.pingCheckers[checker.Name()] = checker
		opts := []ping.OptionFunc{
			ping.WithNodeManager(pc.nodeManager),
		}
		if pc.icmpPingInterval > 0 {
			opts = append(opts, ping.WithICMPPingInterval(pc.icmpPingInterval))
		}
		if pc.icmpPingTimeout > 0 {
			opts = append(opts, ping.WithICMPPingTimeout(pc.icmpPingTimeout))
		}
		checker.Init(opts...)
		pc.startChecker(checker)
	}
	if pc.enableTCPPing {
		checker := ping.NewTcpPingChecker()
		pc.pingCheckers[checker.Name()] = checker
		opts := []ping.OptionFunc{
			ping.WithNodeManager(pc.nodeManager),
		}
		if pc.tcpPingInterval > 0 {
			opts = append(opts, ping.WithTCPPingInterval(pc.tcpPingInterval))
		}
		if pc.tcpPingTimeout > 0 {
			opts = append(opts, ping.WithTCPPingTimeout(pc.tcpPingTimeout))
		}
		checker.Init(opts...)
		pc.startChecker(checker)
	}
}

func (pc *PingCollector) startChecker(checker ping.PingChecker) {
	ctx, _ := context.WithCancel(checker.GetContext())
	go func(ctx context.Context, ch <-chan *ping.PingResult, name string) {
		for {
			select {
			case <-ctx.Done():
				log.Info("ping checker stopped", "name", name)
				return
			case result := <-ch:
				nodeName := fmt.Sprintf("%s_%s_%s", result.Geo, result.ISP, result.PingIp)
				pc.mu.Lock()
				if _, ok := pc.pingResultMap[name]; !ok {
					pc.pingResultMap[name] = make(pingResults)
				}
				if existing, ok := pc.pingResultMap[name][nodeName]; ok {
					existing.Update(result.GetLatestDelay())
				} else {
					pc.pingResultMap[name][nodeName] = result
				}
				pc.mu.Unlock()
			}
		}
	}(ctx, checker.GetChan(), checker.Name())
	checker.Start()
}

// cleanupStaleResults removes results for nodes that no longer exist.
//
// A result is kept if either:
//   - its host (extracted from the node name, with the TCP host:port stripped)
//     matches a current node's Host — the exact match for IP-configured nodes; or
//   - some current node in the same geo/isp is configured with a domain (a
//     non-IP Host). ICMP results for domain nodes are keyed by the RESOLVED IP,
//     which never equals the configured domain, so a plain host comparison
//     wrongly deleted (and reset the 100-sample ring buffer of) every
//     domain-configured node on each reload. This geo/isp fallback keeps them.
//
// Residual: in a geo/isp group that mixes a domain node with a removed IP node,
// the removed IP node's result is also preserved (a small stale-entry leak) —
// the acceptable trade for not deleting valid domain-node results.
func (pc *PingCollector) cleanupStaleResults(currentNodes []*ping.PingNodeInfo) {
	hostSet := make(map[string]struct{})
	geoISPHasDomain := make(map[string]struct{})
	for _, n := range currentNodes {
		hostSet[n.Host] = struct{}{}
		if net.ParseIP(n.Host) == nil { // domain-configured node
			geoISPHasDomain[n.Geo+"\x00"+n.ISP] = struct{}{}
		}
	}

	pc.mu.Lock()
	defer pc.mu.Unlock()

	for checkerName, results := range pc.pingResultMap {
		for nodeName, r := range results {
			host := extractHostFromNodeName(nodeName, checkerName)
			if host == "" {
				continue
			}
			if _, exact := hostSet[host]; exact {
				continue
			}
			if _, domainNode := geoISPHasDomain[r.Geo+"\x00"+r.ISP]; domainNode {
				continue
			}
			delete(results, nodeName)
			log.Info("removed stale ping result", "checker", checkerName, "node", nodeName)
		}
	}
}

// extractHostFromNodeName extracts the host from a node name.
// For ICMP: nodeName format is "{geo}_{isp}_{ip}"
// For TCP:  nodeName format is "{geo}_{isp}_{host:port}"
func extractHostFromNodeName(nodeName, checkerName string) string {
	parts := strings.Split(nodeName, "_")
	if len(parts) < 3 {
		return ""
	}

	lastPart := parts[len(parts)-1]

	// For TCP checker, the last part is "host:port", extract host
	if checkerName == "tcp_ping" {
		if idx := strings.LastIndex(lastPart, ":"); idx > 0 {
			return lastPart[:idx]
		}
	}

	return lastPart
}

// GetPingResults returns all current ping results as proto.PingResult slice.
// Implements pkg/rpc/server.PingCollector interface.
func (pc *PingCollector) GetPingResults() []*proto.PingResult {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	var results []*proto.PingResult
	for _, checkerResults := range pc.pingResultMap {
		for _, r := range checkerResults {
			results = append(results, r.ConvertToProtoPingResult())
		}
	}
	return results
}

// StopPing stops all ping checkers.
// Implements pkg/rpc/server.PingCollector interface.
func (pc *PingCollector) StopPing() {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	for _, checker := range pc.pingCheckers {
		checker.Stop()
	}
	if pc.nodeManager != nil {
		pc.nodeManager.Stop()
	}
}
