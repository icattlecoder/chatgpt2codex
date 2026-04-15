#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${1:-"$ROOT_DIR/dist"}"
APP_NAME="chatgpt2codex"
CHECKSUMS_FILE="${APP_NAME}_checksums.txt"

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

TARGETS=(
  "darwin amd64 tar.gz"
  "darwin arm64 tar.gz"
  "linux amd64 tar.gz"
  "linux arm64 tar.gz"
  "windows amd64 zip"
  "windows arm64 zip"
)

create_zip() {
  local source_dir="$1"
  local archive_path="$2"

  if command -v zip >/dev/null 2>&1; then
    (
      cd "$source_dir"
      zip -q "$archive_path" "$APP_NAME.exe" README.md LICENSE
    )
    return
  fi

  if ! command -v python3 >/dev/null 2>&1; then
    echo "zip or python3 is required to package Windows release archives" >&2
    exit 1
  fi

  python3 - "$source_dir" "$archive_path" <<'PY'
import pathlib
import sys
import zipfile

source_dir = pathlib.Path(sys.argv[1])
archive_path = pathlib.Path(sys.argv[2])
with zipfile.ZipFile(archive_path, "w", compression=zipfile.ZIP_DEFLATED) as archive:
    for relative_name in ("chatgpt2codex.exe", "README.md", "LICENSE"):
        archive.write(source_dir / relative_name, arcname=relative_name)
PY
}

artifacts=()
for target in "${TARGETS[@]}"; do
  read -r go_os go_arch archive_ext <<<"$target"

  stage_dir="$WORK_DIR/${APP_NAME}_${go_os}_${go_arch}"
  mkdir -p "$stage_dir"

  binary_name="$APP_NAME"
  if [[ "$go_os" == "windows" ]]; then
    binary_name="${APP_NAME}.exe"
  fi

  echo "Building ${APP_NAME} for ${go_os}/${go_arch}"
  CGO_ENABLED=0 GOOS="$go_os" GOARCH="$go_arch" \
    go build -trimpath -ldflags="-s -w" -o "$stage_dir/$binary_name" ./cmd/chatgpt2codex

  cp "$ROOT_DIR/README.md" "$stage_dir/README.md"
  cp "$ROOT_DIR/LICENSE" "$stage_dir/LICENSE"

  if [[ "$archive_ext" == "tar.gz" ]]; then
    archive_path="$DIST_DIR/${APP_NAME}_${go_os}_${go_arch}.tar.gz"
    (
      cd "$stage_dir"
      tar -czf "$archive_path" "$binary_name" README.md LICENSE
    )
  else
    archive_path="$DIST_DIR/${APP_NAME}_${go_os}_${go_arch}.zip"
    create_zip "$stage_dir" "$archive_path"
  fi

  artifacts+=("$(basename "$archive_path")")
done

IFS=$'\n' sorted_artifacts=($(printf '%s\n' "${artifacts[@]}" | LC_ALL=C sort))
unset IFS

(
  cd "$DIST_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${sorted_artifacts[@]}" > "$CHECKSUMS_FILE"
  else
    LC_ALL=C LANG=C shasum -a 256 "${sorted_artifacts[@]}" > "$CHECKSUMS_FILE"
  fi
)
