package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

type ProxyHealthConfig struct {
	Enabled           bool          `json:"enabled" yaml:"enabled" koanf:"enabled"`
	ProbeURL          string        `json:"probeUrl" yaml:"probeUrl" koanf:"probeUrl"`
	Interval          time.Duration `json:"interval" yaml:"interval" koanf:"interval"`
	Timeout           time.Duration `json:"timeout" yaml:"timeout" koanf:"timeout"`
	FailureThreshold  int           `json:"failureThreshold" yaml:"failureThreshold" koanf:"failureThreshold"`
	BlacklistDuration time.Duration `json:"blacklistDuration" yaml:"blacklistDuration" koanf:"blacklistDuration"`
	MaxConcurrency    int           `json:"maxConcurrency" yaml:"maxConcurrency" koanf:"maxConcurrency"`
}

func DefaultProxyHealthConfig() ProxyHealthConfig {
	return ProxyHealthConfig{
		Enabled:           true,
		ProbeURL:          "https://www.gstatic.com/generate_204",
		Interval:          3 * time.Minute,
		Timeout:           5 * time.Second,
		FailureThreshold:  3,
		BlacklistDuration: 30 * time.Minute,
		MaxConcurrency:    20,
	}
}

type AppConfig struct {
	ServeAt string `json:"serveAt" yaml:"serveAt" koanf:"serveAt"`
	// StaticMountPath 是静态资源的挂载路径，历史配置键名为 webUrl（旧字段名 WebUrl）。
	StaticMountPath string `json:"webUrl" yaml:"webUrl" koanf:"webUrl"`
	// RequestBodyLimitKB 是 HTTP 请求体上限（KB），历史配置键名为 attachmentSizeLimit（旧字段名 AttachmentSizeLimit）。
	RequestBodyLimitKB int64             `json:"attachmentSizeLimit" yaml:"attachmentSizeLimit" koanf:"attachmentSizeLimit"`
	LogFile            string            `json:"logFile" yaml:"logFile" koanf:"logFile"`
	LogLevel           string            `json:"logLevel" yaml:"logLevel" koanf:"logLevel"`
	DBLogLevel         int               `json:"dbLogLevel" yaml:"dbLogLevel" koanf:"dbLogLevel"`
	CorsAllowOrigins   string            `json:"corsAllowOrigins" yaml:"corsAllowOrigins" koanf:"corsAllowOrigins"`
	UIOverwrite        string            `json:"uiOverwrite" yaml:"uiOverwrite" koanf:"uiOverwrite"`
	AutoMigrate        bool              `json:"autoMigrate" yaml:"autoMigrate" koanf:"autoMigrate"`
	OpenAPIEnabled     bool              `json:"openapiEnabled" yaml:"openapiEnabled" koanf:"openapiEnabled"`
	DocsPath           string            `json:"docsPath" yaml:"docsPath" koanf:"docsPath"`
	APITitle           string            `json:"apiTitle" yaml:"apiTitle" koanf:"apiTitle"`
	APIVersion         string            `json:"apiVersion" yaml:"apiVersion" koanf:"apiVersion"`
	ProxyHealth        ProxyHealthConfig `json:"proxyHealth" yaml:"proxyHealth" koanf:"proxyHealth"`
	DSN                string            `json:"dbUrl" yaml:"dbUrl" koanf:"dbUrl"`
	PrintConfig        bool              `json:"printConfig" yaml:"printConfig" koanf:"printConfig"`
}

var configStore = koanf.New(".")
var configPath string
var configMu sync.Mutex

// ReadConfig 会加载当前数据目录下的 config.yaml，若不存在则写入默认配置。
func ReadConfig() *AppConfig {
	dataDir := GetDataDir()
	configPath = filepath.Join(dataDir, "config.yaml")

	defaults := AppConfig{
		ServeAt:            ":3020",
		StaticMountPath:    "/",
		RequestBodyLimitKB: 65536,
		LogFile:            filepath.Join(dataDir, "service.log"),
		LogLevel:           "info",
		CorsAllowOrigins:   "*",
		AutoMigrate:        true,
		OpenAPIEnabled:     true,
		DocsPath:           "/docs",
		APITitle:           "Proxy Hub API",
		APIVersion:         "1.0.0",
		ProxyHealth:        DefaultProxyHealthConfig(),
		DSN:                filepath.Join(dataDir, "data.db"),
		PrintConfig:        true,
	}

	configStore = koanf.New(".")
	if err := configStore.Load(structs.Provider(&defaults, "yaml"), nil); err != nil {
		panic(err)
	}

	provider := file.Provider(configPath)
	if err := configStore.Load(provider, yaml.Parser()); err != nil {
		fmt.Printf("读取配置失败: %v\n", err)
		if os.IsNotExist(err) {
			WriteConfig(&defaults)
		} else {
			os.Exit(1)
		}
	}

	config := defaults
	if err := configStore.Unmarshal("", &config); err != nil {
		fmt.Printf("解析配置失败: %v\n", err)
		os.Exit(1)
	}

	if config.PrintConfig {
		configStore.Print()
	}

	return &config
}

// UpdateConfig 会基于当前已加载配置应用修改，并写回磁盘。
func UpdateConfig(update func(*AppConfig) error) (*AppConfig, error) {
	configMu.Lock()
	defer configMu.Unlock()

	var config AppConfig
	if err := configStore.Unmarshal("", &config); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	if update != nil {
		if err := update(&config); err != nil {
			return nil, err
		}
	}

	if err := writeConfigLocked(&config); err != nil {
		return nil, err
	}
	return &config, nil
}

// SaveConfig 会将配置写回磁盘，并把失败原因返回给调用方。
func SaveConfig(config *AppConfig) error {
	configMu.Lock()
	defer configMu.Unlock()

	return writeConfigLocked(config)
}

// WriteConfig 会将当前配置写回磁盘，常用于初始化默认配置。
func WriteConfig(config *AppConfig) {
	if err := SaveConfig(config); err != nil {
		fmt.Printf("%v\n", err)
	}
}

func writeConfigLocked(config *AppConfig) error {
	if config != nil {
		if err := configStore.Load(structs.Provider(config, "yaml"), nil); err != nil {
			return fmt.Errorf("写入配置失败: 加载配置错误: %w", err)
		}
	}

	content, err := yaml.Parser().Marshal(configStore.Raw())
	if err != nil {
		return fmt.Errorf("写入配置失败: 序列化错误: %w", err)
	}

	targetPath := configPath
	if targetPath == "" {
		targetPath = filepath.Join(GetDataDir(), "config.yaml")
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("写入配置失败: 无法创建目录: %w", err)
	}
	if err := os.WriteFile(targetPath, content, 0o644); err != nil {
		return fmt.Errorf("写入配置失败: 无法写入文件: %w", err)
	}
	return nil
}
