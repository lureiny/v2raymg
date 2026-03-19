package http

import (
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

type HelpHandler struct{ HttpHandlerImp }

func (handler *HelpHandler) handlerFunc(c *gin.Context) {
	relativePath := c.Param("relativePath")
	if h, ok := handler.getHttpServer().handlersMap[relativePath]; !ok {
		helpInfos := []string{}
		for _, handler := range handler.getHttpServer().handlersMap {
			if handler.help() != "" {
				helpInfos = append(helpInfos, handler.help())
			}
		}
		sort.Strings(helpInfos)
		c.String(200, strings.Join(helpInfos, "\n"))
	} else {
		c.String(200, h.help())
	}
}

func (handler *HelpHandler) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		handler.handlerFunc,
	}
}

func (handler *HelpHandler) getRelativePath() string {
	return "/help/*relativePath"
}

func (handler *HelpHandler) help() string {
	usage := `/help/{relativePath}
	返回指定路径的help信息, 当relativePath为空时返回全部help信息
	`
	return usage
}
