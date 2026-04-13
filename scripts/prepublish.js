#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");

const packageJSONPath = path.join(__dirname, "..", "package.json");
const pkg = JSON.parse(fs.readFileSync(packageJSONPath, "utf8"));

if (pkg.version.endsWith("-development")) {
  console.error(
    "Refusing to publish a development package version. Create a vX.Y.Z tag so the release workflow can set the real npm version.",
  );
  process.exit(1);
}
