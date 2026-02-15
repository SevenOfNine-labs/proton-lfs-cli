#!/usr/bin/env bash
# ensure-info-plist.sh — Create Info.plist if it doesn't already exist.
# Usage: ensure-info-plist.sh <plist-path> <version>
set -euo pipefail

PLIST_PATH="${1:?Usage: ensure-info-plist.sh <plist-path> <version>}"
VERSION="${2:-1.0.0}"

[ -f "$PLIST_PATH" ] && exit 0

cat > "$PLIST_PATH" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>
  <string>Proton Git LFS</string>
  <key>CFBundleIdentifier</key>
  <string>com.proton.git-lfs-tray</string>
  <key>CFBundleVersion</key>
  <string>${VERSION}</string>
  <key>CFBundleShortVersionString</key>
  <string>${VERSION}</string>
  <key>CFBundleExecutable</key>
  <string>proton-git-lfs-tray</string>
  <key>LSUIElement</key>
  <true/>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
PLIST
