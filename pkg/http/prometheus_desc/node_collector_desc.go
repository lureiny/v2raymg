package prometheusdesc

import (
	"context"
	"sync"
	"time"

	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	pb "google.golang.org/protobuf/proto"
)

type ExMetricFamily struct {
	*dto.MetricFamily
	host   string
	source string
}

type nodeMetricDesc struct {
	mutex sync.RWMutex

	nodeMetrics []*proto.NodeMetrics
}

// NewNodeMetricDesc ...
func NewNodeMetricDesc() *nodeMetricDesc {
	return &nodeMetricDesc{}
}

// Describe ...
func (nmc *nodeMetricDesc) Describe(ch chan<- *prometheus.Desc) {}

// Collect ....
func (nmc *nodeMetricDesc) Collect(ch chan<- prometheus.Metric) {
	nmc.mutex.RLock()
	defer nmc.mutex.RUnlock()

	currentTime := time.Now()

	// 反序列化成MetricFamily
	exMetricFamilies := []*ExMetricFamily{}
	for _, nm := range nmc.nodeMetrics {
		for _, snf := range nm.GetSerializedMetrics() {
			metricFaimily := &dto.MetricFamily{}
			pb.Unmarshal(snf, metricFaimily)
			exMetricFamilies = append(exMetricFamilies, &ExMetricFamily{
				MetricFamily: metricFaimily,
				host:         nm.GetHost(),
				source:       nm.GetSource(),
			})
		}
	}

	for _, exMetricFamily := range exMetricFamilies {
		for _, metric := range exMetricFamily.GetMetric() {
			descLabels, labelsValues := GenerateLabel(metric, GenerateExFaamilyLabelParis(exMetricFamily)...)
			desc := prometheus.NewDesc(
				exMetricFamily.GetName(),
				exMetricFamily.GetHelp(),
				descLabels,
				nil,
			)

			ch <- prometheus.NewMetricWithTimestamp(
				currentTime.UTC(),
				prometheus.MustNewConstMetric(
					desc, GetPrometheusValueType(exMetricFamily.GetType()),
					float64(GetMetricValue(exMetricFamily.GetType(), metric)), labelsValues...,
				),
			)
		}
	}
}

// Update ...
func (nmc *nodeMetricDesc) Update(ctx context.Context, rpcClient *client.EndNodeClient, token string) error {
	succList, _, _ := rpcClient.ReqToMultiEndNodeServer(ctx,
		client.GetNodeMetricType,
		&proto.GetNodeMetricReq{},
		token,
	)
	nodeMetrics := []*proto.NodeMetrics{}
	for _, v := range succList {
		nms, _ := v.(*proto.NodeMetrics)
		nodeMetrics = append(nodeMetrics, nms)
	}

	nmc.mutex.Lock()
	defer nmc.mutex.Unlock()

	nmc.nodeMetrics = nodeMetrics
	return nil
}
