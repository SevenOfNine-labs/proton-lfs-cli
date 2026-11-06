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
| `keyPasswordProvider` | string | `pass-cli` | Where `proton-drive-cli login` stores the browser-fork key password: `pass-cli` or `git-credential`. Passed as `--key-password-provider` to login and doctor. |
| `dataCredentialProvider` | string | absent | Provider of the mailbox/data password of a two-password account (`pass-cli` or `git-credential`). When set, `register` adds `--data-credential-provider` and `--data-credential-host` to the adapter arguments. |
| `dataCredentialHost` | string | absent | Host/key of that password. When a provider is set and the host is empty, `proton-data.proton-lfs-cli.local` is used. |
| `enabled` | bool | `true` | Written with every save; no component reads it at present. |

Example:

```json
{
  "keyPasswordProvider": "git-credential",
  "enabled": true
}
```

## Former key

Files written before November 2026 stored the key-password provider under
`credentialProvider`. `LoadPrefs` reads that key when `keyPasswordProvider` is
absent; the next save writes `keyPasswordProvider` and drops the former key.
No code reads `credentialProvider` for any other purpose, and the adapter
never receives it.

## Who reads and writes it

| Component | Access |
| --- | --- |
| Tray menu "Use Proton Pass" / "Use Git Credential Manager" | writes `keyPasswordProvider` |
| `proton-lfs-tray config [provider]` | prints or writes `keyPasswordProvider` |
| Tray "Connect to Proton..." and `proton-lfs-tray login` | read `keyPasswordProvider` |
| Tray "Enable LFS Backend" and `proton-lfs-tray register` | read the data-credential fields to build `lfs.customtransfer.proton.args` |
| Tray "Run Doctor..." | reads all provider fields for the doctor arguments |
| `proton-lfs-tray status` | prints `keyPasswordProvider` as `Provider:` |
| `proton-lfs-tray scope-diagnostics` | reports the providers (environment first, then this file, then the git arguments) |
| `git-lfs-proton-adapter` | does not read this file; its settings arrive as flags written by `register` |

The data-credential fields have no tray UI; set them by editing the file, or
configure the adapter directly with `--data-credential-provider` in
`lfs.customtransfer.proton.args` or `PROTON_DATA_CREDENTIAL_PROVIDER`.
