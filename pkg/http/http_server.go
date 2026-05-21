package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/cluster"
	"github.com/lureiny/v2raymg/pkg/log"
)

// HttpServerConfig holds HTTP server configuration.
type HttpServerConfig struct {
	Listen         string
	Port           int
	Token          string
	Name           string
	JWTSecret      string
	JWTExpireHours int
	// EnableSubUserInfoHeader controls whether /sub responses carry the
	// Subscription-Userinfo header. See SubHandler for the upload/download
	// aggregation behaviour.
	EnableSubUserInfoHeader bool
	// SubUserInfoHeaderFormat is the node-config override of the
	// Subscription-Userinfo header schema. Empty falls back to the built-in
	// DefaultSubUserInfoFormat. Per-request ?sub_userinfo_format= overrides
	// this value.
	SubUserInfoHeaderFormat string
}

// ClusterNodes provides node lookup for routing HTTP requests to cluster members.
type ClusterNodes interface {
	GetNodesWithFilter(f cluster.NodeFilter) []*cluster.Node
	GetClusterToken() string
}


// CertReader provides read access to local certificate files.
// It is a subset of the full CertManager interface, used only by TransferCertHandler.
type CertReader interface {
	// GetCertFiles returns the cert file path and key file path for the given domain.
	// Returns ("", "", false) if not found.
	GetCertFiles(domain string) (certFile, keyFile string, ok bool)
}

var GlobalHttpServer = &HttpServer{}

type HttpServer struct {
	RestfulServer           *gin.Engine
	Host                    string
	Port                    int
	Name                    string
	token                   string
	jwtSecret               string
	jwtExpireHours          int
	handlersMap             map[string]HttpHandlerInterface
	certReader              CertReader
	clusterNodes            ClusterNodes
	localNode               *cluster.LocalNode
	userLister              UserLister
	clusterEnabled          bool
	enableSubUserInfoHeader bool
	subUserInfoHeaderFormat string
}

// Init initializes the HttpServer with config and dependencies.
func (s *HttpServer) Init(cfg HttpServerConfig, localNode *cluster.LocalNode, clusterNodes ClusterNodes, certReader CertReader, userLister UserLister, clusterEnabled bool) {
	s.Host = cfg.Listen
	s.Port = cfg.Port
	s.token = cfg.Token
	s.Name = cfg.Name
	s.jwtSecret = cfg.JWTSecret
	s.jwtExpireHours = cfg.JWTExpireHours
	s.localNode = localNode
	s.clusterNodes = clusterNodes
	s.certReader = certReader
	s.userLister = userLister
	s.clusterEnabled = clusterEnabled
	s.enableSubUserInfoHeader = cfg.EnableSubUserInfoHeader
	s.subUserInfoHeaderFormat = cfg.SubUserInfoHeaderFormat
	s.registerRoutes()
}

// GetLocalNode returns the local cluster node identity.
func (s *HttpServer) GetLocalNode() *cluster.LocalNode {
	return s.localNode
}

func (s *HttpServer) SetName(name string) {
	s.Name = name
}

func (s *HttpServer) Start() {
	log.Info("http server starting", "host", s.Host, "port", s.Port)
	s.RestfulServer.Run(fmt.Sprintf("%s:%d", s.Host, s.Port))
}

// GetTargetNodes returns the nodes to route an HTTP request to based on the target param.
func (s *HttpServer) GetTargetNodes(target string) []*cluster.Node {
	if target == "" {
		target = s.Name
	}
	if target == "all" {
		return s.clusterNodes.GetNodesWithFilter(func(n *cluster.Node) bool {
			return n.Name == s.Name || n.IsValid()
		})
	}
	return s.clusterNodes.GetNodesWithFilter(func(n *cluster.Node) bool {
		return n.IsValid() && n.Name == target
	})
}

// GetClusterToken returns the cluster token for gRPC calls.
func (s *HttpServer) GetClusterToken() string {
	return s.clusterNodes.GetClusterToken()
}

func (s *HttpServer) RegisterHandler(handler HttpHandlerInterface, method string) {
	if s.handlersMap == nil {
		s.handlersMap = make(map[string]HttpHandlerInterface)
	}
	relativePath := handler.getRelativePath()
	handler.setHttpServer(s)
	key := method + " " + relativePath
	s.handlersMap[key] = handler
	handlers := handler.getHandlers()
	switch method {
	case "GET":
		s.RestfulServer.GET(relativePath, handlers...)
	case "POST":
		s.RestfulServer.POST(relativePath, handlers...)
	case "PUT":
		s.RestfulServer.PUT(relativePath, handlers...)
	case "DELETE":
		s.RestfulServer.DELETE(relativePath, handlers...)
	}
}
