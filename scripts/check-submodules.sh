#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

tree_gitlink_sha() {
  local parent="$1"
  local path="$2"
  git -C "$parent" ls-tree HEAD "$path" | awk '$2 == "commit" { print $3 }'
}

check_gitlink() {
  local parent="$1"
  local path="$2"
  local label="$3"
  local expected actual status

  expected="$(tree_gitlink_sha "$parent" "$path")"
  if [ -z "$expected" ]; then
    fail "$label is not recorded as a gitlink at $parent/$path"
  fi

  if [ ! -e "$parent/$path/.git" ]; then
    fail "$label is not initialized at $parent/$path"
  fi

  actual="$(git -C "$parent/$path" rev-parse HEAD)"
  if [ "$actual" != "$expected" ]; then
    fail "$label is checked out at $actual, expected $expected"
  fi

  status="$(git -C "$parent/$path" status --porcelain)"
  if [ -n "$status" ]; then
    echo "$status" >&2
    fail "$label has uncommitted changes"
  fi

  printf 'OK %-18s %s\n' "$label" "$actual"
}

check_gitlink "$ROOT" "submodules/git-lfs" "git-lfs"
check_gitlink "$ROOT" "submodules/pass-cli" "pass-cli"
check_gitlink "$ROOT" "submodules/proton-drive-cli" "proton-drive-cli"
check_gitlink "$ROOT/submodules/proton-drive-cli" "submodules/sdk" "Proton SDK"

sdk_gitlinks="$(
  git -C "$ROOT/submodules/proton-drive-cli/submodules/sdk" ls-tree -r HEAD |
    awk '$2 == "commit" { print $4 }'
)"
if [ -n "$sdk_gitlinks" ]; then
  echo "NOTE: Proton SDK contains nested gitlinks not declared in .gitmodules:"
  while IFS= read -r sdk_gitlink; do
    printf '  %s\n' "$sdk_gitlink"
  done <<<"$sdk_gitlinks"
  echo "NOTE: Skipping recursive SDK descent; root-owned submodule pins are verified."
fi

echo "Submodule check passed."
