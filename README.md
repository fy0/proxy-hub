# Proxy Hub

[English](README.md) | [简体中文](README.zh-CN.md)

Proxy Hub is a backend and frontend foundation for a proxy management platform, built with a GORM data layer, zap logging, koanf configuration, and the Fiber + Huma API framework.

## Feature Overview
- **Configuration center**: `utils/app_config.go` reads and writes `data/config.yaml`, and provides switches for log levels, OpenAPI, Huma docs paths, and related options.
- **Logging system**: `utils/logger.go` uses zap and supports both console and file output.
- **Data layer**:
  - `model/tables` contains GORM table declarations (the default examples are `UserTable` and `UserAccessTokenTable`).
  - `model` wraps the GORM initialization, migration, shutdown, and related lifecycle methods.
- **API framework**: `api/api.go` includes the Fiber + Huma integration and automatically mounts OpenAPI JSON plus the custom docs path.
- **Utilities**: common components such as ID generation are kept for reuse.

## Getting Started
1. Initialize dependencies: `go mod tidy`
2. Update `data/config.yaml` for your environment, including the database DSN and log output.
3. Start the service: `go run -tags with_utls .`
   - To force migrations, append `-m` or `--migrate`.

## Frontend
- Install dependencies: `pnpm -C ui install --frozen-lockfile`
- Generate the OpenAPI client: `pnpm -C ui run generate-api`
- Build the frontend: `pnpm -C ui run build`

## Testing
- Run all tests: `go test -tags with_utls ./...`
- Data-layer tests use an in-memory SQLite database by default (DSN `:memory:`) for fast end-to-end flow validation.

## Supported DSNs
`utils/model_base.DBInit` automatically selects the database driver based on the DSN:
- Postgres: `postgres://...` / `postgresql://...`
- MySQL: `mysql://...` or a DSN containing `@tcp(`
- SQLite: `./data/data.db` / `file:...` / `:memory:`

SQLite uses `github.com/ncruces/go-sqlite3/gormlite` and does not require CGO.

## Directory Layout
- `api/`: HTTP and Huma implementation.
- `model/`: data layer, including GORM tables and initialization.
- `utils/`: shared modules for configuration, logging, IDs, and related helpers.
- `static/`, `docs/`, `data/`: placeholders for static assets, custom documentation, and runtime data.

## Windows Service Commands
- Install: `go run -tags with_utls . -i`
- Uninstall: `go run -tags with_utls . --uninstall`

You can later replace the sample user model or add proxy nodes, subscriptions, health checks, and other modules according to Proxy Hub business requirements.

## Docker / CI
- Build a local image: `docker build -t proxy-hub:local .`
- Run a local container: `docker run --rm -p 3020:3020 -v ${PWD}/data:/app/data proxy-hub:local`
- To customize configuration, mount the host `data/` directory to `/app/data` inside the container.
- `Dockerfile` generates OpenAPI output, builds the frontend, and embeds `static/` into the Go binary during the build stage.
- `.github/workflows/docker-image.yml` builds images on `master`, `v*` tags, PRs, or manual runs, and pushes to `ghcr.io` outside PR contexts.
- `.github/workflows/release-dev.yml` creates a `dev` prerelease archive on `master` pushes or manual runs, then uploads it to GitHub Releases.
