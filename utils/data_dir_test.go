package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetDataDirDefaultsToLocalDataWithoutNPMMarker(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	overrideDataDirHooks(t, filepath.Join(binDir, "proxy-hub"), filepath.Join(tempDir, "home"))

	got := GetDataDir()
	want := filepath.Join(".", "data")
	if got != want {
		t.Fatalf("GetDataDir() = %q, want %q", got, want)
	}
}

func TestGetDataDirUsesHomeForNPMMarker(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, npmInstallMarkerFileName), nil, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	homeDir := filepath.Join(tempDir, "home")

	overrideDataDirHooks(t, filepath.Join(binDir, "proxy-hub"), homeDir)

	got := GetDataDir()
	want := filepath.Join(homeDir, homeDataDirName)
	if got != want {
		t.Fatalf("GetDataDir() = %q, want %q", got, want)
	}
}

func overrideDataDirHooks(t *testing.T, executablePath, homeDir string) {
	t.Helper()

	oldExecutableFunc := executableFunc
	oldUserHomeDirFunc := userHomeDirFunc
	executableFunc = func() (string, error) {
		return executablePath, nil
	}
	userHomeDirFunc = func() (string, error) {
		return homeDir, nil
	}
	t.Cleanup(func() {
		executableFunc = oldExecutableFunc
		userHomeDirFunc = oldUserHomeDirFunc
	})
}
