#!/usr/bin/env bash
# Stand-in for pass-cli 2.7.0 in integration tests: one owned vault holding one
# key-password login item, in the JSON shapes that proton-drive-cli's
# src/credentials/pass-cli.ts documents for each pass-cli command.
set -euo pipefail

MOCK_VAULT_NAME="${MOCK_PASS_VAULT_NAME:-Personal}"
MOCK_ITEM_HOST="${MOCK_PASS_ITEM_HOST:-proton-drive-key.proton-lfs-cli.local}"
MOCK_ITEM_TITLE="${MOCK_PASS_ITEM_TITLE:-Proton ${MOCK_ITEM_HOST}}"
MOCK_USERNAME="${MOCK_PASS_USERNAME:-integration-user@proton.test}"
MOCK_PASSWORD="${MOCK_PASS_PASSWORD:-integration-key-password}"
MOCK_SHARE_ID="mock-share-id"
MOCK_VAULT_ID="mock-vault-id"
MOCK_ITEM_ID="mock-item-id"

json_escape() {
  local value="${1:-}"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '%s' "$value"
}

has_arg() {
  local needle="$1"
  shift
  local arg
  for arg in "$@"; do
    if [[ "$arg" == "$needle" ]]; then
      return 0
    fi
  done
  return 1
}

case "${1:-}" in
  --version)
    echo "pass-cli mock 0.0.0"
    exit 0
    ;;
  info)
    printf -- '- Release track: stable\n- ID: mock-user-id\n- Username: %s\n- Email: %s\n- Session has lock: no\n' \
      "$MOCK_USERNAME" "$MOCK_USERNAME"
    exit 0
    ;;
  share)
    if [[ "${2:-}" == "list" ]] && has_arg json "$@"; then
      printf '{"shares":[{"id":"%s","name":"%s","share_type":"Vault","share_role":"Owner"}]}\n' \
        "$MOCK_SHARE_ID" "$(json_escape "$MOCK_VAULT_NAME")"
      exit 0
    fi
    ;;
  vault)
    if [[ "${2:-}" == "list" ]] && has_arg json "$@"; then
      printf '{"vaults":[{"name":"%s","vault_id":"%s","share_id":"%s"}]}\n' \
        "$(json_escape "$MOCK_VAULT_NAME")" "$MOCK_VAULT_ID" "$MOCK_SHARE_ID"
      exit 0
    fi
    ;;
  item)
    case "${2:-}" in
      list)
        if has_arg json "$@"; then
          printf '{"items":[{"id":"%s","share_id":"%s","vault_id":"%s","state":"active","flags":[],"create_time":"2026-01-01T00:00:00","modify_time":"2026-01-01T00:00:00","title":"%s","item_type":"login"}]}\n' \
            "$MOCK_ITEM_ID" "$MOCK_SHARE_ID" "$MOCK_VAULT_ID" "$(json_escape "$MOCK_ITEM_TITLE")"
          exit 0
        fi
        ;;
      view)
        if ! has_arg "$MOCK_ITEM_ID" "$@"; then
          echo "item not found" >&2
          exit 1
        fi
        if has_arg json "$@"; then
          printf '{"item":{"id":"%s","share_id":"%s","vault_id":"%s","content":{"title":"%s","note":"","item_uuid":"mock-item-uuid","content":{"Login":{"email":"","username":"%s","password":"%s","urls":["https://%s"],"totp_uri":"","passkeys":[]}},"extra_fields":[]},"state":"active","flags":[]},"attachments":[]}\n' \
            "$MOCK_ITEM_ID" "$MOCK_SHARE_ID" "$MOCK_VAULT_ID" \
            "$(json_escape "$MOCK_ITEM_TITLE")" "$(json_escape "$MOCK_USERNAME")" "$(json_escape "$MOCK_PASSWORD")" \
            "$(json_escape "$MOCK_ITEM_HOST")"
        else
          printf -- '- Title: %s\n- ID: %s\n- ShareID: %s\n' "$MOCK_ITEM_TITLE" "$MOCK_ITEM_ID" "$MOCK_SHARE_ID"
        fi
        exit 0
        ;;
      create)
        if has_arg --from-template "$@"; then
          cat >/dev/null
        fi
        exit 0
        ;;
      delete)
        exit 0
        ;;
    esac
    ;;
esac

echo "unsupported command: $*" >&2
exit 2
