package http

import (
	"encoding/base64"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
)

// UserLister provides a user list for Hysteria2 authentication.
// Callers inject an implementation (e.g. wrapping global/user or the new usermanager).
type UserLister interface {
	// ListUsersWithPasswd returns a map of username -> password for all active users.
	ListUsersWithPasswd() map[string]string
}

type AuthHysteria2Data struct {
	Addr string `json:"addr"`
	Auth string `json:"auth"`
	TX   int64  `json:"tx"`
}

type AuthHysteria2 struct{ HttpHandlerImp }

func (handler *AuthHysteria2) parseParam(c *gin.Context) map[string]string {
	parasMap := map[string]string{}
	var req AuthHysteria2Data
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Debug("parse hysteria2 auth req failed", "err", err)
		return parasMap
	}
	parasMap["addr"] = req.Addr
	parasMap["auth"] = req.Auth
	parasMap["tx"] = strconv.FormatInt(req.TX, 10)
	return parasMap
}

func (handler *AuthHysteria2) handlerFunc(c *gin.Context) {
	parasMap := handler.parseParam(c)
	ul := handler.getHttpServer().userLister
	if ul == nil {
		c.String(403, "")
		return
	}
	for name, passwd := range ul.ListUsersWithPasswd() {
		if passwd == parasMap["auth"] ||
			base64.RawStdEncoding.EncodeToString([]byte(passwd)) == parasMap["auth"] {
			c.JSON(200, map[string]interface{}{
				"ok": true,
				"id": name,
			})
			return
		}
	}
	c.String(403, "")
}

func (handler *AuthHysteria2) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *AuthHysteria2) getRelativePath() string { return "/authHysteria2" }

func (handler *AuthHysteria2) help() string {
	return `/authHysteria2 auth hysteria2服务`
}
