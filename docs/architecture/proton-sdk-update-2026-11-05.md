# Proton SDK Update Audit - 2026-11-05

This note records the review done while moving the vendored official Proton
Drive SDK from `a3fc5e54` (js/v0.18.0 era, pinned 2026-06-22) to tag
`js/v0.23.0` (`8a44654a`, 2026-10-09), and what was adapted in
`proton-drive-cli` and the root adapter because of it. No live Proton login or
transfer was attempted.

## Sources Reviewed

- Local SDK checkout at `js/v0.23.0`: `README.md`, `client/README.md`,
  `client/js/{README,CHANGELOG}.md`, `client/js/src/**` diff against
  `a3fc5e54`, `cli/**` (official Bun CLI, tag `cli/v0.9.0`),
  `incubating/account/js/**` (official account/auth module).
- `origin/main` of the SDK (1161 commits ahead of the old pin; only four touch
  `client/js` after the tag, none of them interface changes).
- proton-drive-cli sources, tests, build scripts and CI; root Go constants,
  CI, Makefile and operator docs.

## Upstream Changes Since the Old Pin

Releases: js/v0.18.1, 0.19.0, 0.19.1, 0.19.2, 0.20.0, 0.20.1, 0.21.0, 0.21.1,
0.21.2, 0.21.3, 0.22.0, 0.22.1, 0.22.2, 0.23.0.

Public interface changes (`client/js/src/interface`, `protonDriveClient.ts`):

| Change | Used by proton-drive-cli? |
| --- | --- |
| `NodeEntity.activeRevision` is a plain `Revision` (was `Result<Revision>`) | no |
| `NodeEntity.isSharedPublicly` -> `isSharedByUrl`; "public link" renamed "URL access" everywhere (`PublicLink` -> `URLAccess`, `publicLink` -> `urlAccess`, `experimental.getPublicLinkInfo/authPublicLink` -> `getURLAccessInfo/authURLAccess`) | no |
| `UploadMetadata.expectedSize: number \| null` (explicit opt-out of size checks) | we always pass a number |
| `Device.lastSyncDate` -> `lastSyncTime`; `folder.isImported`, `Revision.isImported` | no |
| `getAvailableName` requires the node type; new `getFolderSize` | no |
| Report-abuse APIs, `AbuseCategory`, folder-size info, telemetry recency/creator fields | no |
| New optional constructor parameter `searchServiceProvider` (experimental search) | no |
| `FeatureFlags.DriveCryptoEncryptBlocksWithPgpAead` removed | no |
| Error messages: "Unknown error" -> "Something went wrong" | string matching in `bridge/validators.ts` does not depend on it |

Result: `tsc` passes against the new SDK without source changes; every
proton-drive-cli adaptation below is about build, identity or auth, not the
Drive API surface.

Build and runtime changes:

- `client/js` is built with swc and emits **ESM** (`dist/index.js` uses
  `import`); previously `tsc` emitted CommonJS. Node and Jest need a transform
  (esbuild already handled it; Jest did not).
- `client/js/package-lock.json` was deleted upstream when the SDK moved to Bun
  (2026-07-22). `npm ci` no longer works; `npm install --no-package-lock` does
  and keeps the checkout clean for `make check-submodules`.
- New runtime dependencies `@boltffi/runtime`, `comlink`, optional
  `@xmldom/xmldom` and `exifreader`. The search engine is Rust/wasm and is
  only pulled in by consumers that import `search/index`; our bundle contains
  no wasm (verified).
- The SDK relies on `Array.fromAsync`, `Promise.withResolvers` and the
  `Uint8Array` base64/hex helpers. Node 22 provides the first two; the helpers
  are polyfilled by `src/auth/srp/uint8array-extensions.ts`, loaded first in
  `src/index.ts`. `engines.node` was raised from 18 to 22.
- `client/js/package.json` `version` is a `0.0.1` placeholder in git; Proton
  stamps the real version only at npm publish time, so the vendored SDK would
  announce `js@0.0.1` in `x-pm-drive-sdk-version`.

Repository-level changes:

- The SDK repository now contains Proton's **official CLI** (`cli/`, Bun 1.4,
  downloads at proton.me/download/drive/cli) and an **official account module**
  (`incubating/account/js`, `proton-drive-sdk-account`: ky-based `ApiClient`,
  `Auth.authViaWeb`/`authViaPassword`, `Addresses`, `Srp`). The README states
  the module is a temporary solution until an Account SDK exists and will not
  be published.
