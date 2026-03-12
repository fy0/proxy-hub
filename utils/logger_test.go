package utils

import (
	"bytes"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestNewLoggerRoutesByLevel(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	logger := newLogger("debug", zapcore.AddSync(&stdout), zapcore.AddSync(&stderr))

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	stdoutText := stdout.String()
	stderrText := stderr.String()

	for _, msg := range []string{"debug message", "info message", "warn message"} {
		if !strings.Contains(stdoutText, msg) {
			t.Fatalf("stdout missing %q: %s", msg, stdoutText)
		}
		if strings.Contains(stderrText, msg) {
			t.Fatalf("stderr should not contain %q: %s", msg, stderrText)
		}
	}

	if !strings.Contains(stderrText, "error message") {
		t.Fatalf("stderr missing error log: %s", stderrText)
	}
	if strings.Contains(stdoutText, "error message") {
		t.Fatalf("stdout should not contain error log: %s", stdoutText)
	}
}

func TestNewLoggerHonorsConfiguredLevel(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	logger := newLogger("warn", zapcore.AddSync(&stdout), zapcore.AddSync(&stderr))

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	stdoutText := stdout.String()
	stderrText := stderr.String()

	for _, msg := range []string{"debug message", "info message"} {
		if strings.Contains(stdoutText, msg) || strings.Contains(stderrText, msg) {
			t.Fatalf("disabled log %q should not be emitted; stdout=%s stderr=%s", msg, stdoutText, stderrText)
		}
	}

	if !strings.Contains(stdoutText, "warn message") {
		t.Fatalf("stdout missing warn log: %s", stdoutText)
	}
	if !strings.Contains(stderrText, "error message") {
		t.Fatalf("stderr missing error log: %s", stderrText)
	}
}
