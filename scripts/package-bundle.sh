#!/usr/bin/env bash
# package-bundle.sh — Assemble platform-specific distribution bundles.
#
# Usage: package-bundle.sh <target> <goos> [version]
#   target:  e.g. darwin-arm64, linux-amd64, windows-amd64
#   goos:    darwin | linux | windows
#   version: bundle version; defaults to $VERSION, then the release tag
#            ($GITHUB_REF_NAME when it starts with "v"), then git describe.
#
# Expects build artifacts in ./build/ (populated by CI or local build).
# Produces archives in ./dist/.
#
# Archives are reproducible: every entry's mtime is SOURCE_DATE_EPOCH (default:
# the commit date), entries are sorted, owners are numeric 0:0, and gzip/zip
# carry no host-specific extra fields. The darwin bundle is ad-hoc signed and
# zipped with ditto on macOS (preserves the signature and resource forks);
# elsewhere it falls back to zip and stays unsigned.
set -euo pipefail

TARGET="${1:?Usage: package-bundle.sh <target> <goos> [version]}"
GOOS="${2:?Usage: package-bundle.sh <target> <goos> [version]}"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BUILD_DIR="build"
DIST_DIR="dist"
BUNDLE_NAME="proton-lfs-cli-${TARGET}"

export TZ=UTC
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || echo 0)}"
export SOURCE_DATE_EPOCH

# autostart desktop entry name; must match autostartDesktopFileName in
# cmd/tray/setup.go, which the tray's "Start at System Login" writes.
DESKTOP_FILE_NAME="proton-lfs.desktop"

# Bundle version without the tag's leading "v" (CFBundleShortVersionString
# and CFBundleVersion take dotted integers), from the third argument, VERSION,
# a v* GITHUB_REF_NAME or git describe, in that order.
resolve_version() {
  local raw
  if [ -n "${3:-}" ]; then
    raw="$3"
  elif [ -n "${VERSION:-}" ]; then
    raw="$VERSION"
  elif [ -n "${GITHUB_REF_NAME:-}" ] && [ "${GITHUB_REF_NAME#v}" != "$GITHUB_REF_NAME" ]; then
    raw="$GITHUB_REF_NAME"
  else
    raw="$(git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")"
  fi
  echo "${raw#v}"
}
BUNDLE_VERSION="$(resolve_version "$@")"

# Set every file and directory under $1 to SOURCE_DATE_EPOCH.
normalize_mtimes() {
  local stamp
  if touch -d "@${SOURCE_DATE_EPOCH}" "$1" 2>/dev/null; then
    find "$1" -exec touch -d "@${SOURCE_DATE_EPOCH}" {} +
  else
    # BSD touch (macOS): -t [[CC]YY]MMDDhhmm[.SS]
    stamp="$(date -u -r "${SOURCE_DATE_EPOCH}" +%Y%m%d%H%M.%S)"
    find "$1" -exec touch -t "$stamp" {} +
  fi
}

# zip -X: no uid/gid/extended timestamps; sorted file list via -@ for a stable
# entry order. Run from $DIST_DIR so entry names start with the bundle name.
zip_sorted() {
  local name="$1"
  (cd "$DIST_DIR" && find "$name" -print | LC_ALL=C sort | zip -X -q "${name}.zip" -@)
}

mkdir -p "$DIST_DIR"

