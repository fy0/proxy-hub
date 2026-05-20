#!/usr/bin/env python3
"""Publish all generated ProxyHub npm packages."""
import argparse
import json
import os
import subprocess
import sys
from pathlib import Path


PLATFORMS = [
    "win32-x64",
    "darwin-x64",
    "darwin-arm64",
    "linux-x64",
    "linux-arm64",
]


def run_command(cmd: list[str], cwd: Path | None = None) -> int:
    print(f"\n[exec] {' '.join(cmd)}")
    print(f"[cwd] {cwd if cwd else Path.cwd()}")
    if sys.platform.startswith("win"):
        return subprocess.run(" ".join(cmd), cwd=cwd, shell=True).returncode
    return subprocess.run(cmd, cwd=cwd).returncode


def is_github_actions() -> bool:
    return os.getenv("GITHUB_ACTIONS") == "true"


def read_package_name(package_json: Path) -> tuple[str, str]:
    with package_json.open("r", encoding="utf-8") as f:
        pkg = json.load(f)
    return pkg.get("name", "unknown"), pkg.get("version", "unknown")


def main() -> int:
    parser = argparse.ArgumentParser(description="Publish generated ProxyHub npm packages")
    parser.add_argument("--tag", default="latest", help="npm dist-tag to publish")
    args = parser.parse_args()

    root_dir = Path(__file__).parent.absolute()
    npm_packages_dir = root_dir / "npm-packages"
    main_package_json = root_dir / "package.json"

    print("=" * 60)
    print("Publish ProxyHub npm packages")
    print("=" * 60)

    if not npm_packages_dir.exists():
        print(f"[error] {npm_packages_dir} does not exist")
        print("Run: python build-npm-multipackage.py")
        return 1
    if not main_package_json.exists():
        print(f"[error] {main_package_json} does not exist")
        return 1

    main_package_name, _ = read_package_name(main_package_json)

    print("\n[auth] npm whoami")
    auth_result = run_command(["npm", "whoami"], cwd=root_dir)
    if auth_result != 0:
        if is_github_actions() and os.getenv("NODE_AUTH_TOKEN"):
            print("[auth] npm whoami failed, but NODE_AUTH_TOKEN is set in GitHub Actions")
        else:
            print("[error] npm authentication failed")
            return auth_result

    publish_cmd = ["npm", "publish", "--access", "public", "--tag", args.tag]

    print("\n[1/2] Publish platform packages")
    for platform in PLATFORMS:
        platform_dir = npm_packages_dir / platform
        package_json = platform_dir / "package.json"
        if not package_json.exists():
            print(f"[warn] missing {package_json}; skipping")
            continue

        pkg_name, pkg_version = read_package_name(package_json)
        print(f"\n[publish] {pkg_name}@{pkg_version}")
        ret = run_command(publish_cmd, cwd=platform_dir)
        if ret != 0:
            print(f"[error] failed to publish {pkg_name}")
            return ret

    print("\n[2/2] Publish main package")
    print(f"[publish] {main_package_name}")
    ret = run_command(publish_cmd, cwd=root_dir)
    if ret != 0:
        print("[error] failed to publish main package")
        return ret

    print("\nPublish complete.")
    print(f"Install test: npm install -g {main_package_name}")
    print(f"Package page: https://www.npmjs.com/package/{main_package_name}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
