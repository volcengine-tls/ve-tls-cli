const test = require('node:test');
const assert = require('node:assert/strict');

const agentPackage = require('../agent-package/package.json');
const fullPackage = require('../../package.json');

test('agent package manifest exposes volclog-agent npm package', () => {
  assert.equal(agentPackage.name, '@volcengine-tls/volclog-agent');
  assert.equal(agentPackage.version, fullPackage.version);
  assert.deepEqual(agentPackage.bin, {
    'volclog-agent': 'bin/volclog-agent.js',
  });
  assert.equal(agentPackage.scripts.postinstall, 'node scripts/postinstall.js');
});
