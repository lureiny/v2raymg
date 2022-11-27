package http

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/common"
	"github.com/lureiny/v2raymg/server"
	"github.com/lureiny/v2raymg/server/rpc/proto"
)

var configManager = common.GetGlobalConfigManager()
var localNode = common.GlobalLocalNode

var GlobalHttpServer = &HttpServer{}

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

	s.Host = configManager.GetString("server.listen")
	s.Port = configManager.GetInt("server.http.port")
	s.token = configManager.GetString("server.http.token")
	s.Name = configManager.GetString("server.name")
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
			return n.IsValid()
		}
		nodes := s.clusterManager.RemoteNode.GetNodesWithFilter(filter)
		*nodes = append(*nodes, &common.Node{
			InToken:  localNode.Token,
			OutToken: localNode.Token,
			Node: &proto.Node{
				Name: s.Name,
				Host: "127.0.0.1",
				Port: int32(configManager.GetInt("server.rpc.port")),
			},
			ReportHeartBeatTime: time.Now().Unix(),
		})
		return nodes
	} else if target == s.Name {
		// 本地节点
		return &[]*common.Node{
			{
				InToken:  localNode.Token,
				OutToken: localNode.Token,
				Node: &proto.Node{
					Name: s.Name,
					Host: "127.0.0.1",
					Port: int32(configManager.GetInt("server.rpc.port")),
				},
				ReportHeartBeatTime: time.Now().Unix(),
			},
		}
	} else {
		filter := func(n *common.Node) bool {
			return n.IsValid() && n.Name == target
		}
		return s.clusterManager.RemoteNode.GetNodesWithFilter(filter)
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
