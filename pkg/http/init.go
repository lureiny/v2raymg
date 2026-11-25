package http

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/http/auth"
)

// NewHttpServer creates and returns a new HttpServer without routes registered.
// Call Init() to configure dependencies and register all routes, then Start().
func NewHttpServer() *HttpServer {
	gin.SetMode(gin.ReleaseMode)
	return &HttpServer{
		RestfulServer: gin.Default(),
		handlersMap:   make(map[string]HttpHandlerInterface),
	}
}

// registerRoutes sets up all HTTP routes grouped by auth requirements.
// Called automatically by Init() after all config fields are set.
func (s *HttpServer) registerRoutes() {
	r := s.RestfulServer

	// Public routes — no authentication required
	s.registerOn(r, &LoginHandler{}, "POST")
	s.registerOn(r, &SubHandler{}, "GET")
	s.registerOn(r, &HelpHandler{}, "GET")
	s.registerOn(r, &AuthHysteria2{}, "POST")

	// User routes — any authenticated user (normal or admin)
	userGroup := r.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret))
	s.registerOn(userGroup, &LogoutHandler{}, "POST")
	registerEndpoint(s, userGroup, s.userListEndpoint())
	registerEndpoint(s, userGroup, s.profileEndpoint())
	registerEndpoint(s, userGroup, s.rotateInboundPortEndpoint())
	registerEndpoint(s, userGroup, s.rotateAllPortsEndpoint())
	registerEndpoint(s, userGroup, s.changePasswordEndpoint())
	registerEndpoint(s, userGroup, s.userResetAuthTokenEndpoint())
	registerEndpoint(s, userGroup, s.statusEndpoint())

	// Admin routes — authenticated + admin role required
	// X-Token holders are automatically treated as admin by AuthMiddleware.
	adminGroup := r.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret), auth.AdminOnly())
	registerEndpoint(s, adminGroup, s.nodeListEndpoint())
	registerEndpoint(s, adminGroup, s.containersEndpoint())
	registerEndpoint(s, adminGroup, s.certsListEndpoint())
	// User CRUD routes — always registered; UserManager is now the single user store.
	registerEndpoint(s, adminGroup, s.userAddEndpoint())
	registerEndpoint(s, adminGroup, s.userUpdateEndpoint())
	registerEndpoint(s, adminGroup, s.userDeleteEndpoint())
	registerEndpoint(s, adminGroup, s.userResetEndpoint())
	registerEndpoint(s, adminGroup, s.userResetTrafficEndpoint())
	registerEndpoint(s, adminGroup, s.inboundGetEndpoint())
	registerEndpoint(s, adminGroup, s.inboundAddEndpoint())
	registerEndpoint(s, adminGroup, s.inboundListEndpoint())
	registerEndpoint(s, adminGroup, s.inboundDeleteByNameEndpoint())
	registerEndpoint(s, adminGroup, s.fastAddInboundEndpoint())
	registerEndpoint(s, adminGroup, s.applyCertEndpoint())
	registerEndpoint(s, adminGroup, s.transferCertEndpoint())
	registerEndpoint(s, adminGroup, s.deleteCertEndpoint())
	registerEndpoint(s, adminGroup, s.updateNodeEndpoint())
	registerEndpoint(s, adminGroup, s.copyUserBetweenNodesEndpoint())
	registerEndpoint(s, adminGroup, s.setGatewayEndpoint())
	registerEndpoint(s, adminGroup, s.setPingCheckEndpoint())
	registerEndpoint(s, adminGroup, s.setUserRoleEndpoint())
	registerEndpoint(s, adminGroup, s.setUserBandwidthEndpoint())
	registerEndpoint(s, adminGroup, s.setUserClientLimitEndpoint())
	// Node Groups routes — only registered when cluster sync is enabled.
	if s.clusterEnabled {
		registerEndpoint(s, adminGroup, s.nodeGroupsGetEndpoint())
		registerEndpoint(s, adminGroup, s.nodeGroupsSetEndpoint())
	}
}

// registerOn registers a handler on the given router (engine or group).
func (s *HttpServer) registerOn(router gin.IRouter, handler HttpHandlerInterface, method string) {
	relativePath := handler.getRelativePath()
	handler.setHttpServer(s)
	key := method + " " + relativePath
	s.handlersMap[key] = handler
	handlers := handler.getHandlers()
	switch method {
	case "GET":
		router.GET(relativePath, handlers...)
	case "POST":
		router.POST(relativePath, handlers...)
	case "PUT":
		router.PUT(relativePath, handlers...)
	case "DELETE":
		router.DELETE(relativePath, handlers...)
	}
}
