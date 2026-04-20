const test = require('node:test');
const assert = require('node:assert/strict');

const { resolveInstallPlan } = require('../human-package/lib/install');

test('human resolveInstallPlan uses human release artifact names', () => {
  const plan = resolveInstallPlan({
    pkgVersion: '1.0.3',
    env: {},
    platform: 'darwin',
    arch: 'arm64',
    packageRoot: '/tmp/pkg-human',
  });

  assert.equal(
    plan.downloadURL,
    'https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.3/volclog-human_darwin_arm64.tar.gz',
  );
  assert.equal(
    plan.sha256URL,
    'https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.3/volclog-human_darwin_arm64.tar.gz.sha256',
  );
  assert.equal(plan.binaryName, 'volclog-human');
  assert.equal(plan.binaryPath, '/tmp/pkg-human/.volclog-human/bin/volclog-human');
});

test('human resolveInstallPlan ignores default-package download URL override', () => {
  const plan = resolveInstallPlan({
    pkgVersion: '1.0.3',
    env: {
      VOLCLOG_DOWNLOAD_URL: 'https://example.com/full/volclog_linux_amd64.tar.gz',
    },
    platform: 'linux',
    arch: 'x64',
    packageRoot: '/tmp/pkg-human',
  });

  assert.equal(
    plan.downloadURL,
    'https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.3/volclog-human_linux_amd64.tar.gz',
  );
  assert.equal(
    plan.sha256URL,
    'https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.3/volclog-human_linux_amd64.tar.gz.sha256',
  );
});
