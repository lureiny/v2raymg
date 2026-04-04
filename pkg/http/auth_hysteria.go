package http

import (
	"encoding/base64"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/pkg/log"
	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// UserLister provides user lookup for HTTP handlers.
type UserLister interface {
	// ListUsersWithPasswd returns a map of username -> password for all active users.
	// Used by Hysteria2 authentication and port rotation.
	ListUsersWithPasswd() map[string]string
	// GetUser returns the full UserSpec for the given username.
	// Used by login and profile handlers.
	GetUser(username string) (*contracts.User, error)
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
		c.JSON(403, gin.H{"ok": false})
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
	c.JSON(403, gin.H{"ok": false})
}

func (handler *AuthHysteria2) getHandlers() []gin.HandlerFunc {
	return []gin.HandlerFunc{handler.handlerFunc}
}

func (handler *AuthHysteria2) getRelativePath() string { return "/api/authHysteria2" }

func (handler *AuthHysteria2) help() string {
	return `/api/authHysteria2 auth hysteria2服务`
}