- The README now publishes the exact third-party identity grammar for
  `x-pm-appversion` and announces a **cryptographic model migration**
  (targeted end of 2026 / early 2027) after which clients on older SDK
  releases stop interoperating. Proton signals required/suggested SDK versions
  through `x-pm-drive-requirements` / `x-pm-drive-platform-requirements`
  response headers, which the official CLI evaluates after every response.

## App Identification

- Grammar: `external-drive-{name}@{major.minor.patch}[-stable|beta|alpha][+build]`,
  `{name}` lowercase letters/underscores. `cli-drive` is reserved for official
  builds (`cli-drive-<distro>` for unmodified repackaging).
- Our default `external-drive-protonlfscli@0.1.2` already complied. The
  validator in `src/constants.ts` previously accepted any `@version` suffix and
  the `cli-drive` identity; it now enforces the published grammar, rejects
  `cli-drive`, and derives the version part from `package.json`.
  `resolveAuthClientId` always forks sessions as `external-drive`.
- `.env.example` shipped `PROTON_APP_VERSION=external-drive-protonlfs@dev`,
  which the Go adapter forwards verbatim and proton-drive-cli now rejects
  before any network call; the example is empty with the grammar documented.
- `x-pm-drive-sdk-version` is overridden with the pinned release
  (`VENDORED_DRIVE_SDK_VERSION`, checked against `git describe` of the
  submodule by `constants.test.ts`), as the official CLI does with its
  build-time `SDK_VERSION`.
- Root Go constants (`internal/config/config.go` `DefaultDriveCLIAppVersion`,
  tray scope diagnostics) still equal the TypeScript default; no Go change was
  required. Adding the optional `-alpha` channel would be honest for a
  pre-alpha project but changes the Go constant and its tests; left as an
  option.

## Authentication Compared With the Official Account Module

| Aspect | Official (`incubating/account/js`, `cli/`) | proton-drive-cli | Outcome |
| --- | --- | --- | --- |
| Fork init / status endpoints, sign-in URL (`/desktop/login?app=drive&pv=3`), AES-256-GCM payload with AAD `fork` | same | same | no change |
| Fork polling | 5 s initial delay, 5 s interval, 10 min max | 1 s, then 1 s for 15 s, then 5 s; 10 min max | kept (deliberate `perf(auth)` choice); see open items |
| Auth client id in fork payload | `external-drive` for forks | `external-drive` | `cli-drive` branch removed |
| Session storage | one secret-store blob incl. key password | `session.json` + key password in git-credential / pass-cli | equivalent; kept |
| Refresh trigger | HTTP 401 only; never for `/auth/v4/refresh`, `/auth/v4/sessions`, `/core/v4/auth` | HTTP 401, Proton 10013, **and HTTP 403 / 9101** | 9101 removed from refresh triggers |
| Refresh failure | any 4xx except 429 signs the session out | file-locked re-read, then surface failure; tray stops retrying rejected refresh tokens | kept (multi-process safe) |
| Refresh request | `ResponseType/GrantType/RefreshToken` with `x-pm-uid` + bearer | same plus `UID`/`RedirectURI` (go-proton-api shape) | no change |
| User key unlock | import user keys with key password | same | no change |
| Address key unlock | decrypt token with user keys **and require a valid detached signature** | decrypted without verification, password fallback | verification added; fallback for token keys removed |
| Server time | `updateServerTime` from `x-pm-date`/`date` on every response, applied to all crypto | device clock | added (`src/utils/server-time.ts`, applied in the OpenPGP proxy) |
| SDK update notices | `x-pm-drive-requirements` headers evaluated, warned once | ignored | ported (`src/api/drive-requirements.ts`), warned once per process |
| SDK version header | real release | `js@0.0.1` placeholder | pinned release reported |
| Telemetry preference (`/core/v4/settings`) | fetched and stored | n/a (no telemetry sent) | none |
| SRP / key password derivation | `@protontech/crypto/srp` | hand-rolled `src/auth/srp/*` + `bcryptjs` | unchanged; see open items |
| OpenPGP proxy | `@protontech/crypto` `CryptoProxy` (with a patch replacing `openpgp/lightweight`) | hand-rolled `src/sdk/cryptoProxy.ts` on `@protontech/openpgp` | unchanged; see open items |

Why the account module was not adopted wholesale: its `ApiClient` keeps the
refresh token in memory and signs the session out on any failed non-429 4xx
refresh. The Git LFS bridge runs up to ten short-lived subprocesses
concurrently that share one session file; a stale in-memory refresh token in
one of them would wipe the session for all. The project's `SessionManager`
exists precisely to re-read the file under a lock before refreshing. Adoption
would require either a single long-lived helper process or a credentials
implementation that re-reads before every refresh; both are larger changes
than this update and are not needed to stay compatible with Proton.

## Changes Made

