package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

const (
	versionCheckInterval = 24 * time.Hour
	versionCheckTimeout  = 5 * time.Second
)

type VersionChecker struct {
	currentVersion string
	packageName    string
	channel        string
	distTag        string
	cacheFile      string
}

type versionCache struct {
	LastCheck  time.Time `json:"last_check"`
	LatestVer  string    `json:"latest_version"`
	CurrentVer string    `json:"current_version"`
	Package    string    `json:"package"`
	Channel    string    `json:"channel"`
	DistTag    string    `json:"dist_tag"`
}

type npmRegistry struct {
	DistTags map[string]string `json:"dist-tags"`
}

type UpdateInfo struct {
	CurrentVersion string
	LatestVersion  string
	HasUpdate      bool
	PackageName    string
	Channel        string
	DistTag        string
	UpdateURL      string
	UpdateCommand  string
}

func NewVersionChecker(currentVersion, packageName string) *VersionChecker {
	return NewVersionCheckerWithChannel(currentVersion, packageName, "")
}

func NewVersionCheckerWithChannel(currentVersion, packageName, channel string) *VersionChecker {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		userConfigDir = os.TempDir()
	}

	configDir := filepath.Join(userConfigDir, "proxy-hub")
	_ = os.MkdirAll(configDir, 0o755)

	normalizedChannel := normalizeUpdateChannel(channel)
	distTag := updateDistTag(normalizedChannel)

	return &VersionChecker{
		currentVersion: currentVersion,
		packageName:    packageName,
		channel:        normalizedChannel,
		distTag:        distTag,
		cacheFile:      filepath.Join(configDir, "version-cache.json"),
	}
}

func (vc *VersionChecker) CheckAsync() {
	go func() {
		defer func() {
			_ = recover()
		}()
		vc.Check()
	}()
}

func (vc *VersionChecker) CheckUpdate() (string, bool, error) {
	info, err := vc.CheckUpdateInfo()
	if err != nil {
		return "", false, err
	}
	return info.LatestVersion, info.HasUpdate, nil
}

func (vc *VersionChecker) CheckUpdateInfo() (*UpdateInfo, error) {
	latestVersion, distTag, err := vc.fetchLatestVersion()
	if err != nil {
		return nil, err
	}
	current, err := semver.NewVersion(vc.currentVersion)
	if err != nil {
		return nil, fmt.Errorf("parse current version: %w", err)
	}
	latest, err := semver.NewVersion(latestVersion)
	if err != nil {
		return nil, fmt.Errorf("parse latest version: %w", err)
	}

	return vc.updateInfo(latestVersion, distTag, latest.GreaterThan(current)), nil
}

func (vc *VersionChecker) Check() {
	cache := vc.loadCache()
	if !vc.shouldCheck(cache) {
		if cache != nil && cache.LatestVer != "" {
			distTag := cache.DistTag
			if distTag == "" {
				distTag = vc.distTag
			}
			vc.showNotification(cache.LatestVer, distTag)
		}
		return
	}

	latestVersion, distTag, err := vc.fetchLatestVersion()
	if err != nil {
		if cache != nil && cache.LatestVer != "" {
			distTag := cache.DistTag
			if distTag == "" {
				distTag = vc.distTag
			}
			vc.showNotification(cache.LatestVer, distTag)
		}
		return
	}

	vc.saveCache(&versionCache{
		LastCheck:  time.Now(),
		LatestVer:  latestVersion,
		CurrentVer: vc.currentVersion,
		Package:    vc.packageName,
		Channel:    vc.channel,
		DistTag:    distTag,
	})
	vc.showNotification(latestVersion, distTag)
}

func (vc *VersionChecker) loadCache() *versionCache {
	data, err := os.ReadFile(vc.cacheFile)
	if err != nil {
		return nil
	}

	var cache versionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	return &cache
}

func (vc *VersionChecker) saveCache(cache *versionCache) {
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(vc.cacheFile, data, 0o644)
}

func (vc *VersionChecker) shouldCheck(cache *versionCache) bool {
	if cache == nil {
		return true
	}
	if cache.CurrentVer != vc.currentVersion {
		return true
	}
	if cache.Package != vc.packageName {
		return true
	}
	if cache.Channel != vc.channel {
		return true
	}
	return time.Since(cache.LastCheck) > versionCheckInterval
}

func (vc *VersionChecker) fetchLatestVersion() (string, string, error) {
	client := &http.Client{Timeout: versionCheckTimeout}
	registryURL := fmt.Sprintf("https://registry.npmjs.org/%s", url.PathEscape(vc.packageName))

	resp, err := client.Get(registryURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", "", fmt.Errorf("npm registry returned status %d", resp.StatusCode)
	}

	var registry npmRegistry
	if err := json.NewDecoder(resp.Body).Decode(&registry); err != nil {
		return "", "", err
	}

	latestVersion, distTag := registry.DistTags[vc.distTag], vc.distTag
	if latestVersion == "" && vc.distTag != "latest" {
		latestVersion, distTag = registry.DistTags["latest"], "latest"
	}
	if latestVersion == "" {
		return "", "", fmt.Errorf("npm registry response missing %q dist-tag", vc.distTag)
	}
	return latestVersion, distTag, nil
}

func (vc *VersionChecker) showNotification(latestVersion, distTag string) {
	if latestVersion == "" || latestVersion == vc.currentVersion {
		return
	}

	current, err := semver.NewVersion(vc.currentVersion)
	if err != nil {
		return
	}
	latest, err := semver.NewVersion(latestVersion)
	if err != nil {
		return
	}
	if !latest.GreaterThan(current) {
		return
	}

	info := vc.updateInfo(latestVersion, distTag, true)

	fmt.Println()
	fmt.Println("New version available")
	fmt.Println()
	fmt.Printf("Current: %s    Latest: %s\n", vc.currentVersion, latestVersion)
	fmt.Printf("Channel: %s    npm tag: %s\n", vc.channel, distTag)
	fmt.Println()
	fmt.Println("Update command:")
	fmt.Printf("  %s\n", info.UpdateCommand)
	fmt.Println()
	fmt.Println("View updates:")
	fmt.Printf("  %s\n", info.UpdateURL)
	fmt.Println()
}

func (vc *VersionChecker) updateInfo(latestVersion, distTag string, hasUpdate bool) *UpdateInfo {
	if distTag == "" {
		distTag = vc.distTag
	}
	return &UpdateInfo{
		CurrentVersion: vc.currentVersion,
		LatestVersion:  latestVersion,
		HasUpdate:      hasUpdate,
		PackageName:    vc.packageName,
		Channel:        vc.channel,
		DistTag:        distTag,
		UpdateURL:      "https://www.npmjs.com/package/" + url.PathEscape(vc.packageName),
		UpdateCommand:  fmt.Sprintf("npm install -g %s@%s", vc.packageName, distTag),
	}
}

func normalizeUpdateChannel(channel string) string {
	normalized := strings.ToLower(strings.TrimSpace(channel))
	switch normalized {
	case "", "release", "latest":
		return "stable"
	default:
		return normalized
	}
}

func updateDistTag(channel string) string {
	switch channel {
	case "stable":
		return "latest"
	default:
		return channel
	}
}
