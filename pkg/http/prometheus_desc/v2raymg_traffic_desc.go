package prometheusdesc

import (
	"context"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/prometheus/client_golang/prometheus"
)

type v2raymgTrafficDesc struct {
	traffic *prometheus.Desc

	stats []*proto.Stats
	mutex sync.RWMutex
}

func NewV2raymgTrafficDesc() *v2raymgTrafficDesc {
	return &v2raymgTrafficDesc{
		traffic: prometheus.NewDesc(
			"v2raymg_traffic",
			"v2raymg traffic per user per inbound",
			[]string{"node", "name", "type", "direction", "protocol", "container"},
			nil,
		),
	}
}

func (d *v2raymgTrafficDesc) Describe(ch chan<- *prometheus.Desc) {
	ch <- d.traffic
}

func (d *v2raymgTrafficDesc) Collect(ch chan<- prometheus.Metric) {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	currentTime := time.Now()
	for _, stat := range d.stats {
		labels := []string{
			stat.GetSource(),       // node = node name from RPC response key
			stat.GetName(),         // name
			stat.GetType(),         // type
			"downlink",             // direction
			stat.GetProtocol(),     // protocol
			stat.GetContainer(),    // container
		}
		ch <- prometheus.NewMetricWithTimestamp(
			currentTime.UTC(),
			prometheus.MustNewConstMetric(
				d.traffic, prometheus.GaugeValue,
				float64(stat.GetDownlink()), labels...,
			),
		)

		labels[3] = "uplink"
		ch <- prometheus.NewMetricWithTimestamp(
			currentTime.UTC(),
			prometheus.MustNewConstMetric(
				d.traffic, prometheus.GaugeValue,
				float64(stat.GetUplink()), labels...,
			),
		)
	}
}

// Update fetches stats from all nodes via RPC.
// The node name is taken directly from the RPC response key (node name).
func (d *v2raymgTrafficDesc) Update(ctx context.Context, rpcClient *client.EndNodeClient, token string) error {
	succList, _, _ := rpcClient.ReqToMultiEndNodeServer(ctx,
		client.GetBandWidthStatsReqType,
		&proto.GetBandwidthStatsReq{},
		token,
	)

	stats := []*proto.Stats{}

	for nodeName, v := range succList {
		nodeStats, _ := v.([]*proto.Stats)
		for _, stat := range nodeStats {
			// Set Source to node name for Prometheus node label
			stat.Source = nodeName
		}
		stats = append(stats, nodeStats...)
	}

	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.stats = stats
	return nil
}
