package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/common"
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
	common.StatsForPrometheus.Mutex.Lock()
	defer common.StatsForPrometheus.Mutex.Unlock()
	stats := []*proto.Stats{}
	for _, s := range common.StatsForPrometheus.StatsMap {
		stats = append(stats, s)
	}
	updateTrafficStats(&stats)
	common.StatsForPrometheus.StatsMap = make(map[string]*proto.Stats)
	c.Next()
}

func (handler *MetricHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		getAuthHandlerFunc(handler.httpServer),
		handler.handlerFunc,
		prometheusHandler(promhttp.Handler()),
	}
}

func (handler *MetricHandler) getRelativePath() string {
	return "/metrics"
}

func RegisterPrometheus() {
	prometheus.Register(trafficStats)
	GlobalHttpServer.RegisterHandler(&MetricHandler{})
}
