# Custom Transfer Protocol Contract

Baseline source: `submodules/git-lfs/docs/custom-transfers.md`.

## Framing

- Transport: stdin/stdout.
- Format: line-delimited JSON.
- One request per line, one response event per line.

## Required Events

- `init`: adapter setup for upload or download.
- `upload`: send object from `path`.
- `download`: materialize object and return local `path`.
- `terminate`: shutdown; no response required.

## Response Rules

- `init` success response is `{}`.
- Transfer completion response must include request `oid`.
- Transfer errors are object-scoped:
  - return `{"event":"complete","oid":"...","error":{...}}`
  - do not terminate process for per-object errors.

## Progress Rules

- Progress is optional but if emitted must be monotonic for `bytesSoFar`.
- Final completion state must be emitted exactly once per transfer request.

## Standalone Mode

With `lfs.standalonetransferagent=proton`, Git LFS can send `action: null`.
Adapter must resolve storage location from object identity and local configuration.
