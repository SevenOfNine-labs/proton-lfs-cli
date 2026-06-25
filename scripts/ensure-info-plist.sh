#!/usr/bin/env bash
# ensure-info-plist.sh — Write Info.plist (always regenerated to stay current).
# Usage: ensure-info-plist.sh <plist-path> <version>
set -euo pipefail

PLIST_PATH="${1:?Usage: ensure-info-plist.sh <plist-path> <version>}"
VERSION="${2:-1.0.0}"

cat > "$PLIST_PATH" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>
  <string>Proton LFS</string>
  <key>CFBundleIdentifier</key>
  <string>com.proton.lfs-tray</string>
  <key>CFBundleVersion</key>
  <string>${VERSION}</string>
  <key>CFBundleShortVersionString</key>
  <string>${VERSION}</string>
  <key>CFBundleExecutable</key>
  <string>proton-lfs-tray</string>
  <key>CFBundleIconFile</key>
  <string>AppIcon</string>
  <key>LSUIElement</key>
  <true/>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
PLIST
