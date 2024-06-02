package http

import (
	"github.com/gin-gonic/gin"
	client "github.com/lureiny/v2raymg/client/rpc"
	"github.com/lureiny/v2raymg/common/log/logger"
	globalCluster "github.com/lureiny/v2raymg/global/cluster"
	prometheusdesc "github.com/lureiny/v2raymg/server/http/prometheus_desc"
	"github.com/lureiny/v2raymg/server/rpc/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type MetricHandler struct{ HttpHandlerImp }

func prometheusHandler(metricHandler *MetricHandler) gin.HandlerFunc {
	// create prometheus desc register
	reg := prometheus.NewPedanticRegistry()
	trafficDesc := prometheusdesc.NewV2raymgTrafficDesc()
	pingDesc := prometheusdesc.NewPingDesc()

	reg.MustRegister(trafficDesc)
	reg.MustRegister(pingDesc)
	handler := promhttp.HandlerFor(reg,
		promhttp.HandlerOpts{
			ErrorHandling: promhttp.ContinueOnError,
		})
	return func(c *gin.Context) {
		// 查询stats
		parasMap := metricHandler.parseParam(c)
		nodes := metricHandler.getHttpServer().GetTargetNodes(parasMap["target"])
		if nodes == nil {
			logger.Error("no avaliable node")
			return
		}
		// stats
		rpcClient := client.NewEndNodeClient(nodes, nil)
		succList, _, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
			client.GetBandWidthStatsReqType,
			&proto.GetBandwidthStatsReq{},
			globalCluster.GetClusterToken(),
		)
		stats := []*proto.Stats{}
		for _, v := range succList {
			s, _ := v.([]*proto.Stats)
			stats = append(stats, s...)
		}
		trafficDesc.Mutex.Lock()
		defer trafficDesc.Mutex.Unlock()
		trafficDesc.Stats = stats

		// ping metrics
		metricSuccList, _, _ := rpcClient.ReqToMultiEndNodeServer(c.Request.Context(),
			client.GetPingMetricType,
			&proto.GetPingMetricReq{},
			globalCluster.GetClusterToken(),
		)
		metrics := []*proto.PingMetric{}
		for _, v := range metricSuccList {
			m, _ := v.(*proto.PingMetric)
			metrics = append(metrics, m)
		}
		pingDesc.Mutex.Lock()
		defer pingDesc.Mutex.Unlock()
		pingDesc.Metrics = metrics

		handler.ServeHTTP(c.Writer, c.Request)
	}
}

func (handler *MetricHandler) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	parasMap["target"] = c.DefaultQuery("target", handler.getHttpServer().Name)
	return parasMap
}

func (handler *MetricHandler) handlerFunc(c *gin.Context) {}

func (handler *MetricHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		prometheusHandler(handler),
	}
}

func (handler *MetricHandler) getRelativePath() string {
	return "/metrics"
}

func RegisterPrometheus() {
	GlobalHttpServer.RegisterHandler(&MetricHandler{}, "GET")
}
