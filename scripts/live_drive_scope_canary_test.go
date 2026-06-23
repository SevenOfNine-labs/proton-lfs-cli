package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLiveDriveScopeCanaryRunsOneReadOnlyList(t *testing.T) {
	env := newLiveDriveScopeScriptEnv(t, `{"ok":true,"payload":{"files":[]}}`, 0)

	out, err := runLiveDriveScopeScript(t, env)
	if err != nil {
		t.Fatalf("live Drive scope canary failed: %v\n%s", err, out)
	}

	commands := readFakeDriveCommands(t, env.logPath)
	assertDriveCommandSequence(t, commands, []string{"bridge"})
	if !strings.Contains(commands[0], "bridge list") {
		t.Fatalf("expected bridge list command, got: %v", commands)
	}
	assertNoLiveScopeTransferCommands(t, commands)
	if got := readFakeDriveStdin(t, env.stdinPath); strings.TrimSpace(got) != `{"folder":"/"}` {
		t.Fatalf("bridge list stdin = %q, want root folder request", got)
	}
	if !strings.Contains(out, "read-only Drive metadata request") {
		t.Fatalf("expected read-only success confirmation, got:\n%s", out)
	}
}

func TestLiveDriveScopeCanaryStopsOnInsufficientScope(t *testing.T) {
	env := newLiveDriveScopeScriptEnv(t, `{"ok":false,"code":403,"error":"API Error (9101): Access token does not have sufficient scope","details":"{\"errorCode\":\"INSUFFICIENT_SCOPE\",\"protonCode\":9101}"}`, 0)

	out, err := runLiveDriveScopeScript(t, env)
	if err == nil {
		t.Fatalf("expected insufficient scope to fail:\n%s", out)
	}
	if !strings.Contains(out, "INSUFFICIENT_SCOPE") || !strings.Contains(out, "9101") {
		t.Fatalf("expected insufficient-scope hard stop, got:\n%s", out)
	}

	commands := readFakeDriveCommands(t, env.logPath)
	assertDriveCommandSequence(t, commands, []string{"bridge"})
	assertNoLiveScopeTransferCommands(t, commands)
}

