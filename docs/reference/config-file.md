# Config File Reference

The tray stores its preferences in `~/.proton-lfs/config.json`. Source of
truth: `internal/config/prefs.go` (structure, defaults, load/save) and
`cmd/tray/*.go` (readers and writers).

## Location and permissions

| Aspect | Rule |
| --- | --- |
| Path | `$HOME/.proton-lfs/config.json` (no override) |
| Directory mode | `0700` |
| File mode | `0600` |
| Write | Atomic (temporary file, `fsync`, rename), indented JSON |
| Missing, unreadable or invalid file | Defaults are used; nothing is written until a preference changes |

## Fields

| Field | Type | Default | Meaning |
| --- | --- | --- | --- |
| `credentialStore` | string | `pass-cli` | Where `proton-drive-cli login` stores the session tokens and the browser-fork key password: `pass-cli` or `git-credential`. Passed as `--credential-store` to `login`. An empty or absent value reads as `pass-cli`. |

This field is the whole file. Any other key is ignored when the file
is read and dropped by the next save.

Example:

```json
{
  "credentialStore": "git-credential"
}
```

## Who reads and writes it

| Component | Access |
| --- | --- |
| Tray menu "Use Proton Pass" / "Use Git Credential Helper" | writes `credentialStore`; the check mark is on "Use Git Credential Helper" for `git-credential` and on "Use Proton Pass" for any other value |
| `proton-lfs-tray config [store]` | prints `credentialStore`, or validates and writes it |
| Tray "Connect to Proton..." and `proton-lfs-tray login` | read `credentialStore` for `proton-drive-cli login --credential-store` |
| `proton-lfs-tray register` | prints `credentialStore` as `store:`; the git registration does not contain it |
| `proton-lfs-tray status` | prints `credentialStore` as `Store: <store> (next login)`; the `Session:` line shows the store recorded in the session |
| `proton-lfs-tray scope-diagnostics` | reports it as `config.credentialStore` |
| `git-lfs-proton-adapter` | does not read this file; transfers use the credential store and host recorded in the saved session |

The credential store recorded in `~/.proton-drive-cli/session.json` by the
last `login` is the one later proton-drive-cli commands (bridge commands,
`doctor`, `logout`, `session refresh`) use. Changing `credentialStore` takes
effect with the next Connect.
