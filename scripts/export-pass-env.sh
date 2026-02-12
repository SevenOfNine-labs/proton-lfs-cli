#!/usr/bin/env bash
set -euo pipefail

# Standardized Proton Pass references for this project.
DEFAULT_PASS_CLI_BIN="${PROTON_PASS_CLI_BIN:-pass-cli}"
DEFAULT_PASS_REF_ROOT="${PROTON_PASS_REF_ROOT:-pass://Personal/Proton Git LFS}"

usage() {
  cat <<'EOF'
Usage:
  eval "$(scripts/export-pass-env.sh)"

Options:
  --pass-cli <bin>        pass-cli binary path (default: pass-cli)
  --ref-root <pass://..>  base reference root (default: pass://Personal/Proton Git LFS)
  --username-ref <ref>    explicit username reference
  --password-ref <ref>    explicit password reference
  --skip-check            do not validate references through pass-cli
  -h, --help              show this help
EOF
}

PASS_CLI_BIN="$DEFAULT_PASS_CLI_BIN"
PASS_REF_ROOT="$DEFAULT_PASS_REF_ROOT"
PASS_USERNAME_REF=""
PASS_PASSWORD_REF=""
SKIP_CHECK="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --pass-cli)
      PASS_CLI_BIN="$2"
      shift 2
      ;;
    --ref-root)
      PASS_REF_ROOT="$2"
      shift 2
      ;;
    --username-ref)
      PASS_USERNAME_REF="$2"
      shift 2
      ;;
    --password-ref)
      PASS_PASSWORD_REF="$2"
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

PASS_REF_ROOT="${PASS_REF_ROOT%/}"
if [[ -z "$PASS_REF_ROOT" ]]; then
  echo "Invalid pass ref root" >&2
  exit 2
fi

if [[ -z "$PASS_USERNAME_REF" ]]; then
  PASS_USERNAME_REF="${PASS_REF_ROOT}/username"
fi
if [[ -z "$PASS_PASSWORD_REF" ]]; then
  PASS_PASSWORD_REF="${PASS_REF_ROOT}/password"
fi

if [[ "$PASS_USERNAME_REF" != pass://* ]]; then
  echo "Username reference must start with pass:// (got: $PASS_USERNAME_REF)" >&2
  exit 2
fi
if [[ "$PASS_PASSWORD_REF" != pass://* ]]; then
  echo "Password reference must start with pass:// (got: $PASS_PASSWORD_REF)" >&2
  exit 2
fi

if [[ "$SKIP_CHECK" != "true" ]]; then
  if ! command -v "$PASS_CLI_BIN" >/dev/null 2>&1; then
    echo "pass-cli binary not found: $PASS_CLI_BIN" >&2
    exit 1
  fi

  if ! user_info_err="$("$PASS_CLI_BIN" user info --output json 2>&1 >/dev/null)"; then
    echo "pass-cli is not authenticated. Run 'pass-cli login' first." >&2
    if [[ -n "$user_info_err" ]]; then
      echo "$user_info_err" >&2
    fi
    exit 1
  fi

  if ! username_err="$("$PASS_CLI_BIN" item view --output json "$PASS_USERNAME_REF" 2>&1 >/dev/null)"; then
    echo "failed to resolve username reference: $PASS_USERNAME_REF" >&2
    if [[ -n "$username_err" ]]; then
      echo "$username_err" >&2
    fi
    exit 1
  fi

  if ! password_err="$("$PASS_CLI_BIN" item view --output json "$PASS_PASSWORD_REF" 2>&1 >/dev/null)"; then
    echo "failed to resolve password reference: $PASS_PASSWORD_REF" >&2
    if [[ -n "$password_err" ]]; then
      echo "$password_err" >&2
    fi
    exit 1
  fi
fi

cat <<EOF
export PROTON_PASS_CLI_BIN='${PASS_CLI_BIN}'
export PROTON_PASS_REF_ROOT='${PASS_REF_ROOT}'
export PROTON_PASS_USERNAME_REF='${PASS_USERNAME_REF}'
export PROTON_PASS_PASSWORD_REF='${PASS_PASSWORD_REF}'
unset PROTON_USERNAME
unset PROTON_PASSWORD
EOF
