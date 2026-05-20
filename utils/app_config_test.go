package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/knadh/koanf/v2"
)

func TestReadConfigDirectModeUsesLocalDataDir(t *testing.T) {
	tempDir := t.TempDir()
	chdirForTest(t, tempDir)

	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	resetConfigForTest(t, filepath.Join(binDir, "proxy-hub"), filepath.Join(tempDir, "home"))

	cfg := ReadConfig()

	if cfg.DSN != filepath.Join("data", "data.db") {
		t.Fatalf("DSN = %q, want local data db", cfg.DSN)
	}
	if cfg.LogFile != filepath.Join("data", "service.log") {
		t.Fatalf("LogFile = %q, want local service log", cfg.LogFile)
	}
	if _, err := os.Stat(filepath.Join(tempDir, "data", "config.yaml")); err != nil {
		t.Fatalf("local config was not written: %v", err)
	}
}

func TestReadConfigNPMModeUsesHomeDataDir(t *testing.T) {
	tempDir := t.TempDir()
	workDir := filepath.Join(tempDir, "work")
	homeDir := filepath.Join(tempDir, "home")
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(work) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, npmInstallMarkerFileName), nil, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	chdirForTest(t, workDir)
	resetConfigForTest(t, filepath.Join(binDir, "proxy-hub"), homeDir)

	cfg := ReadConfig()

	dataDir := filepath.Join(homeDir, homeDataDirName)
	if cfg.DSN != filepath.Join(dataDir, "data.db") {
		t.Fatalf("DSN = %q, want home data db", cfg.DSN)
	}
	if cfg.LogFile != filepath.Join(dataDir, "service.log") {
		t.Fatalf("LogFile = %q, want home service log", cfg.LogFile)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "config.yaml")); err != nil {
		t.Fatalf("home config was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "data", "config.yaml")); !os.IsNotExist(err) {
		t.Fatalf("workdir config should not be written in npm mode, stat err = %v", err)
	}
}

func resetConfigForTest(t *testing.T, executablePath, homeDir string) {
	t.Helper()

	oldConfigStore := configStore
	oldConfigPath := configPath
	configStore = koanf.New(".")
	configPath = ""
	overrideDataDirHooks(t, executablePath, homeDir)

	t.Cleanup(func() {
		configStore = oldConfigStore
		configPath = oldConfigPath
	})
}

func chdirForTest(t *testing.T, dir string) {
	t.Helper()

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})
}
