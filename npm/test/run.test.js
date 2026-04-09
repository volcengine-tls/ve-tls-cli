const test = require('node:test');
const assert = require('node:assert/strict');

const { runCLI } = require('../lib/run');

test('runCLI forwards args to installed volclog binary', () => {
  const calls = [];
  const exitCode = runCLI({
    argv: ['skill', 'install', '--dir', '/tmp/skills'],
    binaryPath: '/tmp/pkg/.volclog/bin/volclog',
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
      command: '/tmp/pkg/.volclog/bin/volclog',
      args: ['skill', 'install', '--dir', '/tmp/skills'],
      options: { stdio: 'inherit' },
    },
  ]);
});
