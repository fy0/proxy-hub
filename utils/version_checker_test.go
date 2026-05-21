package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewVersionCheckerWithChannelUsesDistTag(t *testing.T) {
	tests := []struct {
		name        string
		channel     string
		wantChannel string
		wantDistTag string
	}{
		{name: "stable", channel: "stable", wantChannel: "stable", wantDistTag: "latest"},
		{name: "empty", channel: "", wantChannel: "stable", wantDistTag: "latest"},
		{name: "release alias", channel: "release", wantChannel: "stable", wantDistTag: "latest"},
		{name: "dev", channel: "dev", wantChannel: "dev", wantDistTag: "dev"},
		{name: "alpha", channel: "alpha", wantChannel: "alpha", wantDistTag: "alpha"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewVersionCheckerWithChannel("1.0.0", "pxhub", tt.channel)
			if checker.channel != tt.wantChannel {
				t.Fatalf("channel = %q, want %q", checker.channel, tt.wantChannel)
			}
			if checker.distTag != tt.wantDistTag {
				t.Fatalf("distTag = %q, want %q", checker.distTag, tt.wantDistTag)
			}
		})
	}
}

func TestVersionCheckerUpdateInfo(t *testing.T) {
	checker := NewVersionCheckerWithChannel("1.0.0", "pxhub", "stable")
	checker.installSource = installSourceNPM
	info := checker.updateInfo("1.0.1", "latest", true)

	if !info.HasUpdate {
		t.Fatalf("HasUpdate = false, want true")
	}
	if info.CurrentVersion != "1.0.0" || info.LatestVersion != "1.0.1" {
		t.Fatalf("version info = %+v", info)
	}
	if info.Channel != "stable" || info.DistTag != "latest" {
		t.Fatalf("channel info = %+v", info)
	}
	if info.UpdateCommand != "npm install -g pxhub@latest" {
		t.Fatalf("UpdateCommand = %q", info.UpdateCommand)
	}
	if info.UpdateURL != "https://github.com/fy0/proxy-hub/releases/tag/v1.0.1" {
		t.Fatalf("UpdateURL = %q", info.UpdateURL)
	}
}

func TestVersionCheckerUpdateInfoBinaryOmitsNPMCommand(t *testing.T) {
	checker := NewVersionCheckerWithChannel("1.0.0", "pxhub", "stable")
	checker.installSource = installSourceBinary
	info := checker.updateInfo("1.0.1", "latest", true)

	if info.UpdateCommand != "" {
		t.Fatalf("UpdateCommand = %q, want empty", info.UpdateCommand)
	}
	if info.UpdateURL != "https://github.com/fy0/proxy-hub/releases/tag/v1.0.1" {
		t.Fatalf("UpdateURL = %q", info.UpdateURL)
	}
}

func TestVersionCheckerUpdateInfoUsesDevReleaseTag(t *testing.T) {
	checker := NewVersionCheckerWithChannel("0.1.0-dev", "pxhub", "dev")
	checker.installSource = installSourceNPM
	info := checker.updateInfo("0.1.1-dev", "dev", true)

	if info.UpdateURL != "https://github.com/fy0/proxy-hub/releases/tag/dev" {
		t.Fatalf("UpdateURL = %q", info.UpdateURL)
	}
	if info.UpdateCommand != "npm install -g pxhub@dev" {
		t.Fatalf("UpdateCommand = %q", info.UpdateCommand)
	}
}

func TestVersionCheckerShouldCheckIncludesPackageAndChannel(t *testing.T) {
	checker := NewVersionCheckerWithChannel("1.0.0", "pxhub", "stable")
	currentCache := &versionCache{
		LastCheck:     time.Now(),
		LatestVer:     "1.0.0",
		CurrentVer:    "1.0.0",
		Package:       "pxhub",
		Channel:       "stable",
		DistTag:       "latest",
		InstallSource: installSourceBinary,
	}
	checker.installSource = installSourceBinary

	if checker.shouldCheck(currentCache) {
		t.Fatalf("shouldCheck current cache = true, want false")
	}

	packageChanged := *currentCache
	packageChanged.Package = "other-package"
	if !checker.shouldCheck(&packageChanged) {
		t.Fatalf("shouldCheck package change = false, want true")
	}

	channelChanged := *currentCache
	channelChanged.Channel = "dev"
	if !checker.shouldCheck(&channelChanged) {
		t.Fatalf("shouldCheck channel change = false, want true")
	}

	installSourceChanged := *currentCache
	installSourceChanged.InstallSource = installSourceNPM
	if !checker.shouldCheck(&installSourceChanged) {
		t.Fatalf("shouldCheck install source change = false, want true")
	}
}

func TestVersionCheckerCheckUpdateInfoCachedUsesCache(t *testing.T) {
	cacheDir := t.TempDir()
	cacheFile := filepath.Join(cacheDir, "version-cache.json")
	cache := versionCache{
		LastCheck:     time.Now(),
		LatestVer:     "1.0.2",
		CurrentVer:    "1.0.1",
		Package:       "pxhub",
		Channel:       "stable",
		DistTag:       "latest",
		InstallSource: installSourceBinary,
	}

	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatalf("marshal cache: %v", err)
	}
	if err := os.WriteFile(cacheFile, data, 0o644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	checker := NewVersionCheckerWithChannel("1.0.1", "pxhub", "stable")
	checker.installSource = installSourceBinary
	checker.cacheFile = cacheFile

	info, err := checker.CheckUpdateInfoCached()
	if err != nil {
		t.Fatalf("CheckUpdateInfoCached() error = %v", err)
	}
	if !info.HasUpdate {
		t.Fatalf("HasUpdate = false, want true")
	}
	if info.LatestVersion != "1.0.2" {
		t.Fatalf("LatestVersion = %q, want %q", info.LatestVersion, "1.0.2")
	}
	if info.UpdateURL != "https://github.com/fy0/proxy-hub/releases/tag/v1.0.2" {
		t.Fatalf("UpdateURL = %q", info.UpdateURL)
	}
	if info.UpdateCommand != "" {
		t.Fatalf("UpdateCommand = %q, want empty", info.UpdateCommand)
	}
}
