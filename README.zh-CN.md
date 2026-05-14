# Proxy Hub

[English](README.md) | [简体中文](README.zh-CN.md)

Proxy Hub 基于 GORM 数据层、zap 日志、koanf 配置以及 Fiber + Huma API 框架搭建，作为代理管理平台的后端与前端基础工程。

## 能力概览
- **配置中心**：`utils/app_config.go` 读取 / 写入 `data/config.yaml`，并提供日志级别、OpenAPI、Huma 文档路径等开关。
- **日志系统**：`utils/logger.go` 使用 zap，支持控制台与文件双输出。
- **数据层**：
  - `model/tables` 存放 GORM 表声明（默认只有 `UserTable` / `UserAccessTokenTable` 示范）。
  - `model` 目录下封装了 GORM 初始化、迁移、关闭等生命周期方法。
- **API 框架**：`api/api.go` 预置 Fiber + Huma 集成，自动挂载 OpenAPI JSON 与自定义 docsPath。
- **工具集**：保留 ID 生成等常用组件。

## 使用步骤
1. 初始化依赖：`go mod tidy`
2. 根据环境修改 `data/config.yaml`（数据库 DSN、日志输出等）。
3. 启动服务：`go run -tags with_utls .`
   - 如需强制迁移，可追加 `-m` 或 `--migrate`。

## 前端
- 安装依赖：`pnpm -C ui install --frozen-lockfile`
- 生成 OpenAPI client：`pnpm -C ui run generate-api`
- 构建前端：`pnpm -C ui run build`

## 测试
- 运行全部测试：`go test -tags with_utls ./...`
- 数据层测试默认使用 SQLite 内存库（DSN `:memory:`），用于快速验证完整流程。

## DSN 支持
`utils/model_base.DBInit` 会根据 DSN 自动选择数据库驱动：
- Postgres：`postgres://...` / `postgresql://...`
- MySQL：`mysql://...` 或包含 `@tcp(` 的 DSN
- SQLite：`./data/data.db` / `file:...` / `:memory:`

SQLite 使用 `github.com/ncruces/go-sqlite3/gormlite`（无需 CGO）。

## 目录说明
- `api/`：HTTP & Huma 相关实现。
- `model/`：数据层（GORM 表、初始化）。
- `utils/`：配置、日志、ID 等通用模块。
- `static/`、`docs/`、`data/`：静态资源、自定义文档、运行期数据占位。

## Windows 服务脚本
- 安装：`go run -tags with_utls . -i`
- 卸载：`go run -tags with_utls . --uninstall`

后续可按 Proxy Hub 的业务需求替换示例用户模型或新增代理节点、订阅、检测等模块。

## Docker / CI
- 本地构建镜像：`docker build -t proxy-hub:local .`
- 本地运行容器：`docker run --rm -p 3020:3020 -v ${PWD}/data:/app/data proxy-hub:local`
- 如需自定义配置，可将宿主机的 `data/` 挂载到容器内 `/app/data`
- `Dockerfile` 会在构建阶段生成 OpenAPI、编译前端并将 `static/` 打包进 Go 二进制
- `.github/workflows/docker-image.yml` 会在 `master`、`v*` tag、PR 或手动触发时构建镜像，并在非 PR 场景推送到 `ghcr.io`
- `.github/workflows/release-dev.yml` 会在 `master` 推送或手动触发时生成 `dev` 预发布压缩包并上传到 GitHub Releases
