#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${1:-"$ROOT_DIR/dist"}"

mkdir -p "$DIST_DIR"
rm -f "$DIST_DIR"/chatgpt2codex-* "$DIST_DIR"/SHA256SUMS

TARGETS=(
  "darwin x64 darwin amd64"
  "darwin arm64 darwin arm64"
  "linux x64 linux amd64"
  "linux arm64 linux arm64"
  "win32 x64 windows amd64"
  "win32 arm64 windows arm64"
)

for target in "${TARGETS[@]}"; do
  read -r node_os node_arch go_os go_arch <<<"$target"

  ext=""
  if [[ "$node_os" == "win32" ]]; then
    ext=".exe"
  fi

  output="$DIST_DIR/chatgpt2codex-${node_os}-${node_arch}${ext}"

  echo "Building ${output##*/}"
  CGO_ENABLED=0 GOOS="$go_os" GOARCH="$go_arch" \
    go build -trimpath -ldflags="-s -w" -o "$output" ./cmd/chatgpt2codex
done

(
  cd "$DIST_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum chatgpt2codex-* > SHA256SUMS
  else
    LC_ALL=C LANG=C shasum -a 256 chatgpt2codex-* > SHA256SUMS
  fi
)
