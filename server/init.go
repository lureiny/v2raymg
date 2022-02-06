package server

import "github.com/gin-gonic/gin"

var RestfulServer *gin.Engine

func InitGinServer() {
	gin.SetMode(gin.ReleaseMode)
	RestfulServer = gin.Default()
	UpdatetUsers()
}
