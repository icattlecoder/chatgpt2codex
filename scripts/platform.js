"use strict";

const path = require("node:path");

const SUPPORTED_TARGETS = new Set([
  "darwin:x64",
  "darwin:arm64",
  "linux:x64",
  "linux:arm64",
  "win32:x64",
  "win32:arm64",
]);

function resolveTarget(
  platform = process.env.npm_config_platform || process.platform,
  arch = process.env.npm_config_arch || process.arch,
) {
  const key = `${platform}:${arch}`;

  if (!SUPPORTED_TARGETS.has(key)) {
    const supported = Array.from(SUPPORTED_TARGETS)
      .map((entry) => entry.replace(":", "/"))
      .join(", ");
    throw new Error(
      `Unsupported platform ${platform}/${arch}. Supported targets: ${supported}`,
    );
  }

  const ext = platform === "win32" ? ".exe" : "";

  return {
    platform,
    arch,
    fileName: `chatgpt2codex-${platform}-${arch}${ext}`,
  };
}

function getBinaryPath(rootDir, platform, arch) {
  const target = resolveTarget(platform, arch);
  return path.join(rootDir, "native", target.fileName);
}

module.exports = {
  SUPPORTED_TARGETS,
  getBinaryPath,
  resolveTarget,
};
