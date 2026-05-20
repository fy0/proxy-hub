#!/usr/bin/env python3
"""
Build npm packages for ProxyHub using a main package plus platform-specific
optional dependency packages.
"""
import argparse
import json
import os
import shutil
import subprocess
import sys
from pathlib import Path


PACKAGE_NAME = "pxhub"
PLATFORM_PACKAGE_SCOPE = "@proxy-hub"
APP_NAME = "ProxyHub"
REPOSITORY_URL = "https://github.com/fy0/proxy-hub"
HOMEPAGE_URL = "https://github.com/fy0/proxy-hub#readme"
DESCRIPTION = "Simple proxy format conversion tool with local SOCKS5/HTTP endpoints."
PLATFORMS = [
    ("linux", "amd64", "linux", "x64"),
    ("linux", "arm64", "linux", "arm64"),
    ("darwin", "amd64", "darwin", "x64"),
    ("darwin", "arm64", "darwin", "arm64"),
    ("windows", "amd64", "win32", "x64"),
]


def run_command(cmd: list[str], cwd: Path | None = None, shell: bool = False, env: dict[str, str] | None = None) -> int:
    """Run a command and stream output."""
    if shell:
        print(f"[exec] {cmd[0]}")
        result = subprocess.run(cmd[0], cwd=cwd, shell=True, env=env)
    else:
        print(f"[exec] {' '.join(cmd)}")
        result = subprocess.run(cmd, cwd=cwd, env=env)
    return result.returncode


def clean_static_dir(static_dir: Path) -> None:
    """Clean static output while keeping the directory-level README."""
    print(f"[clean] {static_dir}")
    static_dir.mkdir(parents=True, exist_ok=True)
    for item in static_dir.iterdir():
        if item.name == "README.md":
            continue
        if item.is_dir():
            shutil.rmtree(item)
        else:
            item.unlink()


def copy_dist_to_static(dist_dir: Path, static_dir: Path) -> bool:
    """Copy the built UI dist into static."""
    if not dist_dir.exists():
        print(f"[error] {dist_dir} does not exist; build the UI first")
        return False

    print(f"[copy] {dist_dir} -> {static_dir}")
    for item in dist_dir.iterdir():
        dest = static_dir / item.name
        if item.is_dir():
            if dest.exists():
                shutil.rmtree(dest)
            shutil.copytree(item, dest)
        else:
            shutil.copy2(item, dest)
    return True


def platform_package_name(base_name: str, platform_key: str) -> str:
    _ = base_name
    return f"{PLATFORM_PACKAGE_SCOPE}/{platform_key}"


def make_ldflags(version_main: str, version_prerelease: str, version_build_metadata: str, app_channel: str) -> str:
    parts = ["-s", "-w"]
    if version_main:
        parts.append(f"-X 'main.VERSION_MAIN={version_main}'")
    parts.append(f"-X 'main.VERSION_PRERELEASE={version_prerelease}'")
    if version_build_metadata:
        parts.append(f"-X 'main.VERSION_BUILD_METADATA={version_build_metadata}'")
    if app_channel:
        parts.append(f"-X 'main.APP_CHANNEL={app_channel}'")
    return " ".join(parts)


