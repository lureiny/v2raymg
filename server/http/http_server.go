package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/cluster"
	"github.com/lureiny/v2raymg/common"
	globalCluster "github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/global/logger"
	"github.com/lureiny/v2raymg/lego"
	"github.com/lureiny/v2raymg/server"
	"github.com/prometheus/client_golang/prometheus"
)

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
	token       string // for admin op such as user op, stat op
	handlersMap map[string]HttpHandlerInterface
	certManager *lego.CertManager
}

func (s *HttpServer) Init(certManager *lego.CertManager) {
	s.certManager = certManager

	s.Host = config.GetString(common.ConfigServerListen)
	s.Port = config.GetInt(common.ConfigServerHttpPort)
	s.token = config.GetString(common.ConfigServerHttpToken)
	s.Name = config.GetString(common.ConfigServerName)
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
func (s *HttpServer) GetTargetNodes(target string) *[]*cluster.Node {
	if target == "all" {
		filter := func(n *cluster.Node) bool {
			return n.Name == s.Name || n.IsValid()
		}
		nodes := globalCluster.GetNodesWithFilter(filter)
		return nodes
	} else {
		filter := func(n *cluster.Node) bool {
			return n.IsValid() && n.Name == target
		}
		return globalCluster.GetNodesWithFilter(filter)
	}
}

func (s *HttpServer) RegisterHandler(handler HttpHandlerInterface) {
	relativePath := handler.getRelativePath()
	handler.setHttpServer(s)
	s.handlersMap[relativePath] = handler
	handlers := handler.getHandlers()
	s.RestfulServer.GET(relativePath, handlers...)
}
