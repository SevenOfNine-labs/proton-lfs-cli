# Copilot Instructions

The project instructions live in [CLAUDE.md](../CLAUDE.md) (architecture,
commands, configuration, testing strategy) and [AGENTS.md](../AGENTS.md)
(repository guidelines, changeset tracking). Read both before changing code
or documentation; the submodule has its own `AGENTS.md` and `CLAUDE.md` under
`submodules/proton-drive-cli/`.

Four rules apply to every change, commit, pull request, issue and comment:

1. No AI attribution anywhere: no `Co-Authored-By` or `Signed-off-by` naming
   an assistant, no "generated with" lines.
2. Every commit is signed and signed off by the author: `git commit -s -S`.
3. Conventional Commits for every commit message and pull request title:
   `<type>(<scope>): <subject>` with one of `feat`, `fix`, `docs`, `style`,
   `refactor`, `test`, `chore`, `ci`, `perf`, `build`; the commit-msg hook
   and the Commits workflow enforce it.
4. Comments cite standards, specifications, scientific publications or
   external documentation only; no history, legacy notes, excuses or TODOs,
   and no legacy code (see "Code Comments and Legacy" in CLAUDE.md and
   AGENTS.md; `scripts/check-comment-policy.sh` enforces the vocabulary).
