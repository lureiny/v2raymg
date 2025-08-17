package prometheusdesc

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// GetPrometheusValueType ...
func GetPrometheusValueType(t dto.MetricType) prometheus.ValueType {
	switch t {
	case dto.MetricType_COUNTER:
		return prometheus.CounterValue
	case dto.MetricType_GAUGE:
		return prometheus.GaugeValue
	default:
		return prometheus.UntypedValue
	}
}

// GetMetricValue ...
func GetMetricValue(t dto.MetricType, metric *dto.Metric) float64 {
	switch t {
	case dto.MetricType_COUNTER:
		return metric.Counter.GetValue()
	case dto.MetricType_GAUGE:
		return metric.Gauge.GetValue()
	case dto.MetricType_UNTYPED:
		return metric.Untyped.GetValue()
	case dto.MetricType_SUMMARY, dto.MetricType_HISTOGRAM:
		// 暂时不支持summary, histogram
		return 0
	default:
		return 0
	}
}

// GenerateLabel ...
func GenerateLabel(metric *dto.Metric, exLabelParis ...*dto.LabelPair) ([]string, []string) {
	labelKeys := []string{}
	labelValues := []string{}
	labelParis := append(metric.GetLabel(), exLabelParis...)
	for _, labelPair := range labelParis {
		labelKeys = append(labelKeys, *labelPair.Name)
		labelValues = append(labelValues, *labelPair.Value)
	}
	return labelKeys, labelValues
}

// GenerateExFaamilyLabelParis ...
func GenerateExFaamilyLabelParis(exMetricFamily *ExMetricFamily) []*dto.LabelPair {
	hostLabelName := "host"
	hostLabelValue := exMetricFamily.host
	hostLabelPair := &dto.LabelPair{
		Name:  &hostLabelName,
		Value: &hostLabelValue,
	}

	sourceLabelName := "source"
	sourceLabelValue := exMetricFamily.source
	sourceLabelPair := &dto.LabelPair{
		Name:  &sourceLabelName,
		Value: &sourceLabelValue,
	}

	return []*dto.LabelPair{hostLabelPair, sourceLabelPair}
}
