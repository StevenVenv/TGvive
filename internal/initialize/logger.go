package initialize

import (
	"os"

	"my-go-server/internal/global"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

func InitLogger() {
	_ = os.MkdirAll("logs", 0o755)

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderCfg.EncodeLevel = zapcore.LowercaseLevelEncoder

	encoder := zapcore.NewJSONEncoder(encoderCfg)

	logFile := &lumberjack.Logger{
		Filename:   "logs/app.log",
		MaxSize:    50,
		MaxBackups: 10,
		MaxAge:     28,
		Compress:   true,
	}

	level := zapcore.InfoLevel
	if global.Config.Server.Mode == "debug" {
		level = zapcore.DebugLevel
	}

	core := zapcore.NewTee(
		zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level),
		zapcore.NewCore(encoder, zapcore.AddSync(logFile), level),
	)

	logger := zap.New(core, zap.AddCaller())
	global.Logger = logger
	zap.ReplaceGlobals(logger)
}
