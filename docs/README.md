# Documentation

Project-owned docs live here. Upstream/vendor docs remain under `submodules/` and are referenced, not duplicated.

## Start Here

1. [USAGE.md](../USAGE.md): install, configure a repository, tray app, troubleshooting.
2. [Current State](project/current-state.md): what is implemented and what is open.
3. [Architecture](architecture/architecture.md): components, flows, local state, build and release.
4. [Feature Audit](architecture/feature-audit.md): surface-by-surface maturity and test coverage.
5. [Live Canary Runbook](operations/live-canary-runbook.md): the only path that may touch a real Proton account.

## Pages

### Project

- [Current State](project/current-state.md)

### Architecture

- [Overview](architecture/overview.md): system boundary and non-negotiables.
- [Architecture](architecture/architecture.md): diagrams of the three programs and their state.
- [Proton SDK Bridge](architecture/proton-sdk-bridge.md): the `sdk` backend contract.
- [Subprocess Integration](architecture/subprocess-integration.md): the Go `BridgeClient`.
- [Feature Audit](architecture/feature-audit.md)

### Operations

- [Adapter Configuration](operations/adapter-configuration.md): environment, key-password provider, offline doctor, session refresh.
- [Deployment](operations/deployment.md): local bring-up, release bundles, git wiring.
- [Releasing](operations/releasing.md): the `v*` tag pipeline.
- [Live Canary Runbook](operations/live-canary-runbook.md)
- [Tray Platform Release Checklist](operations/tray-platform-release-checklist.md)

### Reference

- [Adapter](reference/adapter.md): flags, environment, backends, process behaviour.
- [Adapter Errors](reference/adapter-errors.md): exit status, protocol and per-object errors, classification.
- [Status File](reference/status-file.md): `~/.proton-lfs/status.json`.
- [Config File](reference/config-file.md): `~/.proton-lfs/config.json`.
- [Tray](reference/tray.md): subcommands, menu, readiness rules, refresh, files.
- proton-drive-cli bridge protocol and doctor: `submodules/proton-drive-cli/docs/reference/bridge-protocol.md`, `docs/reference/doctor.md` and the JSON schemas under `schemas/`, published on the [proton-drive-cli documentation site](https://sevenofnine-labs.github.io/proton-drive-cli/).

### Testing

- [Integration Testing](testing/integration-testing.md)
- [Requirement mapping](testing/spec-requirements.yaml): machine-readable map from specification clauses to tests.

### Security

- [Threat Model](security/threat-model.md)
- [Security Policy](../SECURITY.md)

## Canonical Rule

When docs disagree, runtime behavior and tests win. Update docs in the same change.
