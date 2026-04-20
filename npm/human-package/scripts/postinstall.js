#!/usr/bin/env node
'use strict';

const path = require('node:path');

const { installBinary } = require('../lib/install');

async function main() {
  const packageRoot = path.resolve(__dirname, '..');
  const pkg = require(path.join(packageRoot, 'package.json'));
  await installBinary({
    pkgVersion: pkg.version,
    packageRoot,
  });
}

main().catch((error) => {
  const message = error && error.message ? error.message : String(error);
  console.error(`[volclog-human] postinstall failed: ${message}`);
  process.exit(1);
});
