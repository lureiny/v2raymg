package http

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/client"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/server"
	"github.com/lureiny/v2raymg/server/rpc/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var configManager = common.GetGlobalConfigManager()
var localNode = common.GlobalLocalNode

var GlobalHttpServer = &HttpServer{}

var trafficStats = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "v2raymg_traffic",
		Help: "v2ray/xray traffic ",
	},
	[]string{"node", "name", "type", "direction"},
)

type HttpServer struct {
	RestfulServer *gin.Engine
	server.ServerConfig
	userManager    *common.UserManager
	clusterManager *common.EndNodeClusterManager
	token          string // for admin op such as user op, stat op
	handlersMap    map[string]HttpHandlerInterface
}

var logger = common.LoggerImp

func (s *HttpServer) SetUserManager(um *common.UserManager) {
	s.userManager = um
}

func (s *HttpServer) Init(um *common.UserManager, cm *common.EndNodeClusterManager) {
	s.userManager = um
	s.clusterManager = cm

	s.Host = configManager.GetString(common.ServerListen)
	s.Port = configManager.GetInt(common.ServerHttpPort)
	s.token = configManager.GetString(common.ServerHttpToken)
	s.Name = configManager.GetString(common.ServerName)

	if configManager.GetBool(common.SupportPrometheus) {
		registerPrometheus(s)
	}
}

func PrometheusHandler(handler http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		handler.ServeHTTP(c.Writer, c.Request)
	}
}

func metricHandler(c *gin.Context) {
	nodes := []*common.Node{{
		InToken:  localNode.Token,
		OutToken: localNode.Token,
		Node: &proto.Node{
			Name: GlobalHttpServer.Name,
			Host: "127.0.0.1",
			Port: int32(configManager.GetInt(common.ServerRpcPort)),
		},
		ReportHeartBeatTime: time.Now().Unix(),
	}}
	rpcClient := client.NewEndNodeClient(&nodes, localNode)
	statsMap, err := rpcClient.GetBandWidthStats("", true)
	if err != nil {
		logger.Info(
			"Err=%s",
			err.Error(),
		)
		c.String(200, err.Error())
		c.Abort()
	}
	updateTrafficStats(statsMap)
	c.Next()
}

func updateTrafficStats(statsMap *map[string][]*proto.Stats) {
	for _, s := range (*statsMap)[GlobalHttpServer.Name] {
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

func registerPrometheus(s *HttpServer) {
	prometheus.Register(trafficStats)
	if authHandler, ok := s.handlersMap["/auth"]; ok {
		s.RestfulServer.GET("/metrics", authHandler.handlerFunc, metricHandler, PrometheusHandler(promhttp.Handler()))
	} else {
		s.RestfulServer.GET("/metrics", metricHandler, PrometheusHandler(promhttp.Handler()))
	}
}

func (s *HttpServer) SetName(name string) {
	s.Name = name
}

func (s *HttpServer) Start() {
	logger.Info(
		"Msg=http server start, listen at %s:%d",
		s.Host,
		s.Port,
	)
	s.RestfulServer.Run(fmt.Sprintf("%s:%d", s.Host, s.Port))
}

// 根据target查找路由的节点
func (s *HttpServer) getTargetNodes(target string) *[]*common.Node {
	if target == "all" {
		filter := func(n *common.Node) bool {
			return n.Name == s.Name || n.IsValid()
		}
		nodes := s.clusterManager.NodeManager.GetNodesWithFilter(filter)
		return nodes
	} else {
		filter := func(n *common.Node) bool {
			return n.IsValid() && n.Name == target
		}
		return s.clusterManager.NodeManager.GetNodesWithFilter(filter)
	}
}

func (s *HttpServer) RegisterHandler(relativePath string, handler HttpHandlerInterface, needAuth bool) {
	if authHandler, ok := s.handlersMap["/auth"]; ok && needAuth {
		s.RestfulServer.GET(relativePath, authHandler.handlerFunc, handler.handlerFunc)
	} else {
		s.RestfulServer.GET(relativePath, handler.handlerFunc)
	}
	s.handlersMap[relativePath] = handler
	handler.setHttpServer(s)
}
