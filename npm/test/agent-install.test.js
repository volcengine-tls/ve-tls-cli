const test = require('node:test');
const assert = require('node:assert/strict');

const { resolveInstallPlan } = require('../agent-package/lib/install');

test('agent resolveInstallPlan uses agent release artifact names', () => {
  const plan = resolveInstallPlan({
    pkgVersion: '1.0.1',
    env: {},
    platform: 'darwin',
    arch: 'arm64',
    packageRoot: '/tmp/pkg-agent',
  });

  assert.equal(
    plan.downloadURL,
    'https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.1/volclog-agent_darwin_arm64.tar.gz',
  );
  assert.equal(
    plan.sha256URL,
    'https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.1/volclog-agent_darwin_arm64.tar.gz.sha256',
  );
  assert.equal(plan.binaryName, 'volclog-agent');
  assert.equal(plan.binaryPath, '/tmp/pkg-agent/.volclog-agent/bin/volclog-agent');
});

test('agent resolveInstallPlan ignores full-package download URL override', () => {
  const plan = resolveInstallPlan({
    pkgVersion: '1.0.1',
    env: {
      VOLCLOG_DOWNLOAD_URL: 'https://example.com/full/volclog_linux_amd64.tar.gz',
    },
    platform: 'linux',
    arch: 'x64',
    packageRoot: '/tmp/pkg-agent',
  });

  assert.equal(
    plan.downloadURL,
    'https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.1/volclog-agent_linux_amd64.tar.gz',
  );
  assert.equal(
    plan.sha256URL,
    'https://github.com/volcengine-tls/ve-tls-cli/releases/download/volclog-v1.0.1/volclog-agent_linux_amd64.tar.gz.sha256',
  );
});
