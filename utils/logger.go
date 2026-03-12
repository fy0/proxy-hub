package utils

import (
	"io"
	"os"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger 全局日志实例
var Logger = zap.NewNop()

// InitLogger 初始化全局日志。
// 普通日志输出到 stdout，错误及以上级别输出到 stderr。
func InitLogger(levels ...string) {
	level := "info"
	if len(levels) > 0 && strings.TrimSpace(levels[0]) != "" {
		level = levels[0]
	}

	Logger = newLogger(level, zapcore.Lock(os.Stdout), zapcore.Lock(os.Stderr))
}

func newLogger(level string, stdout, stderr zapcore.WriteSyncer) *zap.Logger {
	if stdout == nil {
		stdout = zapcore.AddSync(io.Discard)
	}
	if stderr == nil {
		stderr = zapcore.AddSync(io.Discard)
	}

	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	encoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05")
	encoderConfig.ConsoleSeparator = " | "

	logLevel := parseLogLevel(level)
	stdoutPriority := zap.LevelEnablerFunc(func(entryLevel zapcore.Level) bool {
		return logLevel.Enabled(entryLevel) && entryLevel < zapcore.ErrorLevel
	})
	stderrPriority := zap.LevelEnablerFunc(func(entryLevel zapcore.Level) bool {
		return logLevel.Enabled(entryLevel) && entryLevel >= zapcore.ErrorLevel
	})

	core := zapcore.NewTee(
		zapcore.NewCore(zapcore.NewConsoleEncoder(encoderConfig), stdout, stdoutPriority),
		zapcore.NewCore(zapcore.NewConsoleEncoder(encoderConfig), stderr, stderrPriority),
	)

	return zap.New(
		core,
		zap.Development(),
		zap.ErrorOutput(stderr),
		zap.AddStacktrace(zapcore.WarnLevel),
	)
}

func parseLogLevel(level string) zapcore.Level {
	parsedLevel := zapcore.InfoLevel
	if err := parsedLevel.Set(strings.ToLower(strings.TrimSpace(level))); err != nil {
		return zapcore.InfoLevel
	}
	return parsedLevel
}

// init 确保 Logger 不为 nil
func init() {
	InitLogger()
}
