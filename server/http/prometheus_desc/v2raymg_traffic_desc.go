package prometheusdesc

import (
	"sync"
	"time"

	"github.com/lureiny/v2raymg/server/rpc/proto"
	"github.com/prometheus/client_golang/prometheus"
)

type v2raymgTrafficDesc struct {
	traffic *prometheus.Desc

	Stats []*proto.Stats
	Mutex sync.Mutex
}

func NewV2raymgTrafficDesc() *v2raymgTrafficDesc {
	return &v2raymgTrafficDesc{
		traffic: prometheus.NewDesc(
			"v2raymg_traffic",
			"v2ray/xray traffic",
			[]string{"node", "name", "type", "direction", "proxy"},
			prometheus.Labels{},
		),
	}
}

func (d *v2raymgTrafficDesc) Describe(ch chan<- *prometheus.Desc) {
	ch <- d.traffic
}

func (d *v2raymgTrafficDesc) Collect(ch chan<- prometheus.Metric) {
	currentTime := time.Now()
	for _, stat := range d.Stats {
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
