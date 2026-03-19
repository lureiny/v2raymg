package http

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/rpc/client"
	"github.com/lureiny/v2raymg/pkg/log"
	prometheusdesc "github.com/lureiny/v2raymg/pkg/http/prometheus_desc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type MetricHandler struct{ HttpHandlerImp }

func prometheusHandler(metricHandler *MetricHandler) gin.HandlerFunc {
	// create prometheus desc registe
	reg := prometheus.NewRegistry()
	trafficDesc := prometheusdesc.NewV2raymgTrafficDesc()
	pingDesc := prometheusdesc.NewPingDesc()
	nodeMetricDesc := prometheusdesc.NewNodeMetricDesc()

	reg.Register(trafficDesc)
	reg.Register(pingDesc)
	reg.Register(nodeMetricDesc)

	promHandler := promhttp.HandlerFor(reg,
		promhttp.HandlerOpts{
			ErrorHandling: promhttp.ContinueOnError,
		})
	return func(c *gin.Context) {
		parasMap := metricHandler.parseParam(c)
		nodes := metricHandler.getHttpServer().GetTargetNodes(parasMap["target"])
		if nodes == nil {
			log.Error("no avaliable node for metrics")
			return
		}
		token := metricHandler.getHttpServer().GetClusterToken()
		rpcClient := client.NewEndNodeClient(nodes, metricHandler.getHttpServer().GetLocalNode())
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		wg := &sync.WaitGroup{}
		wg.Add(3)
		go func() {
			defer wg.Done()
			trafficDesc.Update(ctx, rpcClient, token)
		}()
		go func() {
			defer wg.Done()
			pingDesc.Update(ctx, rpcClient, token)
		}()
		go func() {
			defer wg.Done()
			nodeMetricDesc.Update(ctx, rpcClient, token)
		}()
		wg.Wait()

		promHandler.ServeHTTP(c.Writer, c.Request)
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

func RegisterPrometheus(s *HttpServer) {
	s.RegisterHandler(&MetricHandler{}, "GET")
}