def build_go_multiplatform(
    root_dir: Path,
    npm_packages_dir: Path,
    version_main: str,
    version_prerelease: str,
    version_build_metadata: str,
    app_channel: str,
) -> int:
    print("\n[3/5] Build Go binaries")
    ldflags = make_ldflags(version_main, version_prerelease, version_build_metadata, app_channel)
    print(
        "[version] "
        f"VERSION_MAIN={version_main} "
        f"VERSION_PRERELEASE={version_prerelease} "
        f"VERSION_BUILD_METADATA={version_build_metadata} "
        f"APP_CHANNEL={app_channel}"
    )

    total_size = 0
    for goos, goarch, npm_os, npm_arch in PLATFORMS:
        platform_key = f"{npm_os}-{npm_arch}"
        output_name = "proxy-hub.exe" if goos == "windows" else "proxy-hub"
        platform_dir = npm_packages_dir / platform_key
        platform_dir.mkdir(parents=True, exist_ok=True)
        output_path = platform_dir / output_name

        env = os.environ.copy()
        env["GOOS"] = goos
        env["GOARCH"] = goarch
        env["CGO_ENABLED"] = "0"

        cmd = [
            "go",
            "build",
            "-tags",
            "with_utls",
            f"-ldflags={ldflags}",
            "-trimpath",
            "-o",
            str(output_path),
            ".",
        ]
        print(f"[build] {goos}/{goarch} -> {platform_key}")
        result = subprocess.run(cmd, cwd=root_dir, env=env)
        if result.returncode != 0:
            print(f"[error] Go build failed for {goos}/{goarch}")
            return result.returncode

        size = output_path.stat().st_size
        total_size += size
        print(f"  ok {output_name} ({size / (1024 * 1024):.2f} MB)")

    print(f"[ok] built {len(PLATFORMS)} platforms, total {total_size / (1024 * 1024):.2f} MB")
    return 0


def create_platform_packages(npm_packages_dir: Path, version: str, base_name: str) -> None:
    print("\n[4/5] Create platform package manifests")
    for _, _, npm_os, npm_arch in PLATFORMS:
        platform_key = f"{npm_os}-{npm_arch}"
        platform_dir = npm_packages_dir / platform_key
        platform_dir.mkdir(parents=True, exist_ok=True)
        (platform_dir / ".npm-global").touch()

        pkg = {
            "name": platform_package_name(base_name, platform_key),
            "version": version,
            "description": f"Platform-specific binary for {APP_NAME} on {platform_key}. Install '{base_name}' instead.",
            "os": [npm_os],
            "cpu": [npm_arch],
            "homepage": HOMEPAGE_URL,
            "repository": {
                "type": "git",
                "url": REPOSITORY_URL,
            },
            "author": "fy0",
            "license": "GPL-3.0-or-later",
        }
        with (platform_dir / "package.json").open("w", encoding="utf-8", newline="\n") as f:
            json.dump(pkg, f, indent=2, ensure_ascii=False)
            f.write("\n")
        print(f"  {pkg['name']}")


def create_launcher(root_dir: Path, base_name: str) -> None:
    print("\n[5/5] Create main package files")
    platforms_mapping = "\n".join(
        f"  '{npm_os}-{npm_arch}': '{platform_package_name(base_name, f'{npm_os}-{npm_arch}')}',"
        for _, _, npm_os, npm_arch in PLATFORMS
    )
    script = f"""#!/usr/bin/env node
const {{ spawn }} = require('child_process');
const path = require('path');

const PLATFORMS = {{
{platforms_mapping}
}};

const platform = process.platform;
const arch = process.arch;
const platformKey = `${{platform}}-${{arch}}`;
const packageName = PLATFORMS[platformKey];

if (!packageName) {{
  console.error(`Unsupported platform: ${{platformKey}}`);
  console.error('Supported platforms:', Object.keys(PLATFORMS).join(', '));
  process.exit(1);
}}

let binPath;
try {{
  const packagePath = require.resolve(`${{packageName}}/package.json`);
  const packageDir = path.dirname(packagePath);
  const binName = platform === 'win32' ? 'proxy-hub.exe' : 'proxy-hub';
  binPath = path.join(packageDir, binName);
}} catch (error) {{
  console.error(`Failed to find binary for ${{platformKey}}`);
  console.error(`Make sure ${{packageName}} is installed.`);
  console.error('');
  console.error('Try one of the following:');
  console.error('  npm uninstall -g {base_name} && npm install -g {base_name}');
  console.error('  pnpm uninstall -g {base_name} && pnpm install -g {base_name}');
  process.exit(1);
}}

const child = spawn(binPath, process.argv.slice(2), {{
  stdio: 'inherit',
  windowsHide: false,
}});

child.on('error', (error) => {{
  console.error('Failed to start binary:', error.message);
  process.exit(1);
}});

child.on('exit', (code, signal) => {{
  if (signal) {{
    process.kill(process.pid, signal);
    return;
  }}
  process.exit(code || 0);
}});
"""
    bin_dir = root_dir / "npm-bin"
    bin_dir.mkdir(exist_ok=True)
    with (bin_dir / "proxy-hub.js").open("w", encoding="utf-8", newline="\n") as f:
        f.write(script)


