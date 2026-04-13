#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { spawn } = require("node:child_process");

const { getBinaryPath, resolveTarget } = require("../scripts/platform");

const rootDir = path.resolve(__dirname, "..");

let target;
let binaryPath;

try {
  target = resolveTarget();
  binaryPath = getBinaryPath(rootDir, target.platform, target.arch);
} catch (error) {
  console.error(error.message);
  process.exit(1);
}

if (!fs.existsSync(binaryPath)) {
  console.error(
    `chatgpt2codex binary for ${target.platform}/${target.arch} is missing at ${binaryPath}.`,
  );
  console.error("Reinstall the package or run `npm rebuild chatgpt2codex`.");
  process.exit(1);
}

const child = spawn(binaryPath, process.argv.slice(2), {
  stdio: "inherit",
});

child.on("error", (error) => {
  console.error(`Failed to start ${binaryPath}: ${error.message}`);
  process.exit(1);
});

for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"]) {
  process.on(signal, () => {
    if (!child.killed) {
      child.kill(signal);
    }
  });
}

child.on("exit", (code, signal) => {
  if (signal) {
    process.exit(1);
  }

  process.exit(code === null ? 1 : code);
});
