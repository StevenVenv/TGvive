package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"my-go-server/internal/engine"
	"my-go-server/internal/global"
	"my-go-server/internal/initialize"
	"my-go-server/internal/router"

	"go.uber.org/zap"
)

func main() {
	if err := initialize.InitConfig(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "init config failed:", err)
		os.Exit(1)
	}
	initialize.InitLogger()
	defer func() {
		if global.Logger != nil {
			_ = global.Logger.Sync()
		}
	}()
	if err := initialize.InitDB(); err != nil {
		global.Logger.Error("init db failed", zap.Error(err))
		os.Exit(1)
	}
	if err := initialize.EnsureAdminUser(); err != nil {
		global.Logger.Error("init admin user failed", zap.Error(err))
		os.Exit(1)
	}
	if removed, err := engine.CleanupStaleTempMedia(); err != nil {
		global.Logger.Warn("cleanup stale temp media failed", zap.Error(err))
	} else if removed > 0 {
		global.Logger.Info("cleanup stale temp media completed", zap.Int("removed", removed))
	}

	// Start per-task scheduler (time-slot rules, polling quota).
	engine.Scheduler.Start()

	// Start dashboard monitor (CPU/mem/disk/net counters).
	global.StartMonitor()

	r := router.SetupRouter()
	host := strings.TrimSpace(global.Config.Server.Host)
	if host == "" {
		host = "0.0.0.0"
	}
	addr := net.JoinHostPort(host, strconv.Itoa(global.Config.Server.Port))

	readTimeout := 15 * time.Second
	writeTimeout := 60 * time.Second
	idleTimeout := 75 * time.Second
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	configFile := ""
	if global.Viper != nil {
		configFile = strings.TrimSpace(global.Viper.ConfigFileUsed())
	}
	if configFile == "" {
		configFile = "(defaults/search path)"
	}
	global.Logger.Info(
		"server starting",
		zap.String("addr", addr),
		zap.String("mode", strings.TrimSpace(global.Config.Server.Mode)),
		zap.String("config_file", configFile),
	)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.ListenAndServe()
	}()

	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Periodically aggregate task-scoped SQLite stats into dashboard (comment queue pending/success/fail).
	engine.StartDashboardLocalDBSampler(stopCtx)

	select {
	case <-stopCtx.Done():
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			global.Logger.Error("http server stopped", zap.Error(err))
		}
	}

	shutdownTimeout := time.Duration(global.Config.Server.ShutdownTimeoutSec) * time.Second
	if shutdownTimeout <= 0 {
		shutdownTimeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	global.Logger.Info("server shutting down", zap.Duration("timeout", shutdownTimeout))

	engine.Scheduler.Stop()
	global.Stats.StopMonitor()
	engine.Manager.Shutdown(ctx)

	if err := srv.Shutdown(ctx); err != nil {
		global.Logger.Warn("http shutdown failed", zap.Error(err))
	}

	if global.DB != nil {
		if sqlDB, err := global.DB.DB(); err == nil && sqlDB != nil {
			_ = sqlDB.Close()
		}
	}
}
