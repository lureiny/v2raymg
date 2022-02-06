package server

import (
	"encoding/base64"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/bound"
	"github.com/lureiny/v2raymg/config"
	"github.com/lureiny/v2raymg/sub"
)

func InitSubService(t, apiHost, proxyHost, configFile, defaultTag string, apiPort, proxyPort int) {
	RestfulServer.GET("/sub", func(c *gin.Context) {
		token := c.DefaultQuery("token", "")
		user := c.Query("user")
		pwd := c.Query("pwd")
		tag := c.DefaultQuery("tag", defaultTag)

		if token != t {
			c.String(200, "error token")
			return
		}

		// 尝试查找用户
		Users.lock.RLock()
		p, ok := (*Users.users)[user]
		Users.lock.RUnlock()

		if !ok {
			// 更新用户
			UpdatetUsers()
			Users.lock.RLock()
			p, ok = (*Users.users)[user]
			Users.lock.RUnlock()
		}

		if ok && p == pwd {
			uri, err := sub.GetUserSubUri(proxyHost, user, uint32(proxyPort), configFile)
			if err != nil && err.Error() == "No User" {
				// 先添加用户，然后获取uri
				runtimeConfig := &config.RuntimeConfig{
					Host:       apiHost,
					Port:       apiPort,
					ConfigFile: configFile,
				}

				p, err := bound.GetProtocol(tag, configFile)
				if err != nil {
					config.Info.Println(err.Error())
					return
				}

				boundUser, err := bound.NewUser(user, tag, bound.Protocol(p))

				if err != nil {
					config.Info.Println(err.Error())
					return
				}

				err = bound.AddUser(runtimeConfig, boundUser)
				if err != nil {
					config.Info.Println(err.Error())
					return
				}

				uri, err := sub.GetUserSubUri(proxyHost, user, uint32(proxyPort), configFile)
				if err != nil {
					config.Error.Println(err)
					return
				}
				if ua := c.GetHeader("User-Agent"); !strings.Contains(strings.ToLower(ua), "qv2ray") {
					uri = base64.StdEncoding.EncodeToString([]byte(uri))
				}
				c.String(200, uri)
			} else {
				if ua := c.GetHeader("User-Agent"); !strings.Contains(strings.ToLower(ua), "qv2ray") {
					uri = base64.StdEncoding.EncodeToString([]byte(uri))
				}
				c.String(200, uri)
			}
		} else {
			c.String(200, "")
		}
	})

}
