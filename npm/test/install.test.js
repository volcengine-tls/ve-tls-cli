const test = require('node:test');
const assert = require('node:assert/strict');

const { resolveInstallPlan } = require('../lib/install');

test('resolveInstallPlan uses package version to build release URLs', () => {
  const plan = resolveInstallPlan({
    pkgVersion: '1.0.0',
    env: {},
    platform: 'darwin',
    arch: 'arm64',
    packageRoot: '/tmp/pkg',
  });

  assert.equal(
    plan.downloadURL,
    'https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.0/volclog_darwin_arm64.tar.gz',
  );
  assert.equal(
    plan.sha256URL,
    'https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.0/volclog_darwin_arm64.tar.gz.sha256',
  );
  assert.equal(plan.binaryName, 'volclog');
});

test('resolveInstallPlan preserves prerelease identifiers in release URLs', () => {
  const plan = resolveInstallPlan({
    pkgVersion: '1.0.5-rc.1',
    env: {},
    platform: 'linux',
    arch: 'x64',
    packageRoot: '/tmp/pkg',
  });

  assert.equal(
    plan.downloadURL,
    'https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.5-rc.1/volclog_linux_amd64.tar.gz',
  );
});

test('resolveInstallPlan prefers explicit download URL override', () => {
  const plan = resolveInstallPlan({
    pkgVersion: '1.0.0',
    env: {
      VOLCLOG_DOWNLOAD_URL: 'https://example.com/custom/volclog.tar.gz',
    },
    platform: 'linux',
    arch: 'x64',
    packageRoot: '/tmp/pkg',
  });

  assert.equal(plan.downloadURL, 'https://example.com/custom/volclog.tar.gz');
  assert.equal(plan.sha256URL, 'https://example.com/custom/volclog.tar.gz.sha256');
});
