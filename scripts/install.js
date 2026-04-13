#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const fsp = require("node:fs/promises");
const path = require("node:path");
const { Readable } = require("node:stream");
const { pipeline } = require("node:stream/promises");

const { getBinaryPath, resolveTarget } = require("./platform");

async function main() {
  const rootDir = path.resolve(__dirname, "..");
  const pkg = JSON.parse(
    await fsp.readFile(path.join(rootDir, "package.json"), "utf8"),
  );

  if (
    process.env.CHATGPT2CODEX_SKIP_DOWNLOAD === "1" ||
    pkg.version.endsWith("-development")
  ) {
    console.log("Skipping chatgpt2codex binary download.");
    return;
  }

  const target = resolveTarget();
  const destination = getBinaryPath(rootDir, target.platform, target.arch);
  const tempFile = `${destination}.tmp`;
  const downloadUrl = buildDownloadURL(pkg.version, target.fileName);

  await fsp.mkdir(path.dirname(destination), { recursive: true });

  try {
    await fsp.rm(destination, { force: true });
    await fsp.rm(tempFile, { force: true });

    console.log(`Downloading ${downloadUrl}`);
    await downloadBinary(downloadUrl, tempFile, pkg.name, pkg.version);

    if (target.platform !== "win32") {
      await fsp.chmod(tempFile, 0o755);
    }

    await fsp.rename(tempFile, destination);
    console.log(`Installed ${path.relative(rootDir, destination)}`);
  } catch (error) {
    await fsp.rm(tempFile, { force: true });
    throw error;
  }
}

function buildDownloadURL(version, fileName) {
  const baseUrl =
    process.env.CHATGPT2CODEX_RELEASE_BASE_URL ||
    "https://github.com/icattlecoder/chatgpt2codex/releases/download";

  return `${baseUrl}/v${version}/${fileName}`;
}

async function downloadBinary(url, destination, packageName, packageVersion) {
  const response = await fetch(url, {
    headers: {
      "user-agent": `${packageName}/${packageVersion}`,
    },
    redirect: "follow",
  });

  if (!response.ok || !response.body) {
    throw new Error(
      `Download failed with status ${response.status} ${response.statusText}`,
    );
  }

  await pipeline(
    Readable.fromWeb(response.body),
    fs.createWriteStream(destination),
  );
}

main().catch((error) => {
  console.error(`Failed to install chatgpt2codex: ${error.message}`);
  process.exit(1);
});
