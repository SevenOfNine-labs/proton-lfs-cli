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
- Local fetched SDK tree: `ProtonDriveApps/sdk@a3fc5e54`
- Current pinned SDK tree: `ProtonDriveApps/sdk@f21e74cc`

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

### Upstream SDK Pin Check

- The nested SDK remote `origin/main` currently resolves to
  `a3fc5e54984011da9f4b73ae63fd9701830842df` (`docs: add readme for client
  module`).
- Our pinned SDK remains `f21e74ccf326d05c0d6efe2cdada89f7000fada6`
  (`cli/v0.4.6`, `Fix persisting content key packet in crypto cache`).
- The remote update is not a fast-forward from our current pin. The fetched
  tree reorganizes key paths, including `js/cli -> cli`, `js/sdk -> client/js`,
  `cs -> client/cs`, and Kotlin/Swift paths under `incubating/client`.
- Our `proton-drive-cli` package currently depends on the nested SDK through
  `portal:./submodules/sdk/js/sdk`. Updating the SDK pin directly would remove
  that path and break dependency resolution/builds until the package path and
  any import assumptions are migrated.

## Local Decision

- Do not update `submodules/proton-drive-cli/submodules/sdk` in this pass.
- Treat the fetched `a3fc5e54` SDK tree as an upstream migration signal, not a
  safe patch-level update.
- Keep the current SDK pin `f21e74cc` until a dedicated migration slice updates
  the package portal path, build scripts, docs, and tests to the new upstream
  layout.

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

## Follow-Up Migration Slice

Before updating to `ProtonDriveApps/sdk@a3fc5e54` or later:

1. Update `proton-drive-cli/package.json` from
   `portal:./submodules/sdk/js/sdk` to the new upstream client package path.
2. Update build/test scripts and TypeScript imports for the new SDK layout.
3. Compare the official CLI browser-auth and secret-store implementation with
   our browser-fork/key-password store.
4. Run full `proton-drive-cli` lint/build/test.
5. Update the root submodule pointer and rerun `make check-submodules`,
   `make test`, and `make test-e2e-mock`.
