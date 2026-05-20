#!/usr/bin/env node
const { spawn } = require('child_process');
const path = require('path');

const PLATFORMS = {
  'win32-x64': '@proxy-hub/win32-x64',
  'darwin-x64': '@proxy-hub/darwin-x64',
  'darwin-arm64': '@proxy-hub/darwin-arm64',
  'linux-x64': '@proxy-hub/linux-x64',
  'linux-arm64': '@proxy-hub/linux-arm64',
};

const platform = process.platform;
const arch = process.arch;
const platformKey = `${platform}-${arch}`;
const packageName = PLATFORMS[platformKey];

if (!packageName) {
  console.error(`Unsupported platform: ${platformKey}`);
  console.error('Supported platforms:', Object.keys(PLATFORMS).join(', '));
  process.exit(1);
}

let binPath;
try {
  const packagePath = require.resolve(`${packageName}/package.json`);
  const packageDir = path.dirname(packagePath);
  const binName = platform === 'win32' ? 'proxy-hub.exe' : 'proxy-hub';
  binPath = path.join(packageDir, binName);
} catch (error) {
  console.error(`Failed to find binary for ${platformKey}`);
  console.error(`Make sure ${packageName} is installed.`);
  console.error('');
  console.error('Try one of the following:');
  console.error('  npm uninstall -g proxy-hub && npm install -g proxy-hub');
  console.error('  pnpm uninstall -g proxy-hub && pnpm install -g proxy-hub');
  process.exit(1);
}

const child = spawn(binPath, process.argv.slice(2), {
  stdio: 'inherit',
  windowsHide: false,
});

child.on('error', (error) => {
  console.error('Failed to start binary:', error.message);
  process.exit(1);
});

child.on('exit', (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exit(code || 0);
});
