#!/usr/bin/env bash
# build-sea.sh — Build proton-drive-cli as a Node.js Single Executable Application.
#
# Uses Node.js 25.5+ native --build-sea (no postject required).
#
# Prerequisites:
#   - Node.js >= 25.5
#   - proton-drive-cli already compiled (yarn build / npm run build)
#
# Output: proton-drive-cli (or proton-drive-cli.exe on Windows) in bin/.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
DRIVE_CLI_DIR="${ROOT_DIR}/submodules/proton-drive-cli"
OUTPUT_DIR="${ROOT_DIR}/bin"

cd "$DRIVE_CLI_DIR"

echo "==> Step 1: esbuild bundle"
if [ ! -f dist/index.js ]; then
  echo "Error: dist/index.js not found. Run 'npm run build' first." >&2
  exit 1
fi
node esbuild.config.mjs

echo "==> Step 2: Build SEA binary"
OUTPUT_NAME="proton-drive-cli"
if [[ "$OSTYPE" == "msys" || "$OSTYPE" == "cygwin" || "$OSTYPE" == "win32" ]]; then
  OUTPUT_NAME="proton-drive-cli.exe"
fi

NODE_BIN="$(command -v node)"
NODE_VERSION="$(node --version)"
echo "    Using node: ${NODE_BIN} (${NODE_VERSION})"

# Generate build-time sea config with correct paths
SEA_BUILD_CONFIG="$(mktemp sea-config-build-XXXX.json)"
cat > "$SEA_BUILD_CONFIG" <<SEACFG
{
  "main": "sea-bundle.cjs",
  "output": "${OUTPUT_NAME}",
  "executable": "${NODE_BIN}",
  "disableExperimentalSEAWarning": true,
  "useCodeCache": true
}
SEACFG

node --build-sea "$SEA_BUILD_CONFIG"
rm -f "$SEA_BUILD_CONFIG"

# macOS: ad-hoc re-sign after build
if [[ "$OSTYPE" == "darwin"* ]]; then
  codesign --sign - "$OUTPUT_NAME"
fi

# Move to output directory
mkdir -p "$OUTPUT_DIR"
mv "$OUTPUT_NAME" "$OUTPUT_DIR/$OUTPUT_NAME"

# Clean up intermediate files
rm -f sea-bundle.cjs

echo "==> SEA binary ready: $OUTPUT_DIR/$OUTPUT_NAME"
