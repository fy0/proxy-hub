package main

import "github.com/Masterminds/semver/v3"

var (
	APPNAME = "ProxyHub"
	VERSION = semver.MustParse(VERSION_MAIN + VERSION_PRERELEASE + VERSION_BUILD_METADATA)

	// VERSION_MAIN is the main semantic version.
	VERSION_MAIN = "0.1.0"
	// VERSION_PRERELEASE is the semantic version prerelease suffix.
	VERSION_PRERELEASE = "-alpha"
	// VERSION_BUILD_METADATA is the semantic version build metadata suffix.
	VERSION_BUILD_METADATA = ""

	// APP_CHANNEL is injected by release builds when needed.
	APP_CHANNEL = "dev" //nolint:revive

	// PACKAGE_NAME is used by the placeholder npm-style update checker.
	PACKAGE_NAME = "proxy-hub"
)
