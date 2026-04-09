#!/usr/bin/env node
'use strict';

const { runCLI } = require('../lib/run');

try {
  process.exit(runCLI());
} catch (error) {
  const message = error && error.message ? error.message : String(error);
  console.error(`[volclog] ${message}`);
  process.exit(1);
}
