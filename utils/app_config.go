package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
	"github.com/samber/lo"
)

type AttachmentConfig struct {
	UseS3     bool   `json:"useS3" yaml:"useS3" koanf:"useS3"`
	Endpoint  string `json:"endpoint" yaml:"endpoint" koanf:"endpoint"`
	Bucket    string `json:"bucket" yaml:"bucket" koanf:"bucket"`
	AccessKey string `json:"accessKey" yaml:"accessKey" koanf:"accessKey"`
	SecretKey string `json:"secretKey" yaml:"secretKey" koanf:"secretKey"`
	Token     string `json:"token" yaml:"token" koanf:"token"`
}

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
	ServeAt             string            `json:"serveAt" yaml:"serveAt" koanf:"serveAt"`
	Domain              string            `json:"domain" yaml:"domain" koanf:"domain"`
	RegisterOpen        bool              `json:"registerOpen" yaml:"registerOpen" koanf:"registerOpen"`
	WebUrl              string            `json:"webUrl" yaml:"webUrl" koanf:"webUrl"`
	AttachmentSizeLimit int64             `json:"attachmentSizeLimit" yaml:"attachmentSizeLimit" koanf:"attachmentSizeLimit"`
	ImageCompress       bool              `json:"imageCompress" yaml:"imageCompress" koanf:"imageCompress"`
	LogFile             string            `json:"logFile" yaml:"logFile" koanf:"logFile"`
	LogLevel            string            `json:"logLevel" yaml:"logLevel" koanf:"logLevel"`
	DBLogLevel          int               `json:"dbLogLevel" yaml:"dbLogLevel" koanf:"dbLogLevel"`
	CorsAllowOrigins    string            `json:"corsAllowOrigins" yaml:"corsAllowOrigins" koanf:"corsAllowOrigins"`
	UIOverwrite         string            `json:"uiOverwrite" yaml:"uiOverwrite" koanf:"uiOverwrite"`
	AutoMigrate         bool              `json:"autoMigrate" yaml:"autoMigrate" koanf:"autoMigrate"`
	OpenAPIEnabled      bool              `json:"openapiEnabled" yaml:"openapiEnabled" koanf:"openapiEnabled"`
	DocsPath            string            `json:"docsPath" yaml:"docsPath" koanf:"docsPath"`
	APITitle            string            `json:"apiTitle" yaml:"apiTitle" koanf:"apiTitle"`
	APIVersion          string            `json:"apiVersion" yaml:"apiVersion" koanf:"apiVersion"`
	AttachmentConfig    AttachmentConfig  `json:"attachmentConfig" yaml:"attachmentConfig" koanf:"attachmentConfig"`
	ProxyHealth         ProxyHealthConfig `json:"proxyHealth" yaml:"proxyHealth" koanf:"proxyHealth"`
	DSN                 string            `json:"dbUrl" yaml:"dbUrl" koanf:"dbUrl"`
	PrintConfig         bool              `json:"printConfig" yaml:"printConfig" koanf:"printConfig"`
}

var configStore = koanf.New(".")
var configPath = filepath.Join(".", "data", "config.yaml")

// ReadConfig 会加载 data/config.yaml，若不存在则写入默认配置。
func ReadConfig() *AppConfig {
	defaults := AppConfig{
		ServeAt:             ":3020",
		Domain:              "127.0.0.1:3020",
		RegisterOpen:        true,
		WebUrl:              "/",
		AttachmentSizeLimit: 8192,
		ImageCompress:       true,
		LogFile:             "./data/service.log",
		LogLevel:            "info",
		CorsAllowOrigins:    "*",
		AutoMigrate:         true,
		OpenAPIEnabled:      true,
		DocsPath:            "/docs",
		APITitle:            "Proxy Hub API",
		APIVersion:          "1.0.0",
		AttachmentConfig: AttachmentConfig{
			UseS3: false,
		},
		ProxyHealth: DefaultProxyHealthConfig(),
		DSN:         "./data/data.db",
		PrintConfig: true,
	}

	lo.Must0(configStore.Load(structs.Provider(&defaults, "yaml"), nil))

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

// WriteConfig 会将当前配置写回磁盘，常用于初始化默认配置。
func WriteConfig(config *AppConfig) {
	if config != nil {
		lo.Must0(configStore.Load(structs.Provider(config, "yaml"), nil))
	}

	content, err := yaml.Parser().Marshal(configStore.Raw())
	if err != nil {
		fmt.Println("写入配置失败: 序列化错误")
		return
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		fmt.Println("写入配置失败: 无法创建目录")
		return
	}
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		fmt.Println("写入配置失败: 无法写入文件")
	}
}
