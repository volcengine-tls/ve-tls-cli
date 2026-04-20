const test = require('node:test');
const assert = require('node:assert/strict');

const { resolveBinaryPath, runCLI } = require('../human-package/lib/run');

test('human resolveBinaryPath points at bundled volclog-human binary', () => {
  const binaryPath = resolveBinaryPath({
    packageRoot: '/tmp/pkg-human',
    platform: 'linux',
  });

  assert.equal(binaryPath, '/tmp/pkg-human/.volclog-human/bin/volclog-human');
});

test('human runCLI forwards args to installed volclog-human binary', () => {
  const calls = [];
  const exitCode = runCLI({
    argv: ['tool', 'list', 'project'],
    binaryPath: '/tmp/pkg-human/.volclog-human/bin/volclog-human',
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
      command: '/tmp/pkg-human/.volclog-human/bin/volclog-human',
      args: ['tool', 'list', 'project'],
      options: { stdio: 'inherit' },
    },
  ]);
});
