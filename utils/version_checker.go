package utils

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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
	cacheFile      string
}

type versionCache struct {
	LastCheck  time.Time `json:"last_check"`
	LatestVer  string    `json:"latest_version"`
	CurrentVer string    `json:"current_version"`
}

type npmRegistry struct {
	DistTags struct {
		Latest string `json:"latest"`
	} `json:"dist-tags"`
}

func NewVersionChecker(currentVersion, packageName string) *VersionChecker {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		userConfigDir = os.TempDir()
	}

	configDir := filepath.Join(userConfigDir, "proxy-hub")
	_ = os.MkdirAll(configDir, 0o755)

	return &VersionChecker{
		currentVersion: currentVersion,
		packageName:    packageName,
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
	latestVersion, err := vc.fetchLatestVersion()
	if err != nil {
		return "", false, err
	}

	current, err := semver.NewVersion(vc.currentVersion)
	if err != nil {
		return latestVersion, false, fmt.Errorf("parse current version: %w", err)
	}
	latest, err := semver.NewVersion(latestVersion)
	if err != nil {
		return latestVersion, false, fmt.Errorf("parse latest version: %w", err)
	}

	return latestVersion, latest.GreaterThan(current), nil
}

func (vc *VersionChecker) Check() {
	cache := vc.loadCache()
	if !vc.shouldCheck(cache) {
		if cache != nil && cache.LatestVer != "" {
			vc.showNotification(cache.LatestVer)
		}
		return
	}

	latestVersion, err := vc.fetchLatestVersion()
	if err != nil {
		if cache != nil && cache.LatestVer != "" {
			vc.showNotification(cache.LatestVer)
		}
		return
	}

	vc.saveCache(&versionCache{
		LastCheck:  time.Now(),
		LatestVer:  latestVersion,
		CurrentVer: vc.currentVersion,
	})
	vc.showNotification(latestVersion)
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
	return time.Since(cache.LastCheck) > versionCheckInterval
}

func (vc *VersionChecker) fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: versionCheckTimeout}
	url := fmt.Sprintf("https://registry.npmjs.org/%s", vc.packageName)

	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("npm registry returned status %d", resp.StatusCode)
	}

	var registry npmRegistry
	if err := json.NewDecoder(resp.Body).Decode(&registry); err != nil {
		return "", err
	}
	if registry.DistTags.Latest == "" {
		return "", fmt.Errorf("npm registry response missing latest dist-tag")
	}
	return registry.DistTags.Latest, nil
}

func (vc *VersionChecker) showNotification(latestVersion string) {
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

	updateCmd := fmt.Sprintf("npm install -g %s@latest", vc.packageName)
	viewLink := fmt.Sprintf("npmjs.com/package/%s", vc.packageName)

	fmt.Println()
	fmt.Println("New version available")
	fmt.Println()
	fmt.Printf("Current: %s    Latest: %s\n", vc.currentVersion, latestVersion)
	fmt.Println()
	fmt.Println("Update command:")
	fmt.Printf("  %s\n", updateCmd)
	fmt.Println()
	fmt.Println("View updates:")
	fmt.Printf("  %s\n", viewLink)
	fmt.Println()
}
