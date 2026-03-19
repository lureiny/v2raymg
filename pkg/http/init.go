package http

import "github.com/gin-gonic/gin"

// NewHttpServer creates and returns a new HttpServer with all handlers registered.
// Call Init() on the returned server before Start().
func NewHttpServer() *HttpServer {
	gin.SetMode(gin.ReleaseMode)
	s := &HttpServer{
		RestfulServer: gin.Default(),
		handlersMap:   make(map[string]HttpHandlerInterface),
	}

	// Handlers that stay GET (not in migration scope)
	s.RegisterHandler(&SubHandler{}, "GET")
	s.RegisterHandler(&HelpHandler{}, "GET")
	s.RegisterHandler(&NodeHandler{}, "GET")
	s.RegisterHandler(&TagHandler{}, "GET")
	s.RegisterHandler(&GetCertsHandler{}, "GET")

	// User handlers (split by method)
	s.RegisterHandler(&UserListHandler{}, "GET")
	s.RegisterHandler(&UserAddHandler{}, "POST")
	s.RegisterHandler(&UserUpdateHandler{}, "PUT")
	s.RegisterHandler(&UserDeleteHandler{}, "DELETE")
	s.RegisterHandler(&UserResetHandler{}, "POST")

	// Inbound handlers (split by method)
	s.RegisterHandler(&InboundGetHandler{}, "GET")
	s.RegisterHandler(&InboundAddHandler{}, "POST")
	s.RegisterHandler(&InboundDeleteHandler{}, "DELETE")

	// POST handlers
	s.RegisterHandler(&FastAddInboundHandler{}, "POST")
	s.RegisterHandler(&CertHandler{}, "POST")
	s.RegisterHandler(&TransferCertHandler{}, "POST")
	s.RegisterHandler(&UpdateHandler{}, "POST")
	s.RegisterHandler(&CopyUserBetweenNodesHandler{}, "POST")
	s.RegisterHandler(&AuthHysteria2{}, "POST")
	s.RegisterHandler(&RotatePortHandler{}, "POST")

	// PUT handlers
	s.RegisterHandler(&GatewayHandler{}, "PUT")
	s.RegisterHandler(&PingCheckHandler{}, "PUT")

	// DELETE handlers
	s.RegisterHandler(&ClearUserHandler{}, "DELETE")

	return s
}
