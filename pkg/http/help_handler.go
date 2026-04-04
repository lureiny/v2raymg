package http

import (
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

type HelpHandler struct{ HttpHandlerImp }

func (handler *HelpHandler) handlerFunc(c *gin.Context) {
	relativePath := c.Param("relativePath")
	// Try to find a handler whose relativePath matches (keys are "METHOD path").
	var matched HttpHandlerInterface
	if relativePath != "" && relativePath != "/" {
		for _, h := range handler.getHttpServer().handlersMap {
			if h.getRelativePath() == relativePath {
				matched = h
				break
			}
		}
	}
	if matched != nil {
		c.String(200, matched.help())
	} else {
		helpInfos := []string{}
		for _, h := range handler.getHttpServer().handlersMap {
			if h.help() != "" {
				helpInfos = append(helpInfos, h.help())
			}
		}
		sort.Strings(helpInfos)
		c.String(200, strings.Join(helpInfos, "\n"))
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
