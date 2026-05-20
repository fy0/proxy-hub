package main

import (
	"context"
	"embed"
	"fmt"
	"os"

	"github.com/jessevdk/go-flags"
	"go.uber.org/zap"

	"proxy-hub/api"
	"proxy-hub/model"
	"proxy-hub/utils"
)

//go:embed all:static
var embedStatic embed.FS

func main() {
	var opts struct {
		Version      bool   `short:"v" long:"version" description:"显示版本信息"`
		Install      bool   `short:"i" long:"install" description:"安装为系统服务"`
		Uninstall    bool   `long:"uninstall" description:"卸载系统服务"`
		ForceMigrate bool   `short:"m" long:"migrate" description:"强制执行数据库迁移"`
		MigrateOnly  bool   `long:"migrate-only" description:"仅执行数据库迁移后退出"`
		CompactDB    bool   `long:"compact-db" description:"清理软删除数据并压缩 SQLite 数据库后退出"`
		GenOpenAPI   string `long:"gen-openapi" description:"生成 OpenAPI JSON 文件后退出，可选指定输出路径（默认 ./openapi.json）" optional:"true" optional-value:"./openapi.json"`
	}

	if _, err := flags.ParseArgs(&opts, os.Args); err != nil {
		return
	}

	if opts.Version {
		fmt.Printf("%s v%s\n", APPNAME, VERSION.String())
		fmt.Printf("Channel: %s\n", APP_CHANNEL)
		return
	}

	if opts.Install {
		serviceInstall(true)
		return
	}

	if opts.Uninstall {
		serviceInstall(false)
		return
	}

	run(opts.ForceMigrate || opts.MigrateOnly, opts.MigrateOnly, opts.CompactDB, opts.GenOpenAPI)
}

func run(forceMigrate, migrateOnly, compactDB bool, genOpenAPI string) {
	cfg := utils.ReadConfig()
	if forceMigrate {
		cfg.AutoMigrate = true
	}

	utils.InitLogger(cfg.LogLevel)
	logger := utils.Logger

	if err := model.InitWithDSN(cfg.DSN, cfg.DBLogLevel, cfg.AutoMigrate); err != nil {
		logger.Fatal("初始化数据层失败", zap.Error(err))
	}
	defer model.DBClose()

	if compactDB {
		result, err := model.CompactDB(context.Background(), cfg.DSN)
		if err != nil {
			logger.Fatal("数据库压缩失败", zap.Error(err))
		}
		logDBCompactResult(logger, result)
		return
	}

	// 如果只是迁移模式，完成迁移后退出
	if migrateOnly {
		logger.Info("数据库迁移完成，退出程序")
		return
	}

	// 如果是生成 OpenAPI 模式，生成后退出
	if genOpenAPI != "" {
		outputPath := genOpenAPI
		if outputPath == "" {
			outputPath = "./openapi.json"
		}
		api.GenOpenAPI(context.Background(), cfg, embedStatic, outputPath, defaultAppInfo())
		logger.Info("OpenAPI JSON 生成完成", zap.String("path", outputPath))
		return
	}

	logger.Info("服务启动中", zap.String("listen", cfg.ServeAt))
	checker := utils.NewVersionCheckerWithChannel(VERSION.String(), PACKAGE_NAME, APP_CHANNEL)
	checker.CheckAsync()

	if err := api.Init(context.Background(), cfg, embedStatic, defaultAppInfo()); err != nil {
		logger.Fatal("服务启动失败", zap.Error(err))
	}
}

func logDBCompactResult(logger *zap.Logger, result *model.DBCompactResult) {
	if result == nil {
		logger.Info("数据库压缩完成")
		return
	}
	fields := []zap.Field{
		zap.Int64("beforeBytes", result.BeforeBytes),
		zap.Int64("afterBytes", result.AfterBytes),
		zap.Int64("savedBytes", result.BeforeBytes-result.AfterBytes),
	}
	for _, table := range result.Tables {
		fields = append(fields, zap.Int64(table.TableName+"Deleted", table.Deleted))
	}
	logger.Info("数据库压缩完成", fields...)
}

func defaultAppInfo() *api.AppInfo {
	return &api.AppInfo{
		Name:        APPNAME,
		Version:     VERSION.String(),
		Channel:     APP_CHANNEL,
		PackageName: PACKAGE_NAME,
	}
}
