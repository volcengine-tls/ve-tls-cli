'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');

const repoRoot = path.resolve(__dirname, '..', '..');
const rootPackage = require(path.join(repoRoot, 'package.json'));
const humanPackage = require(path.join(repoRoot, 'npm', 'human-package', 'package.json'));

test('stable release metadata stays aligned across Go and npm packages', () => {
  const version = '1.0.5';
  const releaseTag = `volclog-v${version}`;
  const goVersion = fs.readFileSync(
    path.join(repoRoot, 'internal', 'version', 'version.go'),
    'utf8',
  );
  const changelog = fs.readFileSync(path.join(repoRoot, 'CHANGELOG.md'), 'utf8');

  assert.equal(rootPackage.version, version);
  assert.equal(humanPackage.version, version);
  assert.equal(rootPackage.publishConfig.tag, 'latest');
  assert.equal(humanPackage.publishConfig.tag, 'latest');
  assert.equal(rootPackage.publishConfig.registry, 'https://registry.npmjs.org/');
  assert.equal(humanPackage.publishConfig.registry, 'https://registry.npmjs.org/');
  assert.equal(rootPackage.scripts.prepublishOnly, 'node scripts/check-npm-publish.mjs');
  assert.equal(
    humanPackage.scripts.prepublishOnly,
    'node ../../scripts/check-npm-publish.mjs',
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

test('GitHub release workflow publishes standalone installer assets', () => {
  const workflow = fs.readFileSync(
    path.join(repoRoot, '.github', 'workflows', 'release-volclog.yml'),
    'utf8',
  );
  const releaseJob = workflow.match(/\n  release:\n([\s\S]*)$/);

  assert.ok(releaseJob, 'release job should exist');
  assert.match(releaseJob[1], /- uses: actions\/checkout@v4/);
  assert.match(releaseJob[1], /^\s+scripts\/install-binary\.sh$/m);
  assert.match(releaseJob[1], /^\s+scripts\/install\.ps1$/m);
});

test('PowerShell installer does not swallow checksum mismatches', () => {
  const installer = fs.readFileSync(
    path.join(repoRoot, 'scripts', 'install.ps1'),
    'utf8',
  );
  const checksumCatch = installer.indexOf('catch {');
  const mismatch = installer.indexOf('throw "sha256 mismatch"');
  const extraction = installer.indexOf('Expand-Archive');

  assert.notEqual(checksumCatch, -1, 'checksum download should remain optional');
  assert.notEqual(mismatch, -1, 'checksum mismatch should fail closed');
  assert.notEqual(extraction, -1, 'installer should extract the verified archive');
  assert.ok(
    checksumCatch < mismatch && mismatch < extraction,
    'checksum mismatch must be checked after the optional-download catch and before extraction',
  );
});

test('npm publish guard keeps stable and release candidate dist-tags separate', () => {
  const script = path.join(repoRoot, 'scripts', 'check-npm-publish.mjs');
  const stableLatest = spawnSync(process.execPath, [script], {
    env: { ...process.env, npm_package_version: '1.0.5', npm_config_tag: 'latest' },
    encoding: 'utf8',
  });
  const stableRC = spawnSync(process.execPath, [script], {
    env: { ...process.env, npm_package_version: '1.0.5', npm_config_tag: 'rc' },
    encoding: 'utf8',
  });
  const rcLatest = spawnSync(process.execPath, [script], {
    env: { ...process.env, npm_package_version: '1.0.6-rc.1', npm_config_tag: 'latest' },
    encoding: 'utf8',
  });
  const rc = spawnSync(process.execPath, [script], {
    env: { ...process.env, npm_package_version: '1.0.6-rc.1', npm_config_tag: 'rc' },
    encoding: 'utf8',
  });

  assert.equal(stableLatest.status, 0, stableLatest.stderr);
  assert.notEqual(stableRC.status, 0);
  assert.match(stableRC.stderr, /must be published with --tag latest/);
  assert.notEqual(rcLatest.status, 0);
  assert.match(rcLatest.stderr, /must be published with --tag rc/);
  assert.equal(rc.status, 0, rc.stderr);
});