proton-drive-cli (`submodules/proton-drive-cli`):

- `submodules/sdk` -> `js/v0.23.0`.
- `jest.config.ts`, `tsconfig.jest.json`: transpile the ESM SDK dist.
- `.github/workflows/*`, `README.md`, `docs/operations/configuration.md`:
  `npm install --no-package-lock` for the SDK, Node 22 requirement.
- `package.json`: `engines.node >= 22`.
- `src/constants.ts`: grammar validation, `VENDORED_DRIVE_SDK_VERSION`.
- `src/auth/browser-fork.ts`: always `external-drive`.
- `src/sdk/httpClientAdapter.ts`: SDK version header, server time, Drive
  requirement notices, no refresh on 9101.
- `src/api/http-client.ts`: server time.
- `src/sdk/cryptoProxy.ts`: every OpenPGP call dated with server time.
- `src/crypto/index.ts`, `src/crypto/drive-crypto.ts`: verified address-key
  token decryption; decrypted address keys now carry their Proton key IDs and
  `src/sdk/accountAdapter.ts` reports those instead of looking IDs up by
  position in `Address.Keys` (a pre-existing bug that would attribute a key
  to another key's ID whenever an earlier key was skipped; stricter
  verification made it reachable).
- `yarn.lock` regenerated (the submodule's standalone CI installs with
  `--immutable`); `scripts/build-runtime.mjs` targets `node22`.
- `src/api/drive-requirements.ts`, `src/utils/server-time.ts` (new).
- Tests: `constants`, `httpClientAdapter`, `cryptoProxy`, `sdk-behavioral`
  (runtime-shape checks instead of `constructor.name`, which swc output broke),
  new `drive-crypto-address-keys`, `drive-requirements`, `server-time`.

Root:

- `Makefile`: `build-sdk`; `build-drive-cli` depends on the SDK dist file
  target, which builds the SDK once when missing.
- `internal/config/app_version_test.go` (new): the Go default identity must
  equal `external-drive-protonlfscli@<proton-drive-cli package.json version>`.
- `.github/workflows/{test,npm-publish,release-bundle}.yml`: SDK install step.
- `.env.example`, `.github/copilot-instructions.md`, `README.md`,
  `docs/operations/{adapter-configuration,deployment}.md`,
  `docs/project/current-state.md`.

## Verification

- Vendored SDK built from source (no search bindings; expected).
- proton-drive-cli: `tsc --noEmit` clean; esbuild bundle builds (4 MB, no
  wasm); `node dist/index.js bridge auth-state` offline works; Jest 52 suites.
- Submodule `yarn install --immutable` passes with the regenerated lockfile.
- An independent adversarial review of the diff was run; its findings were
  fixed (see the root `.changeset/PR_SUMMARY.md` for the list).
- Go: `go vet ./cmd/... ./internal/...` clean; `go test -race -cover` passes
  for `cmd/adapter`, `cmd/tray`, `internal/config` (including the new
  `app_version_test.go`) and `internal/preflight`; `gofmt -l` reports nothing.
- Not run: SEA build (needs Node 25), Go integration suites, any live Proton
  login or transfer. Run the guarded browser-fork canary before relying on
  the auth changes.

## Open Items

1. Fork polling cadence: official clients poll every 5 s; we poll every second
   for the first 15 s. Harmless for one login, but it is the only remaining
   anti-abuse divergence from the reference and is a one-line change in
   `src/auth/browser-fork.ts`.
2. Replace the hand-rolled SRP/key-password code (`src/auth/srp/*`,
   `bcryptjs`) and the hand-rolled OpenPGP proxy with `@protontech/crypto`
   (`srp`, `CryptoProxy`), which the official module uses. Blocked on
   validating Proton's `openpgp/lightweight` patch under Node/esbuild; tracked
   as a follow-up, not part of this update.
3. Crypto model migration (end 2026 / early 2027): keep the SDK pin fresh and
   watch for `Proton Drive requires SDK ... or newer` warnings, which this
   update now surfaces.
4. `.github/copilot-instructions.md` still describes legacy credential paths
   (environment variables, direct auth flow); only the app-version lines were
   corrected here.
5. `.env.example` `PROTON_API_BASE` is not read by any code.
6. proton-drive-cli `package.json` still declares `bundledDependencies:
   ["@protontech/drive-sdk"]` with a `prepack` script that deletes files
   under `node_modules/@protontech/drive-sdk/src`. That path is a Yarn portal
   symlink into the SDK git checkout, so a local `npm pack` deletes tracked
   SDK sources; the esbuild bundle already inlines the SDK, so both are
   likely obsolete. Not changed here (release tooling).
