package http

import "github.com/gin-gonic/gin"

type HttpHandlerInterface interface {
	parseParam(*gin.Context) map[string]string
	handlerFunc(*gin.Context)
	setHttpServer(*HttpServer)
	getHttpServer() *HttpServer
	getHandlers() []gin.HandlerFunc
	help() string
}

type HttpHandlerImp struct {
	httpServer *HttpServer
}

func (handler *HttpHandlerImp) parseParam(*gin.Context) map[string]string {
	return map[string]string{}
}

func (handler *HttpHandlerImp) handlerFunc(*gin.Context) {}

func (handler *HttpHandlerImp) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *HttpHandlerImp) setHttpServer(httpServer *HttpServer) {
	handler.httpServer = httpServer
}

func (handler *HttpHandlerImp) getHttpServer() *HttpServer { return handler.httpServer }

func (handler *HttpHandlerImp) help() string { return "" }
