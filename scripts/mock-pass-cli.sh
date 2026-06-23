#!/usr/bin/env bash
set -euo pipefail

MOCK_VAULT_NAME="${MOCK_PASS_VAULT_NAME:-Personal}"
MOCK_ITEM_TITLE="${MOCK_PASS_ITEM_TITLE:-Proton}"
MOCK_USERNAME="${MOCK_PASS_USERNAME:-integration-user@proton.test}"
MOCK_PASSWORD="${MOCK_PASS_PASSWORD:-integration-password}"

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

if [[ "${1:-}" == "test" ]]; then
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

if [[ "${1:-}" == "vault" && "${2:-}" == "list" && "${3:-}" == "--output" && "${4:-}" == "json" ]]; then
  printf '{"vaults":[{"name":"%s"}]}\n' "$(json_escape "$MOCK_VAULT_NAME")"
  exit 0
fi

if [[ "${1:-}" == "item" && "${2:-}" == "list" ]]; then
  printf '{"items":[{"name":"%s","username":"%s","urls":["https://proton.me"]}]}\n' \
    "$(json_escape "$MOCK_ITEM_TITLE")" \
    "$(json_escape "$MOCK_USERNAME")"
  exit 0
fi

if [[ "${1:-}" == "item" && "${2:-}" == "view" ]]; then
  OUTPUT_JSON="false"
  REF="$MOCK_ITEM_TITLE"

  if [[ "${3:-}" == "--output" && "${4:-}" == "json" ]]; then
    OUTPUT_JSON="true"
  elif [[ "${3:-}" != --* ]]; then
    REF="${3:-}"
  fi

  if [[ -z "$REF" ]]; then
    echo "missing reference" >&2
    exit 2
  fi

  if [[ "$REF" != "$MOCK_ITEM_TITLE" && "$*" != *"--item-title $MOCK_ITEM_TITLE"* ]]; then
    echo "item not found: $REF" >&2
    exit 1
  fi

  if [[ "$OUTPUT_JSON" == "true" ]]; then
    printf '{"item":{"content":{"title":"%s","content":{"Login":{"username":"%s","password":"%s","urls":["https://proton.me"]}}}}}\n' \
      "$(json_escape "$MOCK_ITEM_TITLE")" \
      "$(json_escape "$MOCK_USERNAME")" \
      "$(json_escape "$MOCK_PASSWORD")"
  else
    printf 'Username: %s\nPassword: %s\nURL: https://proton.me\n' "$MOCK_USERNAME" "$MOCK_PASSWORD"
  fi
  exit 0
fi

echo "unsupported command: $*" >&2
exit 2
