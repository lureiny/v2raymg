package http

import "github.com/gin-gonic/gin"

func init() {
	gin.SetMode(gin.ReleaseMode)
	GlobalHttpServer.RestfulServer = gin.Default()
	GlobalHttpServer.handlersMap = map[string]HttpHandlerInterface{}

	// auth handler注册后才可以注册其他需要auth的handler
	authWithTokenHandler := &AuthWithTokenHandler{}
	GlobalHttpServer.RegisterHandler("/auth", authWithTokenHandler, false)

	subHandler := &SubHandler{}
	GlobalHttpServer.RegisterHandler("/sub", subHandler, false)

	helpHandler := &HelpHandler{}
	GlobalHttpServer.RegisterHandler("/help/*relativePath", helpHandler, false)

	adaptiveHandler := &AdaptiveHandler{}
	GlobalHttpServer.RegisterHandler("/adaptive", adaptiveHandler, true)

	adaptiveOpHandler := &AdaptiveOpHandler{}
	GlobalHttpServer.RegisterHandler("/adaptiveOp", adaptiveOpHandler, true)

	boundHandler := &BoundHandler{}
	GlobalHttpServer.RegisterHandler("/bound", boundHandler, true)

	nodeHandler := &NodeHandler{}
	GlobalHttpServer.RegisterHandler("/node", nodeHandler, true)

	statHandler := &StatHandler{}
	GlobalHttpServer.RegisterHandler("/stat", statHandler, true)

	tagHandler := &TagHandler{}
	GlobalHttpServer.RegisterHandler("/tag", tagHandler, true)

	updateHandler := &UpdateHandler{}
	GlobalHttpServer.RegisterHandler("/update", updateHandler, true)

	userHandler := &UserHandler{}
	GlobalHttpServer.RegisterHandler("/user", userHandler, true)
}
