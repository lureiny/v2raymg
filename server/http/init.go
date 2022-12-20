package http

import "github.com/gin-gonic/gin"

func init() {
	gin.SetMode(gin.ReleaseMode)
	GlobalHttpServer.RestfulServer = gin.Default()
	GlobalHttpServer.handlersMap = map[string]HttpHandlerInterface{}

	subHandler := &SubHandler{}
	GlobalHttpServer.RegisterHandler("/sub", subHandler)

	helpHandler := &HelpHandler{}
	GlobalHttpServer.RegisterHandler("/help/*relativePath", helpHandler)

	adaptiveHandler := &AdaptiveHandler{}
	GlobalHttpServer.RegisterHandler("/adaptive", adaptiveHandler)

	adaptiveOpHandler := &AdaptiveOpHandler{}
	GlobalHttpServer.RegisterHandler("/adaptiveOp", adaptiveOpHandler)

	boundHandler := &BoundHandler{}
	GlobalHttpServer.RegisterHandler("/bound", boundHandler)

	nodeHandler := &NodeHandler{}
	GlobalHttpServer.RegisterHandler("/node", nodeHandler)

	statHandler := &StatHandler{}
	GlobalHttpServer.RegisterHandler("/stat", statHandler)

	tagHandler := &TagHandler{}
	GlobalHttpServer.RegisterHandler("/tag", tagHandler)

	updateHandler := &UpdateHandler{}
	GlobalHttpServer.RegisterHandler("/update", updateHandler)

	userHandler := &UserHandler{}
	GlobalHttpServer.RegisterHandler("/user", userHandler)

	gatewayHandler := &GatewayHandler{}
	GlobalHttpServer.RegisterHandler("/gateway", gatewayHandler)
}
