#!/usr/bin/env bash
set -euo pipefail

LIVE_CANARY_ACK_VALUE="I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT"
ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
NODE_BIN="${NODE_BIN:-node}"
GO_BIN="${GO_BIN:-go}"
DRIVE_CLI_BIN="${DRIVE_CLI_BIN:-${PROTON_DRIVE_CLI_BIN:-${ROOT_DIR}/submodules/proton-drive-cli/dist/index.js}}"

fail() {
  printf '%s\n' "$*" >&2
  exit 2
}

split_canary_args() {
  local name="$1"
  local raw="$2"

  if [[ -z "${raw//[[:space:]]/}" ]]; then
    fail "${name} is required"
  fi

  read -r -a SPLIT_CANARY_ARGS <<<"${raw}"
  if [[ "${#SPLIT_CANARY_ARGS[@]}" -eq 0 ]]; then
    fail "${name} is required"
  fi

  local token
  for token in "${SPLIT_CANARY_ARGS[@]}"; do
    if [[ ! "${token}" =~ ^[A-Za-z0-9._=:/@,+-]+$ ]]; then
      fail "${name} contains unsupported token: ${token}"
    fi
  done
}

validate_browser_fork_login_args() {
  local i=0
  local token
  local value

  while [[ "${i}" -lt "${#login_args[@]}" ]]; do
    token="${login_args[${i}]}"
    case "${token}" in
      --auth-mode | --auth-mode=*)
        fail "LIVE_BROWSER_FORK_LOGIN_ARGS must not set --auth-mode; login is browser-fork-only."
        ;;
      --key-password-provider | --key-password-host)
        i=$((i + 1))
        if [[ "${i}" -ge "${#login_args[@]}" ]]; then
          fail "LIVE_BROWSER_FORK_LOGIN_ARGS missing value after ${token}."
        fi
        value="${login_args[${i}]}"
        if [[ "${value}" == --* ]]; then
          fail "LIVE_BROWSER_FORK_LOGIN_ARGS missing value after ${token}."
        fi
        ;;
      --key-password-provider=* | --key-password-host=*)
        value="${token#*=}"
        if [[ -z "${value}" ]]; then
          fail "LIVE_BROWSER_FORK_LOGIN_ARGS contains empty value for ${token%%=*}."
        fi
        ;;
      *)
        fail "LIVE_BROWSER_FORK_LOGIN_ARGS contains unsupported login option: ${token}. Allowed options: --key-password-provider and --key-password-host."
        ;;
    esac
    i=$((i + 1))
  done
}

if [[ "${PROTON_LFS_LIVE_CANARY:-}" != "${LIVE_CANARY_ACK_VALUE}" ]]; then
  fail "Refusing to run browser-fork canary without the exact PROTON_LFS_LIVE_CANARY acknowledgement."
fi

declare -a doctor_args
declare -a login_args
declare -a SPLIT_CANARY_ARGS
split_canary_args "LIVE_CANARY_DOCTOR_ARGS" "${LIVE_CANARY_DOCTOR_ARGS:-}"
doctor_args=("${SPLIT_CANARY_ARGS[@]}")
split_canary_args "LIVE_BROWSER_FORK_LOGIN_ARGS" "${LIVE_BROWSER_FORK_LOGIN_ARGS:-}"
login_args=("${SPLIT_CANARY_ARGS[@]}")
validate_browser_fork_login_args

cd "${ROOT_DIR}"

echo "Running guarded browser-fork canary."
echo "This runs exactly one browser-fork login command, then local inspection only."
"${NODE_BIN}" "${DRIVE_CLI_BIN}" login "${login_args[@]}"

echo "Inspecting saved local session..."
"${NODE_BIN}" "${DRIVE_CLI_BIN}" status

echo "Running offline doctor after browser-fork login..."
if ! doctor_output="$("${NODE_BIN}" "${DRIVE_CLI_BIN}" doctor --json "${doctor_args[@]}")"; then
  printf '%s\n' "${doctor_output}"
  fail "Offline doctor failed after browser-fork login."
fi
printf '%s\n' "${doctor_output}"

if ! printf '%s\n' "${doctor_output}" | "${GO_BIN}" run ./scripts/check_doctor_readiness.go --require-auth-mode browser-fork --require-state ready --require-transfer --quiet; then
  fail "Offline doctor did not mark transfers ready after browser-fork login."
fi

echo "Browser-fork canary inspection passed. No transfer was attempted."
