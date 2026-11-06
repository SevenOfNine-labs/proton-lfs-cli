#!/usr/bin/env bash
set -euo pipefail

md_files_file=$(mktemp)
matches_file=$(mktemp)
trap 'rm -f "${md_files_file}" "${matches_file}"' EXIT

# Tracked Markdown files only (git ls-files honours .gitignore on every host,
# unlike find), excluding submodules.
git ls-files -z -- "*.md" ":!submodules/**" | tr "\0" "\n" | sort >"${md_files_file}"

if [[ ! -s "${md_files_file}" ]]; then
  echo "No markdown files found"
  exit 0
fi

missing=0
file_count=0

while IFS= read -r md_file; do
  [[ -z "${md_file}" ]] && continue
  file_count=$((file_count + 1))
  perl -ne 'while(/\[[^\]]+\]\(([^)]+)\)/g){print "$ARGV\t$.\t$1\n"}' "${md_file}" >>"${matches_file}"
done <"${md_files_file}"

while IFS=$'\t' read -r file line target; do
  [[ -z "${file}" ]] && continue
  [[ -z "${target}" ]] && continue

  if [[ "${target}" == \#* ]]; then
    continue
  fi

  if [[ "${target}" =~ ^[a-zA-Z][a-zA-Z0-9+.-]*: ]]; then
    continue
  fi

  candidate="${target%%#*}"
  candidate="${candidate#<}"
  candidate="${candidate%>}"

  if [[ -z "${candidate}" ]]; then
    continue
  fi

  basedir=$(dirname "${file}")

  # Skip targets inside submodules (not checked out in CI)
  if [[ "${candidate}" == *submodules/* ]]; then
    continue
  fi

  if [[ -e "${basedir}/${candidate}" || -e "${candidate}" ]]; then
    continue
  fi

  echo "Missing link target: ${file}:${line} -> ${target}"
  missing=1
done <"${matches_file}"

if [[ ${missing} -ne 0 ]]; then
  exit 1
fi

echo "Markdown link check passed for ${file_count} files"
