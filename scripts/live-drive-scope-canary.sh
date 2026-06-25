#!/usr/bin/env bash
set -euo pipefail

LIVE_CANARY_ACK_VALUE="I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT"
ROOT_DIR="${ROOT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
NODE_BIN="${NODE_BIN:-node}"
DRIVE_CLI_BIN="${DRIVE_CLI_BIN:-${PROTON_DRIVE_CLI_BIN:-${ROOT_DIR}/submodules/proton-drive-cli/dist/index.js}}"

fail() {
  printf '%s\n' "$*" >&2
  exit 2
}

if [[ "${PROTON_LFS_LIVE_CANARY:-}" != "${LIVE_CANARY_ACK_VALUE}" ]]; then
  fail "Refusing to run live Drive scope canary without the exact PROTON_LFS_LIVE_CANARY acknowledgement."
fi

if [[ -z "${LIVE_CANARY_DOCTOR_ARGS:-}" ]]; then
  fail "Refusing to run live Drive scope canary without LIVE_CANARY_DOCTOR_ARGS."
fi

cd "${ROOT_DIR}"

echo "Running guarded live Drive scope canary."
echo "This performs exactly one read-only bridge list request and no transfer."

build_request_script='
const raw = (process.env.LIVE_CANARY_DOCTOR_ARGS || "").trim();
const tokens = raw ? raw.split(/\s+/) : [];
const request = { folder: "/" };
const valueOptions = new Set([
  "--data-credential-provider",
  "--data-credential-host",
  "--key-password-provider",
  "--key-password-host",
]);
const flagOptions = new Set(["--require-data-password"]);

function setOption(option, value) {
  if (!value || value.startsWith("--")) {
    throw new Error(`missing value after ${option}`);
  }
  if (option === "--data-credential-provider") request.dataCredentialProvider = value;
  if (option === "--data-credential-host") request.dataCredentialHost = value;
}

for (let i = 0; i < tokens.length; i += 1) {
  const token = tokens[i];
  const eq = token.indexOf("=");
  const option = eq === -1 ? token : token.slice(0, eq);
  const inlineValue = eq === -1 ? "" : token.slice(eq + 1);

  if (flagOptions.has(option)) continue;
  if (!valueOptions.has(option)) {
    throw new Error(`unsupported LIVE_CANARY_DOCTOR_ARGS option for scope canary: ${option}`);
  }
  if (eq !== -1) {
    setOption(option, inlineValue);
    continue;
  }
  i += 1;
  setOption(option, tokens[i] || "");
}

console.log(JSON.stringify(request));
'

set +e
scope_request="$(LIVE_CANARY_DOCTOR_ARGS="${LIVE_CANARY_DOCTOR_ARGS}" "${NODE_BIN}" -e "${build_request_script}" 2>&1)"
request_status=$?
set -e
if [[ "${request_status}" -ne 0 ]]; then
  fail "Failed to build live Drive scope request: ${scope_request}"
fi

set +e
bridge_output="$(
  printf '%s\n' "${scope_request}" | "${NODE_BIN}" "${DRIVE_CLI_BIN}" bridge list 2>&1
)"
bridge_status=$?
set -e

parse_script='
const fs = require("fs");

const raw = fs.readFileSync(0, "utf8");
const jsonLine = raw
  .split(/\r?\n/)
  .map((line) => line.trim())
  .filter(Boolean)
  .reverse()
  .find((line) => line.startsWith("{") && line.endsWith("}"));

if (!jsonLine) {
  console.error("Live Drive scope canary could not parse a bridge JSON response.");
  process.exit(2);
}

let response;
try {
  response = JSON.parse(jsonLine);
} catch (error) {
  console.error(`Live Drive scope canary could not parse bridge JSON: ${error.message}`);
  process.exit(2);
}

let details = {};
if (typeof response.details === "string" && response.details.trim()) {
  try {
    details = JSON.parse(response.details);
  } catch {
    details = {};
  }
} else if (response.details && typeof response.details === "object") {
  details = response.details;
}

const errorText = String(response.error || "");
const errorCode = String(details.errorCode || "");
const protonCode = Number(details.protonCode || 0);

if (response.ok === true) {
  console.log("Live Drive scope canary passed. The saved session can perform one read-only Drive metadata request.");
  process.exit(0);
}

if (
  errorCode === "INSUFFICIENT_SCOPE" ||
  protonCode === 9101 ||
  errorText.includes("9101") ||
  /sufficient scope/i.test(errorText)
) {
  console.error("Live Drive scope canary hard stop: INSUFFICIENT_SCOPE / Proton API 9101.");
  console.error("The saved session is locally ready, but Proton rejected the app/session scope for Drive API calls.");
  console.error("Do not retry login loops or run real LFS transfers until the app/session scope is resolved.");
  process.exit(3);
}

console.error(`Live Drive scope canary failed: ${errorText || "bridge list returned ok=false"}`);
process.exit(2);
'

set +e
printf '%s\n' "${bridge_output}" | "${NODE_BIN}" -e "${parse_script}"
parse_status=$?
set -e

if [[ "${parse_status}" -ne 0 ]]; then
  exit "${parse_status}"
fi

if [[ "${bridge_status}" -ne 0 ]]; then
  fail "Bridge list subprocess exited with ${bridge_status} after returning a parseable response."
fi

echo "Live Drive scope canary completed. No upload, download, delete, or init was attempted."
