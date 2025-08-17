package http

import (
	"context"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	client "github.com/lureiny/v2raymg/client/rpc"
	"github.com/lureiny/v2raymg/common/log/logger"
	prometheusdesc "github.com/lureiny/v2raymg/server/http/prometheus_desc"
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
		rpcClient := client.NewEndNodeClient(nodes, nil)
		ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
		defer cancel()
		wg := &sync.WaitGroup{}
		wg.Add(3)
		go func() {
			defer wg.Done()
			trafficDesc.Update(ctx, rpcClient)
		}()
		go func() {
			defer wg.Done()
			pingDesc.Update(ctx, rpcClient)
		}()
		go func() {
			defer wg.Done()
			nodeMetricDesc.Update(ctx, rpcClient)
		}()
		wg.Wait()

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
