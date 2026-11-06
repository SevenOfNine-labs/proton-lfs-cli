#!/usr/bin/env bash
set -euo pipefail

# Prints the export of PROTON_PASS_CLI_BIN, the pass-cli binary that
# proton-drive-cli uses as key-password provider.
DEFAULT_PASS_CLI_BIN="${PROTON_PASS_CLI_BIN:-pass-cli}"

usage() {
  cat <<'EOF'
Usage:
  eval "$(scripts/export-pass-env.sh)"

Options:
  --pass-cli <bin>        pass-cli binary path (default: pass-cli)
  --skip-check            do not check that pass-cli is installed and logged in
  -h, --help              show this help
EOF
}

PASS_CLI_BIN="$DEFAULT_PASS_CLI_BIN"
SKIP_CHECK="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pass-cli)
      if [[ $# -lt 2 || "${2:-}" == --* ]]; then
        echo "--pass-cli requires a binary path" >&2
        exit 2
      fi
      PASS_CLI_BIN="$2"
      shift 2
      ;;
    --skip-check)
      SKIP_CHECK="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$PASS_CLI_BIN" ]]; then
  echo "Invalid pass-cli binary path" >&2
  exit 2
fi

if [[ "$SKIP_CHECK" != "true" ]]; then
  if ! command -v "$PASS_CLI_BIN" >/dev/null 2>&1; then
    echo "pass-cli binary not found: $PASS_CLI_BIN" >&2
    exit 1
  fi

  if ! test_err="$("$PASS_CLI_BIN" info 2>&1 >/dev/null)"; then
    echo "pass-cli is not authenticated. Run 'pass-cli login' first." >&2
    if [[ -n "$test_err" ]]; then
      echo "$test_err" >&2
    fi
    exit 1
  fi
fi

shell_quote() {
  printf '%q' "$1"
}

cat <<EOF
export PROTON_PASS_CLI_BIN=$(shell_quote "$PASS_CLI_BIN")
EOF
