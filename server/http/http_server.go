package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/cluster"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/common/log/logger"
	globalCluster "github.com/lureiny/v2raymg/global/cluster"
	"github.com/lureiny/v2raymg/global/config"
	"github.com/lureiny/v2raymg/lego"
	"github.com/lureiny/v2raymg/server"
)

var GlobalHttpServer = &HttpServer{}

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
func (s *HttpServer) GetTargetNodes(target string) []*cluster.Node {
	if target == "" {
		target = s.Name
	}
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

func (s *HttpServer) RegisterHandler(handler HttpHandlerInterface, method string) {
	relativePath := handler.getRelativePath()
	handler.setHttpServer(s)
	s.handlersMap[relativePath] = handler
	handlers := handler.getHandlers()
	switch method {
	case "GET":
		s.RestfulServer.GET(relativePath, handlers...)
	case "POST":
		s.RestfulServer.POST(relativePath, handlers...)
	}
}
