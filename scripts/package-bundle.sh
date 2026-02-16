#!/usr/bin/env bash
# package-bundle.sh — Assemble platform-specific distribution bundles.
#
# Usage: package-bundle.sh <target> <goos>
#   target: e.g. darwin-arm64, linux-amd64, windows-amd64
#   goos:   darwin | linux | windows
#
# Expects build artifacts in ./build/ (populated by CI or local build).
# Produces archives in ./dist/.
set -euo pipefail

TARGET="${1:?Usage: package-bundle.sh <target> <goos>}"
GOOS="${2:?Usage: package-bundle.sh <target> <goos>}"

BUILD_DIR="build"
DIST_DIR="dist"
BUNDLE_NAME="proton-lfs-cli-${TARGET}"

mkdir -p "$DIST_DIR"

case "$GOOS" in
  darwin)
    # macOS .app bundle
    APP_DIR="${DIST_DIR}/${BUNDLE_NAME}/ProtonGitLFS.app/Contents"
    mkdir -p "${APP_DIR}/MacOS" "${APP_DIR}/Helpers" "${APP_DIR}/Resources"

    cp "${BUILD_DIR}/proton-lfs-cli-tray" "${APP_DIR}/MacOS/proton-lfs-cli-tray"
    cp "${BUILD_DIR}/git-lfs-proton-adapter" "${APP_DIR}/Helpers/git-lfs-proton-adapter"
    cp "${BUILD_DIR}/proton-drive-cli" "${APP_DIR}/Helpers/proton-drive-cli"

    chmod +x "${APP_DIR}/MacOS/proton-lfs-cli-tray"
    chmod +x "${APP_DIR}/Helpers/git-lfs-proton-adapter"
    chmod +x "${APP_DIR}/Helpers/proton-drive-cli"

    cat > "${APP_DIR}/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>
  <string>Proton Git LFS</string>
  <key>CFBundleIdentifier</key>
  <string>com.proton.lfs-tray</string>
  <key>CFBundleVersion</key>
  <string>1.0.0</string>
  <key>CFBundleShortVersionString</key>
  <string>1.0.0</string>
  <key>CFBundleExecutable</key>
  <string>proton-lfs-cli-tray</string>
  <key>LSUIElement</key>
  <true/>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
PLIST

    # Create zip archive
    (cd "$DIST_DIR" && zip -r "${BUNDLE_NAME}.zip" "${BUNDLE_NAME}/")
    rm -rf "${DIST_DIR}/${BUNDLE_NAME}"
    echo "==> ${DIST_DIR}/${BUNDLE_NAME}.zip"
    ;;

  linux)
    STAGE_DIR="${DIST_DIR}/${BUNDLE_NAME}"
    mkdir -p "$STAGE_DIR"

    cp "${BUILD_DIR}/proton-lfs-cli-tray" "${STAGE_DIR}/proton-lfs-cli-tray"
    cp "${BUILD_DIR}/git-lfs-proton-adapter" "${STAGE_DIR}/git-lfs-proton-adapter"
    cp "${BUILD_DIR}/proton-drive-cli" "${STAGE_DIR}/proton-drive-cli"

    chmod +x "${STAGE_DIR}/proton-lfs-cli-tray"
    chmod +x "${STAGE_DIR}/git-lfs-proton-adapter"
    chmod +x "${STAGE_DIR}/proton-drive-cli"

    # Optional desktop entry for autostart
    cat > "${STAGE_DIR}/proton-lfs-cli.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=Proton Git LFS
Exec=proton-lfs-cli-tray
Comment=System tray for Proton Git LFS
Categories=Development;
StartupNotify=false
DESKTOP

    tar -czf "${DIST_DIR}/${BUNDLE_NAME}.tar.gz" -C "$DIST_DIR" "${BUNDLE_NAME}"
    rm -rf "$STAGE_DIR"
    echo "==> ${DIST_DIR}/${BUNDLE_NAME}.tar.gz"
    ;;

  windows)
    STAGE_DIR="${DIST_DIR}/${BUNDLE_NAME}"
    mkdir -p "$STAGE_DIR"

    cp "${BUILD_DIR}/proton-lfs-cli-tray.exe" "${STAGE_DIR}/proton-lfs-cli-tray.exe"
    cp "${BUILD_DIR}/git-lfs-proton-adapter.exe" "${STAGE_DIR}/git-lfs-proton-adapter.exe"
    cp "${BUILD_DIR}/proton-drive-cli.exe" "${STAGE_DIR}/proton-drive-cli.exe"

    (cd "$DIST_DIR" && zip -r "${BUNDLE_NAME}.zip" "${BUNDLE_NAME}/")
    rm -rf "$STAGE_DIR"
    echo "==> ${DIST_DIR}/${BUNDLE_NAME}.zip"
    ;;

  *)
    echo "Unknown GOOS: $GOOS" >&2
    exit 1
    ;;
esac
