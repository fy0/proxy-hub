# Go Template

该模板集成了 GORM 数据层、zap 日志、koanf 配置以及 Fiber + Huma 的 API 框架，可直接拷贝用于新项目。

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
3. 启动服务：`go run .`
   - 如需强制迁移，可追加 `-m` 或 `--migrate`。

## 测试
- 运行全部测试：`go test ./...`
- 数据层测试默认使用 SQLite 内存库（DSN `:memory:`），用于快速验证完整流程。

## DSN 支持
`utils/model_base.DBInit` 会根据 DSN 自动选择数据库驱动：
- Postgres：`postgres://...` / `postgresql://...`
- MySQL：`mysql://...` 或包含 `@tcp(` 的 DSN
- SQLite：`./data/data.db` / `file:...` / `:memory:`

SQLite 使用 `github.com/glebarez/sqlite`（pure Go，无需 CGO）。

## 目录说明
- `api/`：HTTP & Huma 相关实现。
- `model/`：数据层（GORM 表、初始化）。
- `utils/`：配置、日志、ID 等通用模块。
- `static/`、`docs/`、`data/`：静态资源、自定义文档、运行期数据占位。

## Windows 服务脚本
- 安装：`go run . -i`
- 卸载：`go run . --uninstall`

复制项目后，可按需替换示例模型或移除不需要的模块，使其贴合自身业务。