def create_main_package(root_dir: Path, version: str, base_name: str) -> None:
    create_launcher(root_dir, base_name)
    optional_deps = {
        platform_package_name(base_name, f"{npm_os}-{npm_arch}"): version
        for _, _, npm_os, npm_arch in PLATFORMS
    }
    pkg = {
        "name": base_name,
        "version": version,
        "description": DESCRIPTION,
        "bin": {
            "pxhub": "npm-bin/proxy-hub.js",
            "proxy-hub": "npm-bin/proxy-hub.js",
        },
        "scripts": {
            "npm:build": "python build-npm-multipackage.py",
            "npm:publish:all": "python publish-all-packages.py",
            "npm:pack:dry-run": "npm pack --dry-run",
        },
        "optionalDependencies": optional_deps,
        "keywords": [
            "proxy",
            "pxhub",
            "proxy-hub",
            "socks5",
            "http-proxy",
            "sing-box",
            "developer-tools",
        ],
        "author": "fy0",
        "license": "GPL-3.0-or-later",
        "repository": {
            "type": "git",
            "url": REPOSITORY_URL,
        },
        "homepage": HOMEPAGE_URL,
        "engines": {
            "node": ">=14.0.0",
        },
    }
    with (root_dir / "package.json").open("w", encoding="utf-8", newline="\n") as f:
        json.dump(pkg, f, indent=2, ensure_ascii=False)
        f.write("\n")
    print(f"  main package: {base_name}@{version}")


def main() -> int:
    parser = argparse.ArgumentParser(description="Build ProxyHub npm packages")
    parser.add_argument("--version", default="1.0.1", help="npm package version")
    parser.add_argument("--package-name", default=PACKAGE_NAME, help="main npm package name")
    parser.add_argument("--version-main", default="1.0.1", help="VERSION_MAIN injected into the Go binary")
    parser.add_argument("--version-prerelease", default="", help="VERSION_PRERELEASE injected into the Go binary")
    parser.add_argument("--version-build-metadata", default="", help="VERSION_BUILD_METADATA injected into the Go binary")
    parser.add_argument("--app-channel", default="stable", help="APP_CHANNEL injected into the Go binary")
    args = parser.parse_args()

    root_dir = Path(__file__).parent.absolute()
    ui_dir = root_dir / "ui"
    dist_dir = ui_dir / "dist"
    static_dir = root_dir / "static"
    npm_packages_dir = root_dir / "npm-packages"

    print("=" * 60)
    print("ProxyHub npm multi-package build")
    print("=" * 60)
    print(f"package: {args.package_name}")
    print(f"version: {args.version}")

    if npm_packages_dir.exists():
        print(f"[clean] {npm_packages_dir}")
        shutil.rmtree(npm_packages_dir)
    npm_packages_dir.mkdir()

    print("\n[1/5] Build UI")
    if not ui_dir.exists():
        print(f"[error] {ui_dir} does not exist")
        return 1
    if sys.platform.startswith("win"):
        ret = run_command(["pnpm build"], cwd=ui_dir, shell=True)
    else:
        ret = run_command(["pnpm", "build"], cwd=ui_dir)
    if ret != 0:
        print("[error] UI build failed")
        return ret

    print("\n[2/5] Copy UI dist into static")
    clean_static_dir(static_dir)
    if not copy_dist_to_static(dist_dir, static_dir):
        return 1

    ret = build_go_multiplatform(
        root_dir,
        npm_packages_dir,
        version_main=args.version_main,
        version_prerelease=args.version_prerelease,
        version_build_metadata=args.version_build_metadata,
        app_channel=args.app_channel,
    )
    if ret != 0:
        return ret

    create_platform_packages(npm_packages_dir, args.version, args.package_name)
    create_main_package(root_dir, args.version, args.package_name)

    print("\nBuild complete.")
    print("Publish with: python publish-all-packages.py")
    return 0


if __name__ == "__main__":
    sys.exit(main())
