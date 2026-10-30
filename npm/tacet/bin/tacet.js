#!/usr/bin/env node
// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Runs the tacet binary from the platform package npm installed through
// optionalDependencies. No install scripts, no downloads.
"use strict";

const { spawnSync } = require("child_process");
const fs = require("fs");

const supported = ["darwin-arm64", "darwin-x64", "linux-arm64", "linux-x64", "win32-arm64", "win32-x64"];
const target = `${process.platform}-${process.arch}`;
const pkg = `@rhythmsh/tacet-${target}`;
const exe = process.platform === "win32" ? "tacet.exe" : "tacet";

function fail(msg) {
  process.stderr.write(`tacet: ${msg}\n`);
  process.exit(1);
}

if (!supported.includes(target)) {
  fail(`no build for ${target}. Supported: ${supported.join(", ")}.\n` +
    "Other install methods: https://github.com/RhythmShahriar/tacet#install");
}

let bin;
try {
  bin = require.resolve(`${pkg}/bin/${exe}`);
} catch {
  fail(`the platform package ${pkg} is missing.\n` +
    "npm skips optional dependencies when installed with --omit=optional or --no-optional.\n" +
    "Reinstall with: npm i -g @rhythmsh/tacet");
}

// A tarball unpacked without its file modes leaves the binary unrunnable.
if (process.platform !== "win32") {
  try {
    fs.accessSync(bin, fs.constants.X_OK);
  } catch {
    try {
      fs.chmodSync(bin, 0o755);
    } catch {
      // spawnSync reports the error below.
    }
  }
}

const res = spawnSync(bin, process.argv.slice(2), { stdio: "inherit", windowsHide: true });
if (res.error) {
  fail(`could not run ${bin}: ${res.error.message}`);
}
if (res.signal) {
  process.kill(process.pid, res.signal);
}
process.exit(res.status === null ? 1 : res.status);
