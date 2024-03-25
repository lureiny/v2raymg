package http

import "github.com/gin-gonic/gin"

func init() {
	gin.SetMode(gin.ReleaseMode)
	GlobalHttpServer.RestfulServer = gin.Default()
	GlobalHttpServer.handlersMap = map[string]HttpHandlerInterface{}

	GlobalHttpServer.RegisterHandler(&SubHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&HelpHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&AdaptiveHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&AdaptiveOpHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&BoundHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&NodeHandler{}, "GET")
	// 与MerticHandler冲突, 暂时关闭
	// GlobalHttpServer.RegisterHandler(&StatHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&TagHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&UpdateHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&UserHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&GatewayHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&CertHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&FastAddInboundHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&TransferCertHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&GetCertsHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&ClearUserHandler{}, "GET")
	GlobalHttpServer.RegisterHandler(&CopyUserBetweenNodesHandler{}, "GET")
	// hysteria2 post方式进行auth 将auth外置
	GlobalHttpServer.RegisterHandler(&AuthHysteria2{}, "POST")
}
