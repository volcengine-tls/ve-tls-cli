const test = require('node:test');
const assert = require('node:assert/strict');

const { resolveBinaryPath, runCLI } = require('../agent-package/lib/run');

test('agent resolveBinaryPath points at bundled volclog-agent binary', () => {
  const binaryPath = resolveBinaryPath({
    packageRoot: '/tmp/pkg-agent',
    platform: 'linux',
  });

  assert.equal(binaryPath, '/tmp/pkg-agent/.volclog-agent/bin/volclog-agent');
});

test('agent runCLI forwards args to installed volclog-agent binary', () => {
  const calls = [];
  const exitCode = runCLI({
    argv: ['tool', 'list', 'project'],
    binaryPath: '/tmp/pkg-agent/.volclog-agent/bin/volclog-agent',
    existsImpl() {
      return true;
    },
    spawnImpl(command, args, options) {
      calls.push({ command, args, options });
      return {
        status: 0,
        error: null,
      };
    },
  });

  assert.equal(exitCode, 0);
  assert.deepEqual(calls, [
    {
      command: '/tmp/pkg-agent/.volclog-agent/bin/volclog-agent',
      args: ['tool', 'list', 'project'],
      options: { stdio: 'inherit' },
    },
  ]);
});
