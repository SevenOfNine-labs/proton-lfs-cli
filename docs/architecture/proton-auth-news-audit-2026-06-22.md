# Proton Auth News Audit - 2026-06-22

This note records the latest Proton Drive CLI/SDK signals reviewed before any
real Proton login canary. No live login, browser-fork login, or transfer was
attempted during this audit.

## Sources Reviewed

- Proton blog: `https://proton.me/blog/proton-drive-cli`
- Proton support: `https://proton.me/support/drive-cli`
- Proton blog: `https://proton.me/blog/drive-sdk-january-2026`
- Proton blog: `https://proton.me/blog/drive-sdk-june-2026`
- Official SDK upstream: `https://github.com/ProtonDriveApps/sdk`
- Migrated SDK tree: `ProtonDriveApps/sdk@a3fc5e54`
- Previous SDK tree: `ProtonDriveApps/sdk@f21e74cc`
- Migrated drive-cli submodule: `SevenOfNine-labs/proton-drive-cli@b97563b`

## Findings

### Official Proton Drive CLI

- Proton now ships an official `proton-drive` CLI for Windows, macOS, and
  Linux. The public examples use `proton-drive auth login`, then
  `filesystem ...` and `sharing ...` commands for one-shot automation.
- The official CLI is built on the Proton Drive SDK, but its authentication
  layer remains part of the CLI/application, not the SDK business-logic layer.
- The official CLI README in the fetched SDK tree says sign-in uses the browser
  and stores session material in the OS secret store. It does not model
  headless SRP login inside file-transfer commands.
- The official CLI browser flow uses the session-fork endpoint family and a
  browser URL at `https://account.proton.me/desktop/login?app=drive&pv=3`.
  The fork payload returns user key password material through an encrypted
  payload. This matches our prior browser-fork direction more closely than
  direct SRP inside transfers.

### SDK Scope and Auth Ownership

- The latest SDK README still states that the SDK does not include
  authentication/login flows, session management, or user address providers.
  Integrating applications must supply those pieces.
- The SDK usage guidance continues to require honest third-party
  `x-pm-appversion` identity, SDK-aligned behavior, event-based sync, caching,
  bounded parallelism, and exponential backoff. Unsafe or spoofed clients can be
  rate-limited or blocked.
- The January 2026 and June 2026 Proton SDK/Drive posts emphasize faster and
  more reliable SDK-backed file operations. They do not move account login or
  session ownership into the SDK.

### Upstream SDK Migration

- The nested SDK remote `origin/main` currently resolves to
  `a3fc5e54984011da9f4b73ae63fd9701830842df` (`docs: add readme for client
  module`).
- The previous SDK tree was `f21e74ccf326d05c0d6efe2cdada89f7000fada6`
  (`cli/v0.4.6`, `Fix persisting content key packet in crypto cache`).
- The official `main` ref force-moved between the local refs and reorganized
  key paths, including `js/cli -> cli`, `js/sdk -> client/js`, `cs ->
  client/cs`, and Kotlin/Swift paths under `incubating/client`.
- `proton-drive-cli@b97563b` completed the drive-cli-first migration by
  changing the SDK portal path to `portal:./submodules/sdk/client/js`, updating
  build/docs workflows, adapting the new `NodeEntity` result shape, adding the
  SDK-required SRP salt method, and bundling the runtime so Node can load the
  latest SDK safely.

## Local Decision

- The SDK layout migration is now pinned through the root
  `submodules/proton-drive-cli` pointer, after the drive-cli slice passed
  lint, build, docs generation, targeted SDK/bridge tests, and the full mocked
  Jest suite.
- Keep treating SDK updates as drive-cli-first changes: update and push
  `proton-drive-cli`, then update the root submodule pointer and rerun root
  checks.
- Do not run a real Proton login or canary merely because the SDK is now
  migrated; the canary gate remains separate.

## Impact on Our Auth Plan

- Keep transfer initialization gated by offline `bridge auth-state`.
- Keep `allowLogin=false` on all transfer commands.
- Keep refusing unsafe states (`needs_login`, `login_available`,
  `needs_data_password`, `needs_key_password`, `session_expired`,
  `session_invalid`, and `configuration_error`) without attempting SRP.
- Treat a future official-CLI-aligned browser-fork path as the safer canary
  direction, but only after offline gates pass and the disposable-account
  canary acknowledgement is explicit.
- Continue storing account/data/key-password secrets in local credential
  providers or OS secret stores, never in root adapter config and never in Git
  LFS transfer messages.

## Completed Migration Slice

Completed in `proton-drive-cli@b97563b` and this root pointer/docs update:

1. Updated `proton-drive-cli/package.json` from the old SDK package path to
   `portal:./submodules/sdk/client/js`.
2. Updated build/test/docs scripts and TypeScript adapter assumptions for the
   new SDK layout.
3. Kept auth behavior unchanged: transfer commands still check offline
   `auth-state` first and still send `allowLogin=false`.
4. Ran full drive-cli lint/build/docs/test validation without real Proton
   login.
5. Updated the root submodule pointer and root workflows for the new nested SDK
   path.
