# Feature Audit Findings Fixed on 2026-06-22

> Historical record as of 2026-06-22; superseded by `docs/architecture/feature-audit.md`, which now carries only the current inventory. These findings were fixed in the audit pass of that date and are kept as the record of what changed.

- The dependency audit of `proton-drive-cli` found that
  `KEY_PASSWORD_REQUIRED` needed bridge status mapping to `401`. That fix lives
  in the submodule commit for this audit pass.
- The official SDK layout migration from `js/sdk` to `client/js` is now pinned
  through `proton-drive-cli@b97563b` and the root submodule pointer. No real
  Proton login or canary was run for this migration.
- SDK upload dedup now fails closed on non-404 `exists` errors and preserves
  retryable/temporary classification for transient outages.
- Adapter progress coverage now includes a virtual multi-GiB `copyWithProgress`
  test that exercises the real streaming loop without large fixtures.
