package utils

import (
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
	if info.UpdateURL != "https://www.npmjs.com/package/pxhub" {
		t.Fatalf("UpdateURL = %q", info.UpdateURL)
	}
}

func TestVersionCheckerShouldCheckIncludesPackageAndChannel(t *testing.T) {
	checker := NewVersionCheckerWithChannel("1.0.0", "pxhub", "stable")
	currentCache := &versionCache{
		LastCheck:  time.Now(),
		LatestVer:  "1.0.0",
		CurrentVer: "1.0.0",
		Package:    "pxhub",
		Channel:    "stable",
		DistTag:    "latest",
	}

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
}
