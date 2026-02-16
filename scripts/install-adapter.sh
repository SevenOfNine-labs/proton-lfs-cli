#!/usr/bin/env bash
set -euo pipefail

# Install git-lfs-proton-adapter from GitHub Releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/SevenOfNine-ai/proton-lfs-cli/main/scripts/install-adapter.sh | bash
#
# Environment variables:
#   INSTALL_DIR   — target directory (default: /usr/local/bin)
#   VERSION       — release tag to install (default: latest)

REPO="SevenOfNine-ai/proton-lfs-cli"
BINARY_NAME="git-lfs-proton-adapter"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# --- Detect OS ---
detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux)  echo "linux" ;;
    darwin) echo "darwin" ;;
    mingw*|msys*|cygwin*) echo "windows" ;;
    *) echo "Unsupported OS: $os" >&2; exit 1 ;;
  esac
}

# --- Detect architecture ---
detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64)  echo "amd64" ;;
    aarch64|arm64)  echo "arm64" ;;
    *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
  esac
}

# --- Resolve version ---
resolve_version() {
  if [ -n "${VERSION:-}" ]; then
    echo "$VERSION"
    return
  fi

  local latest
  latest="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"

  if [ -z "$latest" ]; then
    echo "Failed to determine latest release" >&2
    exit 1
  fi
  echo "$latest"
}

main() {
  local os arch version asset_name url tmp

  os="$(detect_os)"
  arch="$(detect_arch)"
  version="$(resolve_version)"

  asset_name="${BINARY_NAME}-${os}-${arch}"
  if [ "$os" = "windows" ]; then
    asset_name="${asset_name}.exe"
  fi

  url="https://github.com/${REPO}/releases/download/${version}/${asset_name}"

  echo "Installing ${BINARY_NAME} ${version} (${os}/${arch})..."
  echo "  From: ${url}"
  echo "  To:   ${INSTALL_DIR}/${BINARY_NAME}"

  tmp="$(mktemp)"
  trap 'rm -f "$tmp"' EXIT

  if ! curl -fSL --progress-bar -o "$tmp" "$url"; then
    echo "Download failed. Check that release ${version} has asset ${asset_name}." >&2
    exit 1
  fi

  chmod +x "$tmp"

  if [ -w "$INSTALL_DIR" ]; then
    mv "$tmp" "${INSTALL_DIR}/${BINARY_NAME}"
  else
    echo "Elevated permissions required to install to ${INSTALL_DIR}"
    sudo mv "$tmp" "${INSTALL_DIR}/${BINARY_NAME}"
  fi

  echo "Installed ${BINARY_NAME} ${version} to ${INSTALL_DIR}/${BINARY_NAME}"
  "${INSTALL_DIR}/${BINARY_NAME}" --version 2>/dev/null || true
}

main
