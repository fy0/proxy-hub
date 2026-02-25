package utils

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger 全局日志实例
var Logger = zap.NewNop()

// InitLogger 初始化全局日志
// 彩色控制台输出，紧凑格式
func InitLogger() {
	config := zap.NewDevelopmentConfig()
	config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05")
	// 使用 | 分隔，更紧凑
	config.EncoderConfig.ConsoleSeparator = " | "
	// 禁用 caller（在消息中手动添加 handler 位置）
	config.DisableCaller = true

	var err error
	Logger, err = config.Build()
	if err != nil {
		Logger = zap.NewNop()
	}
}

// init 确保 Logger 不为 nil
func init() {
	InitLogger()
}
