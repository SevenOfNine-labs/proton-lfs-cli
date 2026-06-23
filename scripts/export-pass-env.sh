#!/usr/bin/env bash
set -euo pipefail

# Export only the Proton Pass CLI binary used by proton-drive-cli providers.
# Account usernames/passwords are browser-fork-only and are never exported for
# Git LFS transfers.
DEFAULT_PASS_CLI_BIN="${PROTON_PASS_CLI_BIN:-pass-cli}"

usage() {
  cat <<'EOF'
Usage:
  eval "$(scripts/export-pass-env.sh)"

Options:
  --pass-cli <bin>        pass-cli binary path (default: pass-cli)
  --skip-check            do not validate references through pass-cli
  -h, --help              show this help

This helper does not export Proton account username/password references.
Browser-fork login and two-password unlocks are resolved by proton-drive-cli
through the selected provider (`pass-cli` or `git-credential`).
EOF
}

PASS_CLI_BIN="$DEFAULT_PASS_CLI_BIN"
SKIP_CHECK="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pass-cli)
      PASS_CLI_BIN="$2"
      shift 2
      ;;
    --ref-root | --username-ref | --password-ref)
      echo "Account credential reference options were removed; browser-fork auth searches provider vaults directly." >&2
      exit 2
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

  if ! test_err="$("$PASS_CLI_BIN" test 2>&1 >/dev/null)"; then
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
unset PROTON_PASS_REF_ROOT
unset PROTON_PASS_USERNAME_REF
unset PROTON_PASS_PASSWORD_REF
unset PROTON_USERNAME
unset PROTON_PASSWORD
EOF
