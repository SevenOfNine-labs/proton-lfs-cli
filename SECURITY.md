# Security Policy

## Project Maturity

This repository is pre-production. Do not use it to store production or sensitive data until the production readiness gates in `docs/project/project-plan.md` are complete.

## Reporting A Vulnerability

- Do not open public issues for sensitive findings.
- Contact maintainers privately with:
  - impact summary
  - affected component/path
  - reproducible steps
  - mitigation suggestion (if available)

## Security Requirements For Contributors

- Never commit real credentials, tokens, or secrets.
- Keep mock and test-only behavior fail-closed by default.
- Add or update tests for all security-impacting changes.
- Prefer explicit error handling over silent fallback behavior.
