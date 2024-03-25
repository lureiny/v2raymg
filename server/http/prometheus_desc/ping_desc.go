package prometheusdesc

import (
	"sync"
	"time"

	"github.com/lureiny/v2raymg/server/rpc/proto"
	"github.com/prometheus/client_golang/prometheus"
)

type pingDesc struct {
	maxDesc    *prometheus.Desc
	minDesc    *prometheus.Desc
	stDevDesc  *prometheus.Desc
	avgDesc    *prometheus.Desc
	lossDesc   *prometheus.Desc
	latestDesc *prometheus.Desc

	Metrics []*proto.PingMetric
	Mutex   sync.Mutex
}

var pingLabels []string = []string{
	"node", "host", "geo", "isp",
}

func NewPingDesc() *pingDesc {
	return &pingDesc{
		maxDesc: prometheus.NewDesc(
			"ping_max",
			"ping max value",
			pingLabels,
			prometheus.Labels{},
		),
		minDesc: prometheus.NewDesc(
			"ping_min",
			"ping min value",
			pingLabels,
			prometheus.Labels{},
		),
		stDevDesc: prometheus.NewDesc(
			"ping_st_dev",
			"ping st dev value",
			pingLabels,
			prometheus.Labels{},
		),
		avgDesc: prometheus.NewDesc(
			"ping_avg",
			"ping avg value",
			pingLabels,
			prometheus.Labels{},
		),
		lossDesc: prometheus.NewDesc(
			"ping_loss",
			"ping loss value",
			pingLabels,
			prometheus.Labels{},
		),
		latestDesc: prometheus.NewDesc(
			"ping_latest",
			"laste ping result",
			pingLabels,
			prometheus.Labels{},
		),
	}
}

func (d *pingDesc) Describe(ch chan<- *prometheus.Desc) {
	ch <- d.maxDesc
	ch <- d.minDesc
	ch <- d.avgDesc
	ch <- d.lossDesc
	ch <- d.stDevDesc
	ch <- d.latestDesc
}

func (d *pingDesc) Collect(ch chan<- prometheus.Metric) {
	currentTime := time.Now()
	for _, m := range d.Metrics {
		for _, r := range m.GetResults() {
			labels := []string{m.GetSource(),
				m.GetHost(), r.GetGeo(), r.GetIsp()}

			// max
			ch <- prometheus.NewMetricWithTimestamp(
				currentTime.UTC(),
				prometheus.MustNewConstMetric(
					d.maxDesc, prometheus.GaugeValue,
					float64(r.GetMaxDelay()), labels...,
				),
			)

			// min
			ch <- prometheus.NewMetricWithTimestamp(
				currentTime.UTC(),
				prometheus.MustNewConstMetric(
					d.minDesc, prometheus.GaugeValue,
					float64(r.GetMinDelay()), labels...,
				),
			)

			// avg
			ch <- prometheus.NewMetricWithTimestamp(
				currentTime.UTC(),
				prometheus.MustNewConstMetric(
					d.avgDesc, prometheus.GaugeValue,
					float64(r.GetAvgDelay()), labels...,
				),
			)

			// st dev
			ch <- prometheus.NewMetricWithTimestamp(
				currentTime.UTC(),
				prometheus.MustNewConstMetric(
					d.stDevDesc, prometheus.GaugeValue,
					float64(r.GetStDevDelay()), labels...,
				),
			)

			// loss
			ch <- prometheus.NewMetricWithTimestamp(
				currentTime.UTC(),
				prometheus.MustNewConstMetric(
					d.lossDesc, prometheus.GaugeValue,
					float64(r.GetLoss()), labels...,
				),
			)

			// latest delay
			ch <- prometheus.NewMetricWithTimestamp(
				currentTime.UTC(),
				prometheus.MustNewConstMetric(
					d.latestDesc, prometheus.GaugeValue,
					float64(r.GetLatestDelay()), labels...,
				),
			)
		}

	}
}
