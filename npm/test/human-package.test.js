const test = require('node:test');
const assert = require('node:assert/strict');

const humanPackage = require('../human-package/package.json');
const fullPackage = require('../../package.json');

test('human package manifest exposes volclog-human npm package', () => {
  assert.equal(humanPackage.name, '@volcengine-tls/volclog-human');
  assert.equal(humanPackage.version, fullPackage.version);
  assert.deepEqual(humanPackage.bin, {
    'volclog-human': 'bin/volclog-human.js',
  });
  assert.equal(humanPackage.scripts.postinstall, 'node scripts/postinstall.js');
});
