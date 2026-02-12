#!/usr/bin/env bash
set -euo pipefail

PASS_REF_ROOT="${PROTON_PASS_REF_ROOT:-pass://Personal/Proton Git LFS}"
PASS_REF_ROOT="${PASS_REF_ROOT%/}"
USERNAME_REF="${PROTON_PASS_USERNAME_REF:-${PASS_REF_ROOT}/username}"
PASSWORD_REF="${PROTON_PASS_PASSWORD_REF:-${PASS_REF_ROOT}/password}"

MOCK_USERNAME="${PASS_MOCK_USERNAME:-integration-user@proton.test}"
MOCK_PASSWORD="${PASS_MOCK_PASSWORD:-integration-password}"

json_escape() {
  local value="${1:-}"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s' "$value"
}

if [[ "${1:-}" == "--version" ]]; then
  echo "pass-cli mock 0.0.0"
  exit 0
fi

if [[ "${1:-}" == "user" && "${2:-}" == "info" ]]; then
  if [[ "${3:-}" == "--output" && "${4:-}" == "json" ]]; then
    printf '{"email":"%s"}\n' "$(json_escape "$MOCK_USERNAME")"
    exit 0
  fi
  printf 'Email: %s\n' "$MOCK_USERNAME"
  exit 0
fi

if [[ "${1:-}" == "item" && "${2:-}" == "view" ]]; then
  OUTPUT_JSON="false"
  REF=""

  if [[ "${3:-}" == "--output" && "${4:-}" == "json" ]]; then
    OUTPUT_JSON="true"
    REF="${5:-}"
  else
    REF="${3:-}"
  fi

  if [[ -z "$REF" ]]; then
    echo "missing reference" >&2
    exit 2
  fi

  VALUE=""
  case "$REF" in
    "$USERNAME_REF")
      VALUE="$MOCK_USERNAME"
      ;;
    "$PASSWORD_REF")
      VALUE="$MOCK_PASSWORD"
      ;;
    *)
      echo "reference not found: $REF" >&2
      exit 1
      ;;
  esac

  if [[ "$OUTPUT_JSON" == "true" ]]; then
    printf '{"value":"%s"}\n' "$(json_escape "$VALUE")"
  else
    printf '%s\n' "$VALUE"
  fi
  exit 0
fi

echo "unsupported command: $*" >&2
exit 2
