'use strict';

const crypto = require('node:crypto');
const fs = require('node:fs');
const fsp = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { Readable } = require('node:stream');
const { pipeline } = require('node:stream/promises');

const REPO_BASE_URL = 'https://github.com/volcengine-tls/ve-tls-cli/releases';

function normalizePlatform(platform) {
  switch (platform) {
    case 'darwin':
    case 'linux':
      return platform;
    case 'win32':
      return 'windows';
    default:
      throw new Error(`unsupported platform: ${platform}`);
  }
}

function normalizeArch(arch) {
  switch (arch) {
    case 'x64':
      return 'amd64';
    case 'arm64':
      return 'arm64';
    default:
      throw new Error(`unsupported arch: ${arch}`);
  }
}

function releaseBaseURL(pkgVersion, env) {
  if (env.VOLCLOG_BASE_URL) {
    return env.VOLCLOG_BASE_URL.replace(/\/+$/, '');
  }
  const version = String(env.VOLCLOG_VERSION || pkgVersion || '').trim();
  if (!version || version.includes('dev')) {
    return `${REPO_BASE_URL}/latest/download`;
  }
  return `${REPO_BASE_URL}/download/volclog-v${version}`;
}

function resolveInstallPlan(options) {
  const {
    pkgVersion,
    env = process.env,
    platform = process.platform,
    arch = process.arch,
    packageRoot,
  } = options || {};

  const normalizedPlatform = normalizePlatform(platform);
  const normalizedArch = normalizeArch(arch);
  const binaryName = normalizedPlatform === 'windows' ? 'volclog.exe' : 'volclog';
  const archiveName =
    normalizedPlatform === 'windows'
      ? `volclog_${normalizedPlatform}_${normalizedArch}.zip`
      : `volclog_${normalizedPlatform}_${normalizedArch}.tar.gz`;
  const binaryDir = path.join(packageRoot, '.volclog', 'bin');
  const binaryPath = path.join(binaryDir, binaryName);
  const downloadURL = env.VOLCLOG_DOWNLOAD_URL || `${releaseBaseURL(pkgVersion, env)}/${archiveName}`;
  const sha256URL = `${downloadURL}.sha256`;

  return {
    archiveName,
    binaryDir,
    binaryName,
    binaryPath,
    downloadURL,
    normalizedArch,
    normalizedPlatform,
    packageRoot,
    sha256URL,
  };
}

async function download(url, destination) {
  const response = await fetch(url, { redirect: 'follow' });
  if (!response.ok || !response.body) {
    throw new Error(`download failed: ${url} (${response.status})`);
  }
  await fsp.mkdir(path.dirname(destination), { recursive: true });
  await pipeline(Readable.fromWeb(response.body), fs.createWriteStream(destination));
}

async function maybeDownloadSha256(url, destination) {
  const response = await fetch(url, { redirect: 'follow' });
  if (response.status === 404) {
    return false;
  }
  if (!response.ok || !response.body) {
    throw new Error(`download failed: ${url} (${response.status})`);
  }
  await fsp.mkdir(path.dirname(destination), { recursive: true });
  await pipeline(Readable.fromWeb(response.body), fs.createWriteStream(destination));
  return true;
}

async function verifySha256(archivePath, shaPath) {
  const content = await fsp.readFile(shaPath, 'utf8');
  const expected = content.trim().split(/\s+/)[0];
  if (!expected) {
    throw new Error(`invalid sha256 file: ${shaPath}`);
  }
  const hash = crypto.createHash('sha256');
  hash.update(await fsp.readFile(archivePath));
  const actual = hash.digest('hex');
  if (actual !== expected) {
    throw new Error(`sha256 mismatch for ${path.basename(archivePath)}`);
  }
}

function extractArchive(archivePath, destination, platform) {
  const command =
    platform === 'windows'
      ? 'powershell.exe'
      : 'tar';
  const args =
    platform === 'windows'
      ? [
          '-NoProfile',
          '-Command',
          `Expand-Archive -Path '${archivePath.replace(/'/g, "''")}' -DestinationPath '${destination.replace(/'/g, "''")}' -Force`,
        ]
      : ['-xzf', archivePath, '-C', destination];

  const result = spawnSync(command, args, { stdio: 'inherit' });
  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    throw new Error(`extract failed with exit code ${result.status}`);
  }
}

async function installBinary(options) {
  const plan = resolveInstallPlan(options);
  if (process.env.VOLCLOG_NPM_SKIP_DOWNLOAD === '1') {
    return plan;
  }
  if (fs.existsSync(plan.binaryPath)) {
    return plan;
  }

  const tmpRoot = await fsp.mkdtemp(path.join(os.tmpdir(), 'volclog-npm-'));
  try {
    const archivePath = path.join(tmpRoot, plan.archiveName);
    const shaPath = `${archivePath}.sha256`;
    await download(plan.downloadURL, archivePath);
    if (await maybeDownloadSha256(plan.sha256URL, shaPath)) {
      await verifySha256(archivePath, shaPath);
    }
    await fsp.mkdir(plan.binaryDir, { recursive: true });
    extractArchive(archivePath, tmpRoot, plan.normalizedPlatform);

    const extractedBinary = path.join(tmpRoot, plan.binaryName);
    if (!fs.existsSync(extractedBinary)) {
      throw new Error(`binary not found in package: ${plan.binaryName}`);
    }
    await fsp.copyFile(extractedBinary, plan.binaryPath);
    if (plan.normalizedPlatform !== 'windows') {
      await fsp.chmod(plan.binaryPath, 0o755);
    }
    return plan;
  } finally {
    await fsp.rm(tmpRoot, { recursive: true, force: true });
  }
}

module.exports = {
  installBinary,
  resolveInstallPlan,
};
