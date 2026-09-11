'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

function resolveBinaryPath(options = {}) {
  if (options.binaryPath) {
    return options.binaryPath;
  }
  const packageRoot = options.packageRoot || path.resolve(__dirname, '..');
  const binaryName = (options.platform || process.platform) === 'win32' ? 'volclog-human.exe' : 'volclog-human';
  return path.join(packageRoot, '.volclog-human', 'bin', binaryName);
}

function runCLI(options = {}) {
  const argv = options.argv || process.argv.slice(2);
  const packageRoot = options.packageRoot || path.resolve(__dirname, '..');
  const binaryPath = resolveBinaryPath({ ...options, packageRoot });
  const spawnImpl = options.spawnImpl || spawnSync;
  const existsImpl = options.existsImpl || fs.existsSync;
  if (!existsImpl(binaryPath)) {
    throw new Error(
      `volclog-human binary not found: ${binaryPath}. Reinstall package or run npm rebuild @volcengine-tls/volclog-human.`,
    );
  }
  const env = {
    ...(options.env || process.env),
    VOLCLOG_INSTALL_METHOD: 'npm',
    VOLCLOG_NPM_PACKAGE: '@volcengine-tls/volclog-human',
    VOLCLOG_NPM_PACKAGE_ROOT: packageRoot,
  };
  const result = spawnImpl(binaryPath, argv, { stdio: 'inherit', env });
  if (result.error) {
    throw result.error;
  }
  return typeof result.status === 'number' ? result.status : 1;
}

module.exports = {
  resolveBinaryPath,
  runCLI,
};
