# Security Policy

## Project Maturity

This repository is pre-production. Do not use it to store production or
sensitive data; `docs/project/current-state.md` records what is implemented
and what is open, and `docs/security/threat-model.md` the trust boundaries.

## Reporting A Vulnerability

- Do not open public issues for sensitive findings.
- Report privately through
  [GitHub private vulnerability reporting](https://github.com/SevenOfNine-labs/proton-lfs-cli/security/advisories/new)
  with:
  - impact summary
  - affected component/path
  - reproducible steps
  - mitigation suggestion (if available)

Fixes go into the latest release only.

## Security Requirements For Contributors

- Never commit real credentials, tokens, or secrets.
- Keep mock and test-only behavior fail-closed by default.
- Add or update tests for all security-impacting changes.
- Prefer explicit error handling over silent fallback behavior.
