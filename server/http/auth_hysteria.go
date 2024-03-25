package http

import (
	"encoding/base64"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/common/log/logger"
	"github.com/lureiny/v2raymg/global/user"
)

type AuthHysteria2Data struct {
	Addr string `json:"addr"`
	Auth string `json:"auth"`
	TX   int64  `json:"tx`
}

type AuthHysteria2 struct{ HttpHandlerImp }

func (handler *AuthHysteria2) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}

	var req *AuthHysteria2Data = &AuthHysteria2Data{}

	if err := c.ShouldBindJSON(req); err != nil {
		logger.Debug("parse auth req fail, err: %v", err)
		return parasMap
	}

	parasMap["addr"] = req.Addr
	parasMap["auth"] = req.Auth
	parasMap["tx"] = string(req.TX)

	return parasMap
}

func (handler *AuthHysteria2) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)

	userMpa := user.GetUserList()
	for _, u := range userMpa {
		if u.Passwd == parasMap["auth"] ||
			base64.RawStdEncoding.EncodeToString([]byte(u.Passwd)) == parasMap["auth"] {
			c.JSON(200, map[string]interface{}{
				"ok": true,
				"id": u.Name,
			})
			return
		}
	}
	c.String(403, "")
}

func (handler *AuthHysteria2) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{
		handler.handlerFunc,
	}
}

func (handler *AuthHysteria2) getRelativePath() string {
	return "/authHysteria2"
}

func (handler *AuthHysteria2) help() string {
	usage := `/authHysteria2
	auth hysteria2服务
	`
	return usage
}
