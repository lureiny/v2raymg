package server

import (
	context "context"
	"fmt"

	"github.com/lureiny/v2raymg/pkg/rpc/proto"
	pb "google.golang.org/protobuf/proto"
)

func (s *EndNodeServer) SetPingCheck(ctx context.Context, setPingCheckReq *proto.SetPingCheckReq) (*proto.SetPingCheckRsp, error) {
	setPingCheckRsp := &proto.SetPingCheckRsp{Code: 0}
	if !setPingCheckReq.GetEnablePingCheck() && s.pingCollector != nil {
		s.pingCollector.StopPing()
	}
	s.cfg.EnablePingCheck = setPingCheckReq.GetEnablePingCheck()
	return setPingCheckRsp, nil
}

func (s *EndNodeServer) GetPingMetric(ctx context.Context, in *proto.GetPingMetricReq) (*proto.GetPingMetricRsp, error) {
	getMetricRsp := &proto.GetPingMetricRsp{Code: 0}
	pingMetric := &proto.PingMetric{
		Host:    s.cfg.ProxyHost,
		Source:  s.cfg.Name,
		Results: make([]*proto.PingResult, 0),
	}
	if s.pingCollector != nil {
		pingMetric.Results = s.pingCollector.GetPingResults()
	}
	getMetricRsp.Metric = pingMetric
	return getMetricRsp, nil
}

func (s *EndNodeServer) GetNodeMetric(ctx context.Context, in *proto.GetNodeMetricReq) (*proto.GetNodeMetricRsp, error) {
	getMetricRsp := &proto.GetNodeMetricRsp{Code: 0}
	if s.nodeMetricCol == nil {
		return getMetricRsp, nil
	}
	mfs, err := s.nodeMetricCol.Collect()
	if err != nil {
		getMetricRsp.Code = 1050
		getMetricRsp.Msg = fmt.Sprintf("collect node metric fail, err: %v", err)
		return getMetricRsp, nil
	}
	nodeMetrics := &proto.NodeMetrics{
		Host:   s.cfg.ProxyHost,
		Source: s.cfg.Name,
	}
	for _, mf := range mfs {
		data, _ := pb.Marshal(mf)
		nodeMetrics.SerializedMetrics = append(nodeMetrics.SerializedMetrics, data)
	}
	getMetricRsp.NodeMetrics = nodeMetrics
	return getMetricRsp, nil
}
