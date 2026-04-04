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
	s.registerOn(userGroup, &ProfileHandler{}, "GET")
	s.registerOn(userGroup, &RotatePortHandler{}, "POST")
	s.registerOn(userGroup, &RotateInboundPortHandler{}, "POST")
	s.registerOn(userGroup, &RotateAllPortsHandler{}, "POST")
	s.registerOn(userGroup, &ChangePasswordHandler{}, "PUT")

	// Admin routes — authenticated + admin role required
	// X-Token holders are automatically treated as admin by AuthMiddleware.
	adminGroup := r.Group("/api", auth.AuthMiddleware(s.token, s.jwtSecret), auth.AdminOnly())
	s.registerOn(adminGroup, &NodeHandler{}, "GET")
	s.registerOn(adminGroup, &GetCertsHandler{}, "GET")
	s.registerOn(adminGroup, &UserListHandler{}, "GET")
	s.registerOn(adminGroup, &UserAddHandler{}, "POST")
	s.registerOn(adminGroup, &UserUpdateHandler{}, "PUT")
	s.registerOn(adminGroup, &UserDeleteHandler{}, "DELETE")
	s.registerOn(adminGroup, &UserResetHandler{}, "POST")
	s.registerOn(adminGroup, &InboundGetHandler{}, "GET")
	s.registerOn(adminGroup, &InboundAddHandler{}, "POST")
	s.registerOn(adminGroup, &InboundDeleteHandler{}, "DELETE")
	s.registerOn(adminGroup, &InboundListHandler{}, "GET")
	s.registerOn(adminGroup, &InboundDeleteByNameHandler{}, "DELETE")
	s.registerOn(adminGroup, &FastAddInboundHandler{}, "POST")
	s.registerOn(adminGroup, &CertHandler{}, "POST")
	s.registerOn(adminGroup, &TransferCertHandler{}, "POST")
	s.registerOn(adminGroup, &UpdateHandler{}, "POST")
	s.registerOn(adminGroup, &CopyUserBetweenNodesHandler{}, "POST")
	s.registerOn(adminGroup, &GatewayHandler{}, "PUT")
	s.registerOn(adminGroup, &PingCheckHandler{}, "PUT")
	s.registerOn(adminGroup, &ClearUserHandler{}, "DELETE")
	s.registerOn(adminGroup, &SetUserRoleHandler{}, "PUT")
}

// registerOn registers a handler on the given router (engine or group).
func (s *HttpServer) registerOn(router gin.IRouter, handler HttpHandlerInterface, method string) {
	relativePath := handler.getRelativePath()
	handler.setHttpServer(s)
	s.handlersMap[relativePath] = handler
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
