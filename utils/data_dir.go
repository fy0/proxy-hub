package utils

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	homeDataDirName          = ".proxy-hub"
	npmInstallMarkerFileName = ".npm-global"
)

var (
	userHomeDirFunc = os.UserHomeDir
	executableFunc  = os.Executable
)

// GetDataDir returns the directory used for runtime-owned config and data files.
func GetDataDir() string {
	if isNPMInstall() {
		if homeDir, err := userHomeDirFunc(); err == nil {
			if homeDir = strings.TrimSpace(homeDir); homeDir != "" {
				return filepath.Join(homeDir, homeDataDirName)
			}
		}
	}
	return filepath.Join(".", "data")
}

func isNPMInstall() bool {
	executablePath, err := executableFunc()
	if err != nil {
		return false
	}
	executablePath = strings.TrimSpace(executablePath)
	if executablePath == "" {
		return false
	}

	info, err := os.Stat(filepath.Join(filepath.Dir(executablePath), npmInstallMarkerFileName))
	return err == nil && !info.IsDir()
}
