package http

import "github.com/gin-gonic/gin"

func init() {
	gin.SetMode(gin.ReleaseMode)
	GlobalHttpServer.RestfulServer = gin.Default()
	GlobalHttpServer.handlersMap = map[string]HttpHandlerInterface{}

	GlobalHttpServer.RegisterHandler(&SubHandler{})
	GlobalHttpServer.RegisterHandler(&HelpHandler{})
	GlobalHttpServer.RegisterHandler(&AdaptiveHandler{})
	GlobalHttpServer.RegisterHandler(&AdaptiveOpHandler{})
	GlobalHttpServer.RegisterHandler(&BoundHandler{})
	GlobalHttpServer.RegisterHandler(&NodeHandler{})
	GlobalHttpServer.RegisterHandler(&StatHandler{})
	GlobalHttpServer.RegisterHandler(&TagHandler{})
	GlobalHttpServer.RegisterHandler(&UpdateHandler{})
	GlobalHttpServer.RegisterHandler(&UserHandler{})
	GlobalHttpServer.RegisterHandler(&GatewayHandler{})
	GlobalHttpServer.RegisterHandler(&CertHandler{})
	GlobalHttpServer.RegisterHandler(&FastAddInboundHandler{})
	GlobalHttpServer.RegisterHandler(&TransferCertHandler{})
	GlobalHttpServer.RegisterHandler(&GetCertsHandler{})
	GlobalHttpServer.RegisterHandler(&ClearUserHandler{})
	GlobalHttpServer.RegisterHandler(&CopyUserBetweenNodesHandler{})
}
