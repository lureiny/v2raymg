package server

import (
	"github.com/gin-gonic/gin"
	"github.com/lureiny/v2raymg/config"
	"github.com/lureiny/v2raymg/stats"
)

func InitStatServer(t, apiHost string, apiPort int) {
	RestfulServer.GET("/stat", func(c *gin.Context) {
		token := c.DefaultQuery("token", "")
		if token != t {
			c.String(200, "err token")
			return
		}
		userStats, err := stats.QueryAllStats(apiHost, apiPort)
		if err != nil {
			config.Error.Println(err.Error())
		}
		var jsonDatas = gin.H{}
		for _, s := range *userStats {
			jsonDatas[s.Name] = *s
		}
		c.JSON(200, jsonDatas)
	})
}

// TODO(lureiny): 统计用户每分钟流量使用情况