case "$GOOS" in
  darwin)
    # macOS .app bundle
    APP_DIR="${DIST_DIR}/${BUNDLE_NAME}/ProtonLFS.app/Contents"
    mkdir -p "${APP_DIR}/MacOS" "${APP_DIR}/Helpers" "${APP_DIR}/Resources"

    cp "${BUILD_DIR}/proton-lfs-tray" "${APP_DIR}/MacOS/proton-lfs-tray"
    cp "${BUILD_DIR}/git-lfs-proton-adapter" "${APP_DIR}/Helpers/git-lfs-proton-adapter"
    cp "${BUILD_DIR}/proton-drive-cli" "${APP_DIR}/Helpers/proton-drive-cli"

    chmod +x "${APP_DIR}/MacOS/proton-lfs-tray"
    chmod +x "${APP_DIR}/Helpers/git-lfs-proton-adapter"
    chmod +x "${APP_DIR}/Helpers/proton-drive-cli"

    # AppIcon.icns comes from go run scripts/gen-app-icon.go.
    ICON_SRC="cmd/tray/AppIcon.icns"
    [ -f "$ICON_SRC" ] && cp "$ICON_SRC" "${APP_DIR}/Resources/AppIcon.icns"

    # One generator for Info.plist (bundle identifier, version) shared with
    # `make install`.
    bash "${SCRIPT_DIR}/ensure-info-plist.sh" "${APP_DIR}/Info.plist" "$BUNDLE_VERSION"

    if command -v codesign >/dev/null 2>&1; then
      # Ad-hoc signature: required on Apple silicon, and the SEA helper lost
      # its signature when the blob was injected. Users still clear the
      # quarantine attribute (docs/operations/deployment.md).
      codesign --force --deep --sign - "${DIST_DIR}/${BUNDLE_NAME}/ProtonLFS.app"
    else
      echo "WARNING: codesign not available; ProtonLFS.app is unsigned" >&2
    fi

    normalize_mtimes "${DIST_DIR}/${BUNDLE_NAME}"
    if command -v ditto >/dev/null 2>&1; then
      (cd "$DIST_DIR" && ditto -c -k --keepParent "${BUNDLE_NAME}" "${BUNDLE_NAME}.zip")
    else
      echo "WARNING: ditto not available; zip may drop macOS metadata" >&2
      zip_sorted "$BUNDLE_NAME"
    fi
    rm -rf "${DIST_DIR:?}/${BUNDLE_NAME}"
    echo "==> ${DIST_DIR}/${BUNDLE_NAME}.zip (version ${BUNDLE_VERSION})"
    ;;

  linux)
    STAGE_DIR="${DIST_DIR}/${BUNDLE_NAME}"
    mkdir -p "$STAGE_DIR"

    cp "${BUILD_DIR}/proton-lfs-tray" "${STAGE_DIR}/proton-lfs-tray"
    cp "${BUILD_DIR}/git-lfs-proton-adapter" "${STAGE_DIR}/git-lfs-proton-adapter"
    cp "${BUILD_DIR}/proton-drive-cli" "${STAGE_DIR}/proton-drive-cli"

    chmod +x "${STAGE_DIR}/proton-lfs-tray"
    chmod +x "${STAGE_DIR}/git-lfs-proton-adapter"
    chmod +x "${STAGE_DIR}/proton-drive-cli"

    # Desktop entry for manual installation into ~/.config/autostart or
    # ~/.local/share/applications; Exec expects proton-lfs-tray on PATH.
    cat > "${STAGE_DIR}/${DESKTOP_FILE_NAME}" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=Proton LFS
Exec=proton-lfs-tray
Comment=System tray for Proton LFS
Categories=Development;
StartupNotify=false
DESKTOP

    if ! tar --version 2>/dev/null | grep -q 'GNU tar'; then
      echo "GNU tar is required for a reproducible linux bundle (--sort, --mtime, --owner)" >&2
      exit 1
    fi
    # gzip -n: no file name or timestamp in the gzip header.
    tar --sort=name --mtime="@${SOURCE_DATE_EPOCH}" --owner=0 --group=0 --numeric-owner \
      -C "$DIST_DIR" -cf - "${BUNDLE_NAME}" | gzip -n > "${DIST_DIR}/${BUNDLE_NAME}.tar.gz"
    rm -rf "$STAGE_DIR"
    echo "==> ${DIST_DIR}/${BUNDLE_NAME}.tar.gz (version ${BUNDLE_VERSION})"
    ;;

  windows)
    STAGE_DIR="${DIST_DIR}/${BUNDLE_NAME}"
    mkdir -p "$STAGE_DIR"

    cp "${BUILD_DIR}/proton-lfs-tray.exe" "${STAGE_DIR}/proton-lfs-tray.exe"
    cp "${BUILD_DIR}/git-lfs-proton-adapter.exe" "${STAGE_DIR}/git-lfs-proton-adapter.exe"
    cp "${BUILD_DIR}/proton-drive-cli.exe" "${STAGE_DIR}/proton-drive-cli.exe"

    normalize_mtimes "$STAGE_DIR"
    zip_sorted "$BUNDLE_NAME"
    rm -rf "$STAGE_DIR"
    echo "==> ${DIST_DIR}/${BUNDLE_NAME}.zip (version ${BUNDLE_VERSION})"
    ;;

  *)
    echo "Unknown GOOS: $GOOS" >&2
    exit 1
    ;;
esac
