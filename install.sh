#!/usr/bin/env sh
set -eu

APP_NAME="chatgpt2codex"
OWNER="icattlecoder"
REPO="chatgpt2codex"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"
INSTALL_DIR="${CHATGPT2CODEX_INSTALL_DIR:-${BINDIR:-$DEFAULT_INSTALL_DIR}}"
VERSION="${CHATGPT2CODEX_VERSION:-latest}"

usage() {
  cat <<EOF
Usage: install.sh [-b dir] [-v version]

Options:
  -b, --bindir DIR    Install directory. Default: ${DEFAULT_INSTALL_DIR}
  -v, --version VER   Git tag such as v0.1.0. Default: latest release
  -h, --help          Show this help message
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    -b|--bindir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    -v|--version)
      VERSION="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

normalize_os() {
  case "$(uname -s)" in
    Darwin)
      printf 'darwin'
      ;;
    Linux)
      printf 'linux'
      ;;
    *)
      echo "Unsupported operating system: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

normalize_arch() {
  case "$(uname -m)" in
    x86_64|amd64)
      printf 'amd64'
      ;;
    arm64|aarch64)
      printf 'arm64'
      ;;
    *)
      echo "Unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

download() {
  url="$1"
  destination="$2"

  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$destination"
    return
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -qO "$destination" "$url"
    return
  fi

  echo "curl or wget is required to install ${APP_NAME}" >&2
  exit 1
}

checksum() {
  file_path="$1"

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file_path" | awk '{print $1}'
    return
  fi

  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file_path" | awk '{print $1}'
    return
  fi

  echo "sha256sum or shasum is required to verify ${APP_NAME}" >&2
  exit 1
}

OS="$(normalize_os)"
ARCH="$(normalize_arch)"
ASSET_NAME="${APP_NAME}_${OS}_${ARCH}.tar.gz"
CHECKSUMS_NAME="${APP_NAME}_checksums.txt"

if [ "$VERSION" = "latest" ]; then
  DOWNLOAD_BASE="https://github.com/${OWNER}/${REPO}/releases/latest/download"
else
  case "$VERSION" in
    v*) ;;
    *) VERSION="v${VERSION}" ;;
  esac
  DOWNLOAD_BASE="https://github.com/${OWNER}/${REPO}/releases/download/${VERSION}"
fi

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t ${APP_NAME})"
trap 'rm -rf "$TMP_DIR"' EXIT INT HUP TERM

ARCHIVE_PATH="${TMP_DIR}/${ASSET_NAME}"
CHECKSUMS_PATH="${TMP_DIR}/${CHECKSUMS_NAME}"
EXTRACT_DIR="${TMP_DIR}/extract"
INSTALL_PATH="${INSTALL_DIR}/${APP_NAME}"

mkdir -p "$EXTRACT_DIR"
mkdir -p "$INSTALL_DIR"

echo "Downloading ${ASSET_NAME}"
download "${DOWNLOAD_BASE}/${ASSET_NAME}" "$ARCHIVE_PATH"
download "${DOWNLOAD_BASE}/${CHECKSUMS_NAME}" "$CHECKSUMS_PATH"

EXPECTED_SUM="$(awk -v file="$ASSET_NAME" '$2 == file { print $1 }' "$CHECKSUMS_PATH")"
if [ -z "$EXPECTED_SUM" ]; then
  echo "Failed to find checksum for ${ASSET_NAME}" >&2
  exit 1
fi

ACTUAL_SUM="$(checksum "$ARCHIVE_PATH")"
if [ "$EXPECTED_SUM" != "$ACTUAL_SUM" ]; then
  echo "Checksum verification failed for ${ASSET_NAME}" >&2
  exit 1
fi

tar -xzf "$ARCHIVE_PATH" -C "$EXTRACT_DIR"

if command -v install >/dev/null 2>&1; then
  install -m 0755 "${EXTRACT_DIR}/${APP_NAME}" "$INSTALL_PATH"
else
  cp "${EXTRACT_DIR}/${APP_NAME}" "$INSTALL_PATH"
  chmod 0755 "$INSTALL_PATH"
fi

echo "Installed ${APP_NAME} to ${INSTALL_PATH}"
case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo "Add ${INSTALL_DIR} to your PATH to run ${APP_NAME} from a new shell."
    ;;
esac
