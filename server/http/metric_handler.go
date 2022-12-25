package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/server/rpc/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type MetricHandler struct{ HttpHandlerImp }

func prometheusHandler(handler http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		handler.ServeHTTP(c.Writer, c.Request)
	}
}

func updateTrafficStats(stats *[]*proto.Stats) {
	for _, s := range *stats {
		trafficStats.WithLabelValues(
			GlobalHttpServer.Name,
			s.Name,
			s.Type,
			"downlink",
		).Set(float64(s.Downlink))

		trafficStats.WithLabelValues(
			GlobalHttpServer.Name,
			s.Name,
			s.Type,
			"uplink",
		).Set(float64(s.Uplink))
	}
}

func (handler *MetricHandler) handlerFunc(c *gin.Context) {
	nodes := handler.getHttpServer().getTargetNodes(handler.getHttpServer().Name)
	rpcClient := client.NewEndNodeClient(nodes, localNode)
	succList, failedList, _ := rpcClient.ReqToMultiEndNodeServer(
		client.GetBandWidthStatsReqType,
		&proto.GetBandwidthStatsReq{
			Pattern: "",
			Reset_:  true,
		},
	)
	if len(failedList) != 0 {
		errMsg := joinFailedList(failedList)
		logger.Error("Err=%s", errMsg)
		c.String(200, errMsg)
		c.Abort()
	}
	stats := succList[handler.getHttpServer().Name].([]*proto.Stats)
	updateTrafficStats(&stats)
	c.Next()
}

func (handler *MetricHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
		prometheusHandler(promhttp.Handler()),
	}
}

func RegisterPrometheus() {
	prometheus.Register(trafficStats)
	metricHandler := &MetricHandler{}
	GlobalHttpServer.RegisterHandler("/metrics", metricHandler)
}
