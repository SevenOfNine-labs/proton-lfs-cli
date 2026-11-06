#!/usr/bin/env bash
set -euo pipefail

# Install git-lfs-proton-adapter from GitHub Releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/SevenOfNine-labs/proton-lfs-cli/main/scripts/install-adapter.sh | bash
#
# Environment variables:
#   INSTALL_DIR      target directory (default: /usr/local/bin)
#   VERSION          release tag to install (default: latest)
#   VERIFY_CHECKSUM  required (default): the asset must match its entry in the
#                    release's SHA256SUMS.txt (release-bundle.yml); skip: no check

REPO="SevenOfNine-labs/proton-lfs-cli"
BINARY_NAME="git-lfs-proton-adapter"
CHECKSUM_FILE="SHA256SUMS.txt"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERIFY_CHECKSUM="${VERIFY_CHECKSUM:-required}"

case "$VERIFY_CHECKSUM" in
  required|skip) ;;
  *) echo "VERIFY_CHECKSUM must be required or skip (got: ${VERIFY_CHECKSUM})" >&2; exit 1 ;;
esac

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

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64)  echo "amd64" ;;
    aarch64|arm64)  echo "arm64" ;;
    *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
  esac
}

# GitHub redirects releases/latest to releases/tag/<tag>; the tag is the last
# path segment of the final URL, so no API JSON needs parsing.
resolve_version() {
  if [ -n "${VERSION:-}" ]; then
    echo "$VERSION"
    return
  fi

  local effective tag
  effective="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
    "https://github.com/${REPO}/releases/latest")" || effective=""
  tag="${effective##*/}"
  case "$tag" in
    ""|latest|releases)
      echo "Failed to determine latest release from ${effective:-no response}" >&2
      exit 1
      ;;
  esac
  echo "$tag"
}

# SHA-256 of a file: coreutils sha256sum(1), or shasum(1) on macOS.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    return 1
  fi
}

checksum_unavailable() {
  echo "Checksum verification failed: $1 (VERIFY_CHECKSUM=skip installs unverified)" >&2
  exit 1
}

verify_checksum() {
  local asset_file="$1" asset_name="$2" version="$3"
  local sums expected actual

  if [ "$VERIFY_CHECKSUM" = "skip" ]; then
    return 0
  fi

  sums="${TMP_DIR}/${CHECKSUM_FILE}"
  if ! curl -fsSL -o "$sums" \
      "https://github.com/${REPO}/releases/download/${version}/${CHECKSUM_FILE}"; then
    checksum_unavailable "release ${version} publishes no ${CHECKSUM_FILE}"
  fi

  # sha256sum(1) check format: "<hex>  <name>", a leading '*' marks binary mode.
  expected="$(awk -v name="$asset_name" '{ f = $2; sub(/^\*/, "", f); if (f == name) { print $1; exit } }' "$sums")"
  if [ -z "$expected" ]; then
    checksum_unavailable "${CHECKSUM_FILE} of release ${version} has no entry for ${asset_name}"
  fi

  if ! actual="$(sha256_of "$asset_file")"; then
    echo "No sha256sum or shasum tool available to verify ${asset_name}" >&2
    exit 1
  fi
  if [ "$actual" != "$expected" ]; then
    echo "Checksum mismatch for ${asset_name}: expected ${expected}, got ${actual}" >&2
    exit 1
  fi
  echo "Checksum verified (${CHECKSUM_FILE})"
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

  TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/install-adapter.XXXXXX")"
  trap 'rm -rf "$TMP_DIR"' EXIT
  tmp="${TMP_DIR}/${asset_name}"

  if ! curl -fSL --progress-bar -o "$tmp" "$url"; then
    echo "Download failed. Check that release ${version} has asset ${asset_name}." >&2
    exit 1
  fi

  verify_checksum "$tmp" "$asset_name" "$version"

  # install(1) copies the file with an explicit mode and the invoking user's
  # ownership (root under sudo), unlike moving a 0600 mktemp file into place.
  if [ -d "$INSTALL_DIR" ] && [ -w "$INSTALL_DIR" ]; then
    install -m 0755 "$tmp" "${INSTALL_DIR}/${BINARY_NAME}"
  else
    echo "Elevated permissions required to install to ${INSTALL_DIR}"
    sudo install -d -m 0755 "$INSTALL_DIR"
    sudo install -m 0755 "$tmp" "${INSTALL_DIR}/${BINARY_NAME}"
  fi

  echo "Installed ${BINARY_NAME} ${version} to ${INSTALL_DIR}/${BINARY_NAME}"
  "${INSTALL_DIR}/${BINARY_NAME}" --version 2>/dev/null || true
}

main
