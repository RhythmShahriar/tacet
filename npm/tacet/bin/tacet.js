#!/usr/bin/env node
// Copyright (c) 2026 Rhythm Shahriar <me@rhy.io>
// SPDX-License-Identifier: MIT

// Runs tacet. The first run downloads this platform's binary from the
// GitHub release, checks it against the SHA-256 written into this
// package when it was published and keeps it in the user cache. No
// install scripts and no dependencies: only Node's own modules.
"use strict";

const { spawnSync } = require("child_process");
const crypto = require("crypto");
const fs = require("fs");
const os = require("os");
const path = require("path");
const zlib = require("zlib");

const meta = require("../package.json");
const version = meta.version;
const archives = (meta.tacet && meta.tacet.archives) || {};
const target = `${process.platform}-${process.arch}`;
const exe = process.platform === "win32" ? "tacet.exe" : "tacet";

function fail(msg) {
  process.stderr.write(`tacet: ${msg}\n`);
  process.exit(1);
}

// The same folder Go's os.UserCacheDir gives the binary itself.
function cacheDir() {
  if (process.platform === "win32") {
    return path.join(process.env.LOCALAPPDATA || path.join(os.homedir(), "AppData", "Local"), "tacet", "npm");
  }
  if (process.platform === "darwin") {
    return path.join(os.homedir(), "Library", "Caches", "tacet", "npm");
  }
  return path.join(process.env.XDG_CACHE_HOME || path.join(os.homedir(), ".cache"), "tacet", "npm");
}

// untar returns the file called name from an uncompressed tar archive.
function untar(buf, name) {
  for (let off = 0; off + 512 <= buf.length;) {
    const header = buf.subarray(off, off + 512);
    if (header.every((b) => b === 0)) break;
    const field = (a, b) => header.subarray(a, b).toString("utf8").replace(/\0.*$/s, "");
    const entry = (field(345, 500) ? field(345, 500) + "/" : "") + field(0, 100);
    const size = parseInt(field(124, 136).trim() || "0", 8);
    const type = String.fromCharCode(header[156] || 48);
    const start = off + 512;
    if ((type === "0" || type === "\0") && path.posix.basename(entry) === name) {
      return buf.subarray(start, start + size);
    }
    off = start + Math.ceil(size / 512) * 512;
  }
  return null;
}

// unzip returns the file called name from a zip archive.
function unzip(buf, name) {
  let eocd = -1;
  for (let i = buf.length - 22; i >= Math.max(0, buf.length - 65557); i--) {
    if (buf.readUInt32LE(i) === 0x06054b50) { eocd = i; break; }
  }
  if (eocd < 0) return null;
  let p = buf.readUInt32LE(eocd + 16);
  const count = buf.readUInt16LE(eocd + 10);
  for (let n = 0; n < count && buf.readUInt32LE(p) === 0x02014b50; n++) {
    const method = buf.readUInt16LE(p + 10);
    const csize = buf.readUInt32LE(p + 20);
    const nlen = buf.readUInt16LE(p + 28);
    const elen = buf.readUInt16LE(p + 30);
    const clen = buf.readUInt16LE(p + 32);
    const local = buf.readUInt32LE(p + 42);
    const entry = buf.subarray(p + 46, p + 46 + nlen).toString("utf8");
    if (path.posix.basename(entry) === name) {
      const data = local + 30 + buf.readUInt16LE(local + 26) + buf.readUInt16LE(local + 28);
      const raw = buf.subarray(data, data + csize);
      if (method === 0) return raw;
      if (method === 8) return zlib.inflateRawSync(raw);
      return null;
    }
    p += 46 + nlen + elen + clen;
  }
  return null;
}

async function install(dir, bin) {
  const a = archives[target];
  if (!a) {
    fail(`no build for ${target}. Supported: ${Object.keys(archives).join(", ")}.\n` +
      "Other install methods: https://github.com/RhythmShahriar/tacet#install");
  }
  const url = `https://github.com/RhythmShahriar/tacet/releases/download/v${version}/${a.file}`;
  process.stderr.write(`tacet: downloading tacet ${version} for ${target} (first run only)\n`);
  let buf;
  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    buf = Buffer.from(await res.arrayBuffer());
  } catch (err) {
    fail(`couldn't download ${url}: ${err.message}\n` +
      "Other install methods: https://github.com/RhythmShahriar/tacet#install");
  }
  const sum = crypto.createHash("sha256").update(buf).digest("hex");
  if (sum !== a.sha256) {
    fail(`the download of ${a.file} doesn't match the checksum published with this package; nothing was installed.`);
  }
  const file = a.file.endsWith(".zip") ? unzip(buf, exe) : untar(zlib.gunzipSync(buf), exe);
  if (!file) fail(`${a.file} has no ${exe}`);
  fs.mkdirSync(dir, { recursive: true });
  const tmp = `${bin}.${process.pid}.tmp`;
  fs.writeFileSync(tmp, file, { mode: 0o755 });
  fs.renameSync(tmp, bin);
  fs.writeFileSync(path.join(dir, "version"), version);
}

async function main() {
  const dir = cacheDir();
  const bin = path.join(dir, exe);
  let have = "";
  try { have = fs.readFileSync(path.join(dir, "version"), "utf8").trim(); } catch { /* first run */ }
  if (have !== version || !fs.existsSync(bin)) {
    await install(dir, bin);
  }
  const res = spawnSync(bin, process.argv.slice(2), { stdio: "inherit", windowsHide: true });
  if (res.error) fail(`could not run ${bin}: ${res.error.message}`);
  if (res.signal) process.kill(process.pid, res.signal);
  process.exit(res.status === null ? 1 : res.status);
}

main();
