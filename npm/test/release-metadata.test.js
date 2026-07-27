'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '..', '..');
const rootPackage = require(path.join(repoRoot, 'package.json'));
const humanPackage = require(path.join(repoRoot, 'npm', 'human-package', 'package.json'));

test('release candidate metadata stays aligned across Go and npm packages', () => {
  const version = '1.0.5-rc.1';
  const releaseTag = `volclog-v${version}`;
  const goVersion = fs.readFileSync(
    path.join(repoRoot, 'internal', 'version', 'version.go'),
    'utf8',
  );
  const changelog = fs.readFileSync(path.join(repoRoot, 'CHANGELOG.md'), 'utf8');

  assert.equal(rootPackage.version, version);
  assert.equal(humanPackage.version, version);
  assert.equal(rootPackage.publishConfig.tag, 'rc');
  assert.equal(humanPackage.publishConfig.tag, 'rc');
  assert.equal(rootPackage.publishConfig.registry, 'https://registry.npmjs.org/');
  assert.equal(humanPackage.publishConfig.registry, 'https://registry.npmjs.org/');
  assert.equal(rootPackage.scripts.prepublishOnly, 'node scripts/check-npm-rc-publish.mjs');
  assert.equal(
    humanPackage.scripts.prepublishOnly,
    'node ../../scripts/check-npm-rc-publish.mjs',
  );
  assert.match(goVersion, new RegExp(`var Version = "${releaseTag.replaceAll('.', '\\.')}"`));
  assert.match(changelog, new RegExp(`^## ${releaseTag.replaceAll('.', '\\.')}$`, 'm'));
});

test('GitHub release workflow marks release candidate tags as prereleases', () => {
  const workflow = fs.readFileSync(
    path.join(repoRoot, '.github', 'workflows', 'release-volclog.yml'),
    'utf8',
  );

  assert.match(workflow, /prerelease:\s*\$\{\{\s*contains\(github\.ref_name, '-rc\.'\)\s*\}\}/);
  assert.match(
    workflow,
    /make_latest:\s*\$\{\{\s*contains\(github\.ref_name, '-rc\.'\)\s*&&\s*'false'\s*\|\|\s*'legacy'\s*\}\}/,
  );
});

test('npm release candidate guard rejects latest and accepts rc', () => {
  const script = path.join(repoRoot, 'scripts', 'check-npm-rc-publish.mjs');
  const latest = spawnSync(process.execPath, [script], {
    env: { ...process.env, npm_config_tag: 'latest' },
    encoding: 'utf8',
  });
  const rc = spawnSync(process.execPath, [script], {
    env: { ...process.env, npm_config_tag: 'rc' },
    encoding: 'utf8',
  });

  assert.notEqual(latest.status, 0);
  assert.match(latest.stderr, /must be published with --tag rc/);
  assert.equal(rc.status, 0, rc.stderr);
});
