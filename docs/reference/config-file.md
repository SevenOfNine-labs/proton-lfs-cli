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
| `keyPasswordProvider` | string | `pass-cli` | Where `proton-drive-cli login` stores the browser-fork key password: `pass-cli` or `git-credential`. Passed as `--key-password-provider` to `login`. An empty or absent value reads as `pass-cli`. |

This field is the whole file. Any other key is ignored when the file
is read and dropped by the next save.

Example:

```json
{
  "keyPasswordProvider": "git-credential"
}
```

## Who reads and writes it

| Component | Access |
| --- | --- |
| Tray menu "Use Proton Pass" / "Use Git Credential Manager" | writes `keyPasswordProvider`; the check mark is on "Use Git Credential Manager" for `git-credential` and on "Use Proton Pass" for any other value |
| `proton-lfs-tray config [provider]` | prints `keyPasswordProvider`, or validates and writes it |
| Tray "Connect to Proton..." and `proton-lfs-tray login` | read `keyPasswordProvider` for `proton-drive-cli login --key-password-provider` |
| `proton-lfs-tray register` | prints `keyPasswordProvider` as `provider:`; the git registration does not contain it |
| `proton-lfs-tray status` | prints `keyPasswordProvider` as `Provider: <provider> (next login)`; the `Session:` line shows the provider recorded in the session |
| `proton-lfs-tray scope-diagnostics` | reports it as `config.keyPasswordProvider` |
| `git-lfs-proton-adapter` | does not read this file; transfers use the provider and host recorded in the saved session |

The provider recorded in `~/.proton-drive-cli/session.json` by the last
`login` is the one later proton-drive-cli commands (bridge commands,
`doctor`, `logout`) use. Changing `keyPasswordProvider` takes effect with the
next Connect.
