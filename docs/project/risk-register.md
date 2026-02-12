# Risk Register

Updated: 2026-02-09

| ID | Severity | Risk | Impact | Mitigation | Status |
| --- | --- | --- | --- | --- | --- |
| R-001 | Critical | Mock upload/download path could be mistaken for real persistence | Silent data loss | Adapter now fails closed unless mock mode is explicitly enabled | Mitigated (monitor) |
| R-002 | High | Real Proton transfer path is experimental and not yet production-hardened | Core goal is only partially met | In-repo real mode now exists via proton-drive-cli TypeScript bridge; next hardening: persistent session lifecycle, stronger secret handling, and soak validation | Open |
| R-003 | High | Auth/session model is prototype-only | Account compromise or invalid session behavior | Design secure session/token lifecycle with explicit revocation and expiry handling | Open |
| R-004 | High | SDK service tests do not validate real server behavior deeply | Regressions can ship undetected | Go integration suite now executes real SDK bridge API contract (`/init`, `/upload`, `/download`, `/refresh`, `/list`) and `git-lfs` roundtrip against local or external service | Mitigated (monitor) |
| R-005 | Medium | CI historically referenced missing files/paths | Broken PR signal, delayed delivery | CI workflows rewritten to test actual repo paths only | Mitigated |
| R-006 | Medium | Submodule SSH URLs can fail in CI/non-interactive environments | Build failures | Submodule URLs migrated to HTTPS | Mitigated |
| R-007 | Medium | No production observability baseline yet | Slow incident detection and triage | Add transfer metrics, structured logs, and alerting thresholds in Phase 4 | Open |
| R-008 | Medium | Lack of explicit threat model and security review | Unknown security gaps before launch | Add threat model + security gate in Phase 5 | Open |
