package http

import (
	"strings"

	"github.com/gin-gonic/gin"
)

type HelpHandler struct{ HttpHandlerImp }

func (handler *HelpHandler) handlerFunc(c *gin.Context) {
	relativePath := c.Param("relativePath")
	if h, ok := handler.getHttpServer().handlersMap[relativePath]; !ok {
		helpInfos := []string{}
		for _, handler := range handler.getHttpServer().handlersMap {
			helpInfos = append(helpInfos, handler.help())
		}
		c.String(200, strings.Join(helpInfos, "\n"))
	} else {
		c.String(200, h.help())
	}
}

func (handler *HelpHandler) help() string {
	usage := `/help/{relativePath}
	返回指定路径的help信息, 当relativePath为空时返回全部help信息
	`
	return usage
}
