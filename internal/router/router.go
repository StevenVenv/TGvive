package router

import (
	"my-go-server/internal/api/v1"
	"my-go-server/internal/global"
	"my-go-server/internal/middleware"

	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine {
	gin.SetMode(global.Config.Server.Mode)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.Cors())

	r.GET("/ping", v1.Ping)

	return r
}
