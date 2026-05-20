# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS go-base

WORKDIR /build/app
RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

FROM go-base AS openapi-builder

COPY . ./
RUN go run -tags with_utls . --gen-openapi=./ui/openapi.json

FROM --platform=$BUILDPLATFORM node:22-alpine AS ui-builder

WORKDIR /build/ui
COPY ui/package.json ui/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile
COPY ui/ ./
COPY --from=openapi-builder /build/app/ui/openapi.json ./openapi.json
RUN pnpm run generate-client && pnpm run build

FROM go-base AS backend-builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION_MAIN=1.0.1
ARG VERSION_PRERELEASE=
ARG VERSION_BUILD_METADATA=
ARG APP_CHANNEL=stable

COPY . ./
RUN rm -rf ./static/*
COPY --from=ui-builder /build/ui/dist/. ./static/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags with_utls -trimpath \
    -ldflags="-s -w -X 'main.VERSION_MAIN=${VERSION_MAIN}' -X 'main.VERSION_PRERELEASE=${VERSION_PRERELEASE}' -X 'main.VERSION_BUILD_METADATA=${VERSION_BUILD_METADATA}' -X 'main.APP_CHANNEL=${APP_CHANNEL}'" \
    -o /build/proxy-hub .

FROM alpine:3.21 AS runner

ENV TZ=Asia/Shanghai

WORKDIR /app

COPY --from=go-base /etc/ssl /etc/ssl
COPY --from=go-base /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=backend-builder /build/proxy-hub /app/app

EXPOSE 3020
VOLUME ["/app/data"]

ENTRYPOINT ["/app/app"]
