package main

import (
	"fmt"

	"my-go-server/internal/engine"
	"my-go-server/internal/global"
	"my-go-server/internal/initialize"
	"my-go-server/internal/router"

	"go.uber.org/zap"
)

func main() {
	initialize.InitConfig()
	initialize.InitLogger()
	defer func() {
		if global.Logger != nil {
			_ = global.Logger.Sync()
		}
	}()
	initialize.InitDB()

	// Start per-task scheduler (time-slot rules, polling quota).
	engine.Scheduler.Start()

	// Start dashboard monitor (CPU/mem/disk/net counters).
	global.StartMonitor()

	r := router.SetupRouter()
	addr := fmt.Sprintf(":%d", global.Config.Server.Port)
	global.Logger.Info("server starting", zap.String("addr", addr))
	if err := r.Run(addr); err != nil {
		panic(err)
	}
}
