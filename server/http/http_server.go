package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/lego"
	"github.com/lureiny/v2raymg/server"
	"github.com/prometheus/client_golang/prometheus"
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
	certManager    *lego.CertManager
}

var logger = common.LoggerImp

func (s *HttpServer) SetUserManager(um *common.UserManager) {
	s.userManager = um
}

func (s *HttpServer) Init(um *common.UserManager, cm *common.EndNodeClusterManager, certManager *lego.CertManager) {
	s.userManager = um
	s.clusterManager = cm
	s.certManager = certManager

	s.Host = configManager.GetString(common.ServerListen)
	s.Port = configManager.GetInt(common.ServerHttpPort)
	s.token = configManager.GetString(common.ServerHttpToken)
	s.Name = configManager.GetString(common.ServerName)
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
func (s *HttpServer) GetTargetNodes(target string) *[]*common.Node {
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

func (s *HttpServer) RegisterHandler(handler HttpHandlerInterface) {
	relativePath := handler.getRelativePath()
	handler.setHttpServer(s)
	s.handlersMap[relativePath] = handler
	handlers := handler.getHandlers()
	s.RestfulServer.GET(relativePath, handlers...)
}
