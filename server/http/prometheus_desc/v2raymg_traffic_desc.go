package prometheusdesc

import (
	"context"
	"sync"
	"time"

	client "github.com/lureiny/v2raymg/client/rpc"
	globalCluster "github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/server/rpc/proto"
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
			"v2ray/xray traffic",
			[]string{"node", "name", "type", "direction", "proxy"},
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
		labels := []string{stat.GetSource(),
			stat.GetName(), stat.GetType(), "downlink", stat.GetProxy()}
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

// Update ...
func (d *v2raymgTrafficDesc) Update(ctx context.Context, rpcClient *client.EndNodeClient) error {
	succList, _, _ := rpcClient.ReqToMultiEndNodeServer(ctx,
		client.GetBandWidthStatsReqType,
		&proto.GetBandwidthStatsReq{},
		globalCluster.GetClusterToken(),
	)
	stats := []*proto.Stats{}
	for _, v := range succList {
		s, _ := v.([]*proto.Stats)
		stats = append(stats, s...)
	}
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.stats = stats
	return nil
}
