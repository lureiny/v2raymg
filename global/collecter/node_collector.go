package collecter

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	prometCollector "github.com/prometheus/node_exporter/collector"
)

type NodeCollector struct {
	register *prometheus.Registry
}

var defaultNodeCollector *NodeCollector = nil
var once = sync.Once{}

// NewNodeCollector ...
func NewNodeCollector() *NodeCollector {
	nc, err := prometCollector.NewNodeCollector(slog.Default())
	if err != nil {
		panic(fmt.Sprintf("init node collector fail, err: %v", err))
	}
	reg := prometheus.NewRegistry()
	reg.Register(nc)

	return &NodeCollector{
		register: reg,
	}
}

// Collect ...
func (nc *NodeCollector) Collect() ([]*io_prometheus_client.MetricFamily, error) {
	return nc.register.Gather()
}

// DefauleNodeCollector ...
func DefauleNodeCollector() *NodeCollector {
	once.Do(func() {
		defaultNodeCollector = NewNodeCollector()
	})
	return defaultNodeCollector
}
