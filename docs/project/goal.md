# Goal

Build a trustworthy Git LFS backend on top of Proton Drive.

## Problem Statement

Git repositories need a secure place to store large binary objects. The backend must preserve normal Git LFS behavior while enforcing integrity and confidentiality.

## Success Criteria

1. `git lfs push` persists encrypted object data to Proton-backed storage.
2. `git lfs pull` restores exact object bytes for the pointer `oid` and `size`.
3. Transfer failures are explicit and never reported as success.
4. Authentication and token management are secure and auditable.
5. Operational visibility exists for latency, failures, and capacity.

## Constraints

- Must remain compatible with Git LFS custom transfer protocol.
- Must honor Proton SDK integration requirements and official endpoints.
- Must avoid plaintext credential storage in repository or build artifacts.
- Must be testable without relying on live Proton infrastructure for unit tests.

## Non-Goals (Current Scope)

- Building a general-purpose alternative Git hosting platform.
- Supporting production multi-tenant workloads before readiness gates are met.
- Treating mock transfer paths as production-capable behavior.
