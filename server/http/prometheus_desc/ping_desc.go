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

type pingDesc struct {
	maxDesc    *prometheus.Desc
	minDesc    *prometheus.Desc
	stDevDesc  *prometheus.Desc
	avgDesc    *prometheus.Desc
	lossDesc   *prometheus.Desc
	latestDesc *prometheus.Desc

	metrics []*proto.PingMetric
	mutex   sync.RWMutex
}

var pingLabels []string = []string{
	"node", "host", // v2raymg node info
	"geo", "isp", "ping_ip", // ping dst node info
	"ping_checker",
}

func NewPingDesc() *pingDesc {
	return &pingDesc{
		maxDesc: prometheus.NewDesc(
			"ping_max",
			"ping max value",
			pingLabels,
			nil,
		),
		minDesc: prometheus.NewDesc(
			"ping_min",
			"ping min value",
			pingLabels,
			nil,
		),
		stDevDesc: prometheus.NewDesc(
			"ping_st_dev",
			"ping st dev value",
			pingLabels,
			nil,
		),
		avgDesc: prometheus.NewDesc(
			"ping_avg",
			"ping avg value",
			pingLabels,
			nil,
		),
		lossDesc: prometheus.NewDesc(
			"ping_loss",
			"ping loss value",
			pingLabels,
			nil,
		),
		latestDesc: prometheus.NewDesc(
			"ping_latest",
			"laste ping result",
			pingLabels,
			nil,
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
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	currentTime := time.Now()
	for _, m := range d.metrics {
		for _, r := range m.GetResults() {
			labels := []string{m.GetSource(),
				m.GetHost(), r.GetGeo(), r.GetIsp(), r.GetPingIp(), r.GetPingChecker()}

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

// Update ...
func (d *pingDesc) Update(ctx context.Context, rpcClient *client.EndNodeClient) error {
	// ping metrics
	metricSuccList, _, _ := rpcClient.ReqToMultiEndNodeServer(ctx,
		client.GetPingMetricType,
		&proto.GetPingMetricReq{},
		globalCluster.GetClusterToken(),
	)
	metrics := []*proto.PingMetric{}
	for _, v := range metricSuccList {
		m, _ := v.(*proto.PingMetric)
		metrics = append(metrics, m)
	}
	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.metrics = metrics
	return nil
}
