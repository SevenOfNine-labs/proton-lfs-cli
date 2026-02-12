# Subprocess Integration Pattern

## Why Subprocess vs Library

The bridge uses `proton-drive-cli` as a subprocess rather than importing it as a Node.js library for several reasons:

1. **Process isolation**: A crash in proton-drive-cli doesn't take down the SDK service.
2. **Resource control**: Subprocess pool limits and timeouts prevent resource exhaustion.
3. **Consistent pattern**: Matches the existing pass-cli subprocess pattern in the Go adapter.
4. **Language boundary**: proton-drive-cli uses ESM and TypeScript; the SDK service uses CommonJS. Subprocess avoids module system conflicts.
5. **Independent lifecycle**: proton-drive-cli can be updated, rebuilt, and tested independently.

## Subprocess Lifecycle

```
SDK Service                          proton-drive-cli
    |                                      |
    |-- spawn(node, [cli, bridge, cmd]) -->|
    |-- stdin.write(JSON payload) -------->|
    |-- stdin.end() ---------------------->|
    |                                      |-- parse JSON from stdin
    |                                      |-- execute operation
    |                                      |-- write JSON to stdout
    |<-- stdout.on('data') ---------------|
    |<-- close event (exit code) ---------|
    |                                      |
    |-- parse JSON response                |
    |-- resolve/reject promise             |
```

## Error Handling

1. **Structured errors**: Bridge writes `{ ok: false, error: "...", code: N }` to stdout — parsed by SDK service.
2. **Non-JSON failures**: If stdout contains no valid JSON, stderr is included in the error details.
3. **Process crash**: Exit code != 0 with no JSON output generates a 500 BridgeError.
4. **Timeout**: After `PROTON_DRIVE_CLI_TIMEOUT_MS` (default 5 min), process is killed with SIGKILL and a 504 error is returned.

## Performance Considerations

- **Cold start**: First operation requires Node.js startup + authentication (~2-5s).
- **Session reuse**: Subsequent operations reuse saved session (~1-2s per operation).
- **Concurrency**: Up to 10 simultaneous subprocess operations.
- **Overhead**: ~50-100ms per subprocess spawn vs direct library call. Acceptable for Git LFS operations which are I/O-bound.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `PROTON_DRIVE_CLI_BIN` | `submodules/proton-drive-cli/dist/index.js` | Path to CLI entry point |
| `PROTON_DRIVE_CLI_TIMEOUT_MS` | `300000` | Per-operation timeout |
| `PROTON_DRIVE_CLI_SESSION_DIR` | `~/.proton-drive-cli` | Session persistence directory |