func TestLiveDriveScopeCanaryCarriesDataCredentialArgs(t *testing.T) {
	env := newLiveDriveScopeScriptEnv(t, `{"ok":true,"payload":{"files":[]}}`, 0)
	env.vars = replaceEnv(env.vars, "LIVE_CANARY_DOCTOR_ARGS", "--key-password-provider pass-cli --data-credential-provider pass-cli --data-credential-host proton-data.test --require-data-password")

	out, err := runLiveDriveScopeScript(t, env)
	if err != nil {
		t.Fatalf("live Drive scope canary failed: %v\n%s", err, out)
	}

	got := readFakeDriveStdin(t, env.stdinPath)
	for _, want := range []string{
		`"folder":"/"`,
		`"dataCredentialProvider":"pass-cli"`,
		`"dataCredentialHost":"proton-data.test"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("scope request missing %s:\n%s", want, got)
		}
	}
}

func TestLiveDriveScopeCanaryRejectsMissingAckBeforeDriveCall(t *testing.T) {
	env := newLiveDriveScopeScriptEnv(t, `{"ok":true,"payload":{"files":[]}}`, 0)
	env.vars = replaceEnv(env.vars, "PROTON_LFS_LIVE_CANARY", "")

	out, err := runLiveDriveScopeScript(t, env)
	if err == nil {
		t.Fatalf("expected missing acknowledgement to fail:\n%s", out)
	}
	if !strings.Contains(out, "exact PROTON_LFS_LIVE_CANARY") {
		t.Fatalf("expected acknowledgement error, got:\n%s", out)
	}
	if commands := readFakeDriveCommands(t, env.logPath); len(commands) != 0 {
		t.Fatalf("drive CLI was called before acknowledgement passed: %v", commands)
	}
}

func TestLiveDriveScopeCanaryRejectsMissingDoctorArgsBeforeDriveCall(t *testing.T) {
	env := newLiveDriveScopeScriptEnv(t, `{"ok":true,"payload":{"files":[]}}`, 0)
	env.vars = replaceEnv(env.vars, "LIVE_CANARY_DOCTOR_ARGS", "")

	out, err := runLiveDriveScopeScript(t, env)
	if err == nil {
		t.Fatalf("expected missing doctor args to fail:\n%s", out)
	}
	if !strings.Contains(out, "LIVE_CANARY_DOCTOR_ARGS") {
		t.Fatalf("expected doctor args error, got:\n%s", out)
	}
	if commands := readFakeDriveCommands(t, env.logPath); len(commands) != 0 {
		t.Fatalf("drive CLI was called before doctor args passed: %v", commands)
	}
}

type liveDriveScopeScriptEnv struct {
	vars      []string
	logPath   string
	stdinPath string
}

func newLiveDriveScopeScriptEnv(t *testing.T, bridgeJSON string, exitCode int) liveDriveScopeScriptEnv {
	t.Helper()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "drive-commands.log")
	stdinPath := filepath.Join(tempDir, "drive-stdin.log")
	responsePath := filepath.Join(tempDir, "bridge-response.json")
	if err := os.WriteFile(responsePath, []byte(bridgeJSON), 0o600); err != nil {
		t.Fatalf("write fake bridge response: %v", err)
	}
	fakeNodePath := filepath.Join(tempDir, "fake-node.sh")
	if err := os.WriteFile(fakeNodePath, []byte(fakeLiveDriveScopeNodeScript), 0o700); err != nil {
		t.Fatalf("write fake node: %v", err)
	}

	return liveDriveScopeScriptEnv{
		logPath:   logPath,
		stdinPath: stdinPath,
		vars: []string{
			"PROTON_LFS_LIVE_CANARY=" + browserForkCanaryAck,
			"LIVE_CANARY_DOCTOR_ARGS=--key-password-provider pass-cli",
			"NODE_BIN=" + fakeNodePath,
			"DRIVE_CLI_BIN=" + filepath.Join(tempDir, "fake-drive-cli.js"),
			"LIVE_SCOPE_FAKE_LOG=" + logPath,
			"LIVE_SCOPE_FAKE_STDIN=" + stdinPath,
			"LIVE_SCOPE_FAKE_RESPONSE=" + responsePath,
			"LIVE_SCOPE_FAKE_EXIT_CODE=" + strconv.Itoa(exitCode),
		},
	}
}

const fakeLiveDriveScopeNodeScript = `#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "-e" || "${1:-}" == "-" || "${1:-}" == "" ]]; then
  exec node "$@"
fi

printf '%s\n' "$*" >> "${LIVE_SCOPE_FAKE_LOG}"
cmd="${2:-}"
subcmd="${3:-}"

case "${cmd}:${subcmd}" in
  bridge:list)
    cat > "${LIVE_SCOPE_FAKE_STDIN}"
    cat "${LIVE_SCOPE_FAKE_RESPONSE}"
    exit "${LIVE_SCOPE_FAKE_EXIT_CODE}"
    ;;
  *)
    echo "unexpected command: ${cmd} ${subcmd}" >&2
    exit 10
    ;;
esac
`

func runLiveDriveScopeScript(t *testing.T, env liveDriveScopeScriptEnv) (string, error) {
	t.Helper()

	root := repoRoot(t)
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "live-drive-scope-canary.sh"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env.vars...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func readFakeDriveStdin(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fake drive stdin: %v", err)
	}
	return string(body)
}

func assertNoLiveScopeTransferCommands(t *testing.T, commands []string) {
	t.Helper()

	for _, command := range commands {
		fields := strings.Fields(command)
		if len(fields) < 2 {
			t.Fatalf("command malformed: %q", command)
		}
		for _, forbidden := range []string{"login", "status", "doctor", "upload", "download", "init", "delete", "auth", "refresh"} {
			if fields[1] == forbidden {
				t.Fatalf("unexpected command %q in %v", forbidden, commands)
			}
		}
		if len(fields) >= 3 && fields[1] == "bridge" {
			for _, forbidden := range []string{"upload", "download", "init", "delete", "refresh"} {
				if fields[2] == forbidden {
					t.Fatalf("unexpected bridge command %q in %v", forbidden, commands)
				}
			}
		}
	}
}
