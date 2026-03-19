// Package collecter provides metric collection utilities for v2raymg nodes.
package collecter

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	prometCollector "github.com/prometheus/node_exporter/collector"
)

// NodeCollector gathers system-level Prometheus metrics via node_exporter.
type NodeCollector struct {
	register *prometheus.Registry
}

var (
	defaultNodeCollector *NodeCollector
	nodeCollectorOnce    sync.Once
)

// NewNodeCollector creates a new NodeCollector backed by prometheus node_exporter.
func NewNodeCollector() *NodeCollector {
	nc, err := prometCollector.NewNodeCollector(slog.Default())
	if err != nil {
		panic(fmt.Sprintf("init node collector fail, err: %v", err))
	}
	reg := prometheus.NewRegistry()
	reg.Register(nc)
	return &NodeCollector{register: reg}
}

// Collect gathers and returns all metric families.
func (nc *NodeCollector) Collect() ([]*io_prometheus_client.MetricFamily, error) {
	return nc.register.Gather()
}

// DefaultNodeCollector returns the package-level singleton NodeCollector.
func DefaultNodeCollector() *NodeCollector {
	nodeCollectorOnce.Do(func() {
		defaultNodeCollector = NewNodeCollector()
	})
	return defaultNodeCollector
}
