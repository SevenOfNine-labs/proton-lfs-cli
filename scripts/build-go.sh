#!/usr/bin/env bash
# build-go.sh — Build a first-party Go binary with version stamping.
#
# The single source for -ldflags used by the Makefile, build.yml and
# release-bundle.yml, so every binary carries the same Version, GitCommit and
# BuildTime no matter where it was built.
#
# Usage: build-go.sh <adapter|tray> [output-path]
#
# Environment:
#   VERSION            version string (default: git describe --tags --always --dirty)
#   SOURCE_DATE_EPOCH  Unix time stamped as BuildTime (default: commit date of HEAD)
#   GOOS / GOARCH      cross-compile target (default: the host, via go env)
#   STRIP=1            add -s -w (release builds)
#   GO                 go binary (default: go)
set -euo pipefail

TARGET="${1:?Usage: build-go.sh <adapter|tray> [output-path]}"
OUT="${2:-}"
GO="${GO:-go}"

GOOS="${GOOS:-$("$GO" env GOOS)}"
GOARCH="${GOARCH:-$("$GO" env GOARCH)}"
export GOOS GOARCH

EXT=""
if [ "$GOOS" = "windows" ]; then
  EXT=".exe"
fi

case "$TARGET" in
  adapter)
    PKG="./cmd/adapter"
    OUT="${OUT:-bin/git-lfs-proton-adapter${EXT}}"
    export CGO_ENABLED=0
    EXTRA_LDFLAGS=""
    ;;
  tray)
    PKG="./cmd/tray"
    OUT="${OUT:-bin/proton-lfs-tray${EXT}}"
    # fyne.io/systray needs cgo only on darwin (Cocoa); Linux (D-Bus) and
    # Windows (Win32 syscalls) are pure Go.
    if [ "$GOOS" = "darwin" ]; then
      export CGO_ENABLED=1
    else
      export CGO_ENABLED=0
    fi
    EXTRA_LDFLAGS=""
    if [ "$GOOS" = "windows" ]; then
      EXTRA_LDFLAGS="-H windowsgui"
    fi
    ;;
  *)
    echo "Unknown target: $TARGET (expected adapter or tray)" >&2
    exit 1
    ;;
esac

# One version source: an explicit VERSION (the release tag in CI), otherwise
# git describe.
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
GIT_COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo dev)"

# BuildTime from SOURCE_DATE_EPOCH (reproducible-builds.org/specs/source-date-epoch),
# else the commit date.
SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct 2>/dev/null || echo 0)}"
export SOURCE_DATE_EPOCH
if BUILD_TIME="$(date -u -d "@${SOURCE_DATE_EPOCH}" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null)"; then
  : # GNU date
else
  BUILD_TIME="$(date -u -r "${SOURCE_DATE_EPOCH}" +%Y-%m-%dT%H:%M:%SZ)" # BSD date
fi

LDFLAGS="-X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}"
if [ "${STRIP:-0}" = "1" ]; then
  LDFLAGS="-s -w ${LDFLAGS}"
fi
if [ -n "$EXTRA_LDFLAGS" ]; then
  LDFLAGS="${LDFLAGS} ${EXTRA_LDFLAGS}"
fi

mkdir -p "$(dirname "$OUT")"
echo "==> go build ${PKG} -> ${OUT} (${GOOS}/${GOARCH}, cgo=${CGO_ENABLED}, version ${VERSION}, commit ${GIT_COMMIT}, built ${BUILD_TIME})"
"$GO" build -trimpath -ldflags "$LDFLAGS" -o "$OUT" "$PKG"
