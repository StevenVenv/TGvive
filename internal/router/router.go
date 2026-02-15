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

	taskApi := v1.TaskApi{}
	authApi := v1.AuthApi{}
	tgAuthApi := v1.TGAuthApi{}
	apiV1 := r.Group("/api/v1")
	{
		apiV1.GET("/ping", v1.Ping)

		authV1 := apiV1.Group("/auth")
		{
			authV1.GET("/dev/token", authApi.DevToken)
		}

		apiV1.GET("/tg/qr", tgAuthApi.GetQRCode)
		apiV1.GET("/tg/qr/status", tgAuthApi.CheckQRStatus)
		apiV1.GET("/tg/qr/ws", tgAuthApi.QRWebSocket)

		tgV1 := apiV1.Group("/tg")
		// tgV1.Use(middleware.JWTAuth())
		{
			tgV1.GET("/accounts", tgAuthApi.ListAccounts)
			tgV1.POST("/accounts/qr", tgAuthApi.StartAccountQR)
			tgV1.GET("/accounts/qr/status", tgAuthApi.CheckQRStatus)
			tgV1.POST("/accounts/code", tgAuthApi.StartCodeLogin)
			tgV1.GET("/accounts/code/status", tgAuthApi.CheckCodeStatus)
			tgV1.POST("/accounts/code/submit", tgAuthApi.SubmitCode)
			tgV1.POST("/accounts/code/password", tgAuthApi.SubmitPassword)
		}

		taskV1 := apiV1.Group("/tasks")
		// taskV1.Use(middleware.JWTAuth())
		{
			taskV1.POST("", taskApi.CreateTask)
			taskV1.GET("", taskApi.GetTaskList)
			taskV1.POST("/action", taskApi.UpdateTaskStatus)
			taskV1.GET("/:id/progress", taskApi.GetTaskProgress)
		}
	}

	return r
}
