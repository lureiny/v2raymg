package collecter

import (
	"context"
	"fmt"
	"sync"

	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/collecter/ping"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
)

type pingResults map[string]*ping.PingResult // key: node name

// PingCollector manages PingCheckers and aggregates their results.
// It implements the PingCollector interface expected by pkg/rpc/server.
type PingCollector struct {
	host            string
	pingCheckers    map[string]ping.PingChecker
	pingResultMap   map[string]pingResults
	mu              sync.RWMutex
	enablePingPe    bool
	enableICMPPing  bool
}

// PingCollectorConfig configures which checkers to enable and the local host.
type PingCollectorConfig struct {
	Host           string
	EnablePingPe   bool
	EnableICMPPing bool
}

// NewPingCollector creates and starts a PingCollector.
func NewPingCollector(cfg PingCollectorConfig) *PingCollector {
	pc := &PingCollector{
		host:           cfg.Host,
		pingCheckers:   make(map[string]ping.PingChecker),
		pingResultMap:  make(map[string]pingResults),
		enablePingPe:   cfg.EnablePingPe,
		enableICMPPing: cfg.EnableICMPPing,
	}
	pc.init()
	return pc
}

func (pc *PingCollector) init() {
	// Add default ping nodes
	nodeManager := ping.NewPingNodeManager()
	allNodes := append(append([]*ping.PingNodeInfo{}, telecomPingNodes...), mobilePingNodes...)
	allNodes = append(allNodes, unicomPingNodes...)
	for _, node := range allNodes {
		if err := nodeManager.AddNode(node); err != nil {
			log.Error("add ping node failed", "node", node, "err", err)
		}
	}

	if pc.enablePingPe {
		checker := ping.NewPingPeChekcer()
		pc.pingCheckers[checker.Name()] = checker
		pc.startChecker(checker, nodeManager)
	}
	if pc.enableICMPPing {
		checker := ping.NewIcmpPingChecker()
		pc.pingCheckers[checker.Name()] = checker
		pc.startChecker(checker, nodeManager)
	}
}

func (pc *PingCollector) startChecker(checker ping.PingChecker, nodeManager *ping.PingNodeManager) {
	checker.Init(
		ping.WithHost(pc.host),
		ping.WithGetNodeFunc(func() []*ping.PingNodeInfo {
			return nodeManager.ListNode()
		}),
	)
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
}
