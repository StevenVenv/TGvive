package router

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

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
	strategyApi := v1.StrategyApi{}
	keywordApi := v1.KeywordProfileApi{}
	authApi := v1.AuthApi{}
	tgAuthApi := v1.TGAuthApi{}
	tgBotApi := v1.TGBotApi{}
	dashboardApi := v1.DashboardApi{}
	watermarkApi := v1.WatermarkApi{}
	proxyApi := v1.ProxyApi{}
	apiV1 := r.Group("/api/v1")
	{
		apiV1.GET("/ping", v1.Ping)

		authV1 := apiV1.Group("/auth")
		{
			authV1.GET("/dev/token", authApi.DevToken)
		}

		protected := apiV1.Group("")
		protected.Use(middleware.JWTAuth())
		{
			protected.GET("/tg/qr", tgAuthApi.GetQRCode)
			protected.GET("/tg/qr/status", tgAuthApi.CheckQRStatus)
			protected.GET("/tg/qr/ws", tgAuthApi.QRWebSocket)

			// Convenience alias for frontend: /accounts mirrors /tg/accounts.
			protected.GET("/accounts", tgAuthApi.ListAccounts)

			tgV1 := protected.Group("/tg")
			{
				tgV1.GET("/accounts", tgAuthApi.ListAccounts)
				tgV1.DELETE("/accounts/:key", tgAuthApi.RemoveAccount)
				tgV1.POST("/accounts/qr", tgAuthApi.StartAccountQR)
				tgV1.GET("/accounts/qr/status", tgAuthApi.CheckQRStatus)
				tgV1.GET("/accounts/:key/dialogs", tgAuthApi.ListAccountDialogs)
				tgV1.GET("/accounts/:key/resolve", tgAuthApi.ResolveAccountPeer)
				tgV1.POST("/accounts/qr/password", tgAuthApi.SubmitQRPassword)
				tgV1.POST("/accounts/code", tgAuthApi.StartCodeLogin)
				tgV1.GET("/accounts/code/status", tgAuthApi.CheckCodeStatus)
				tgV1.POST("/accounts/code/submit", tgAuthApi.SubmitCode)
				tgV1.POST("/accounts/code/password", tgAuthApi.SubmitPassword)

				tgV1.GET("/bots", tgBotApi.ListBots)
				tgV1.POST("/bots", tgBotApi.AddBot)
				tgV1.PUT("/bots/:id", tgBotApi.UpdateBot)
				tgV1.DELETE("/bots/:id", tgBotApi.RemoveBot)
				tgV1.POST("/bots/:id/test", tgBotApi.TestBot)
			}

			settingsV1 := protected.Group("/settings")
			{
				settingsV1.GET("/proxy", proxyApi.Get)
				settingsV1.PUT("/proxy", proxyApi.Update)
				settingsV1.POST("/proxy/test", proxyApi.Test)
			}
		}

		taskV1 := protected.Group("/tasks")
		{
			taskV1.POST("", taskApi.CreateTask)
			taskV1.GET("", taskApi.GetTaskList)
			taskV1.GET("/progress", taskApi.GetTaskProgressBatch)
			taskV1.PUT("/:id", taskApi.UpdateTask)
			taskV1.DELETE("/:id", taskApi.DeleteTask)
			taskV1.POST("/action", taskApi.UpdateTaskStatus)
			taskV1.GET("/:id/progress", taskApi.GetTaskProgress)
		}

		strategyV1 := protected.Group("/strategies")
		{
			strategyV1.POST("", strategyApi.CreateStrategy)
			strategyV1.GET("", strategyApi.GetStrategyList)
			strategyV1.PUT("/:id", strategyApi.UpdateStrategy)
			strategyV1.DELETE("/:id", strategyApi.DeleteStrategy)
		}

		keywordV1 := protected.Group("/keyword-profiles")
		{
			keywordV1.POST("", keywordApi.CreateKeywordProfile)
			keywordV1.GET("", keywordApi.GetKeywordProfileList)
			keywordV1.PUT("/:id", keywordApi.UpdateKeywordProfile)
			keywordV1.DELETE("/:id", keywordApi.DeleteKeywordProfile)
		}

		dashV1 := protected.Group("/dashboard")
		{
			dashV1.GET("/summary", dashboardApi.Summary)
			dashV1.GET("/events", dashboardApi.Events)
		}

		wsV1 := protected.Group("/ws")
		{
			wsV1.GET("/dashboard", dashboardApi.DashboardWS)
		}

		wmV1 := protected.Group("/watermarks")
		{
			wmV1.POST("/upload", watermarkApi.UploadWatermarkPNG)
			wmV1.POST("/fonts/upload", watermarkApi.UploadWatermarkFont)
			wmV1.GET("/files/:name", watermarkApi.GetWatermarkFile)
			wmV1.GET("/fonts/:name", watermarkApi.GetWatermarkFont)
		}
	}

	setupFrontendSPA(r)
	return r
}

func setupFrontendSPA(r *gin.Engine) {
	if r == nil {
		return
	}

	dir := strings.TrimSpace(global.Config.Server.FrontendDir)
	if dir == "" {
		dir = "./frontend_dist"
	}
	fi, err := os.Stat(dir)
	if err != nil || fi == nil || !fi.IsDir() {
		return
	}

	indexPath := filepath.Join(dir, "index.html")
	if st, err := os.Stat(indexPath); err != nil || st == nil || st.IsDir() {
		return
	}

	r.NoRoute(func(c *gin.Context) {
		if c == nil || c.Request == nil || c.Request.URL == nil {
			c.Status(http.StatusNotFound)
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}

		urlPath := strings.TrimSpace(c.Request.URL.Path)
		if urlPath == "" {
			urlPath = "/"
		}
		if urlPath == "/api" || strings.HasPrefix(urlPath, "/api/") {
			c.Status(http.StatusNotFound)
			return
		}

		clean := path.Clean("/" + urlPath)
		clean = strings.TrimPrefix(clean, "/")

		// Root or explicit directory: always return SPA index.
		if clean == "" || strings.HasSuffix(urlPath, "/") {
			c.Header("Cache-Control", "no-cache")
			c.File(indexPath)
			return
		}

		// Try to serve exact file first.
		filePath := filepath.Join(dir, filepath.FromSlash(clean))
		if st, err := os.Stat(filePath); err == nil && st != nil && !st.IsDir() {
			if strings.HasPrefix(clean, "assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				c.Header("Cache-Control", "no-cache")
			}
			c.File(filePath)
			return
		}

		// Missing static asset: do not fall back to index.html.
		if filepath.Ext(clean) != "" {
			c.Status(http.StatusNotFound)
			return
		}

		// SPA route fallback.
		c.Header("Cache-Control", "no-cache")
		c.File(indexPath)
	})
}
