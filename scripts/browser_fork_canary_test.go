package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const browserForkCanaryAck = "I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT"

func TestBrowserForkCanaryRunsExactlyOneLoginAndNoTransfer(t *testing.T) {
	env := newBrowserForkScriptEnv(t, `{
		"ok": true,
		"canAttemptTransfer": true,
		"canAttemptLiveCanary": true,
		"authState": {"state": "ready", "authMode": "browser-fork"}
	}`)
	out, err := runBrowserForkScript(t, env)
	if err != nil {
		t.Fatalf("browser-fork canary failed: %v\n%s", err, out)
	}

	commands := readFakeDriveCommands(t, env.logPath)
	assertDriveCommandSequence(t, commands, []string{"login", "status", "doctor"})
	if got := countDriveCommands(commands, "login"); got != 1 {
		t.Fatalf("login command count = %d, want 1\ncommands=%v", got, commands)
	}
	assertNoTransferCommands(t, commands)
	if !strings.Contains(out, "No transfer was attempted") {
		t.Fatalf("expected no-transfer confirmation, got:\n%s", out)
	}
}

func TestBrowserForkCanaryRejectsMissingAckBeforeLogin(t *testing.T) {
	env := newBrowserForkScriptEnv(t, `{
		"ok": true,
		"canAttemptTransfer": true,
		"canAttemptLiveCanary": true,
		"authState": {"state": "ready", "authMode": "browser-fork"}
	}`)
	env.vars = replaceEnv(env.vars, "PROTON_LFS_LIVE_CANARY", "")

	out, err := runBrowserForkScript(t, env)
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

func TestBrowserForkCanaryRejectsAuthModeOverrideBeforeLogin(t *testing.T) {
	env := newBrowserForkScriptEnv(t, `{
		"ok": true,
		"canAttemptTransfer": true,
		"canAttemptLiveCanary": true,
		"authState": {"state": "ready", "authMode": "browser-fork"}
	}`)
	env.vars = replaceEnv(env.vars, "LIVE_BROWSER_FORK_LOGIN_ARGS", "--auth-mode srp --key-password-provider git-credential")

	out, err := runBrowserForkScript(t, env)
	if err == nil {
		t.Fatalf("expected auth-mode override to fail:\n%s", out)
	}
	if !strings.Contains(out, "must not set --auth-mode") {
		t.Fatalf("expected auth-mode override error, got:\n%s", out)
	}
	if commands := readFakeDriveCommands(t, env.logPath); len(commands) != 0 {
		t.Fatalf("drive CLI was called before login args passed: %v", commands)
	}
}

func TestBrowserForkCanaryRejectsPostLoginDoctorMismatch(t *testing.T) {
	env := newBrowserForkScriptEnv(t, `{
		"ok": true,
		"canAttemptTransfer": true,
		"canAttemptLiveCanary": true,
		"authState": {"state": "ready", "authMode": "srp"}
	}`)

	out, err := runBrowserForkScript(t, env)
	if err == nil {
		t.Fatalf("expected post-login doctor mismatch to fail:\n%s", out)
	}
	if !strings.Contains(out, "auth mode mismatch") {
		t.Fatalf("expected structured doctor mismatch, got:\n%s", out)
	}
	commands := readFakeDriveCommands(t, env.logPath)
	assertDriveCommandSequence(t, commands, []string{"login", "status", "doctor"})
	assertNoTransferCommands(t, commands)
}

type browserForkScriptEnv struct {
	vars    []string
	logPath string
}

func newBrowserForkScriptEnv(t *testing.T, doctorJSON string) browserForkScriptEnv {
	t.Helper()

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "drive-commands.log")
	doctorPath := filepath.Join(tempDir, "doctor.json")
	if err := os.WriteFile(doctorPath, []byte(doctorJSON), 0o600); err != nil {
		t.Fatalf("write fake doctor JSON: %v", err)
	}
	fakeNodePath := filepath.Join(tempDir, "fake-node.sh")
	if err := os.WriteFile(fakeNodePath, []byte(fakeNodeScript), 0o700); err != nil {
		t.Fatalf("write fake node: %v", err)
	}

	return browserForkScriptEnv{
		logPath: logPath,
		vars: []string{
			"PROTON_LFS_LIVE_CANARY=" + browserForkCanaryAck,
			"LIVE_CANARY_DOCTOR_ARGS=--credential-provider pass-cli",
			"LIVE_BROWSER_FORK_LOGIN_ARGS=--key-password-provider git-credential",
			"NODE_BIN=" + fakeNodePath,
			"DRIVE_CLI_BIN=" + filepath.Join(tempDir, "fake-drive-cli.js"),
			"BROWSER_FORK_FAKE_LOG=" + logPath,
			"BROWSER_FORK_FAKE_DOCTOR_JSON=" + doctorPath,
		},
	}
}

const fakeNodeScript = `#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >> "${BROWSER_FORK_FAKE_LOG}"
cmd="${2:-}"

case "${cmd}" in
  login)
    if [[ "${3:-}" != "--auth-mode" || "${4:-}" != "browser-fork" ]]; then
      echo "bad login auth mode" >&2
      exit 7
    fi
    echo "login ok"
    ;;
  status)
    echo "status ok"
    ;;
  doctor)
    if [[ "${3:-}" != "--json" ]]; then
      echo "doctor missing --json" >&2
      exit 8
    fi
    cat "${BROWSER_FORK_FAKE_DOCTOR_JSON}"
    ;;
  bridge | upload | download | init | auth)
    echo "unexpected transfer/auth command: ${cmd}" >&2
    exit 9
    ;;
  *)
    echo "unexpected command: ${cmd}" >&2
    exit 10
    ;;
esac
`

func runBrowserForkScript(t *testing.T, env browserForkScriptEnv) (string, error) {
	t.Helper()

	root := repoRoot(t)
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "browser-fork-canary.sh"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env.vars...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(file))
}

func readFakeDriveCommands(t *testing.T, logPath string) []string {
	t.Helper()

	body, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read fake drive command log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(body)), "\n")
}

func assertDriveCommandSequence(t *testing.T, commands []string, want []string) {
	t.Helper()

	if len(commands) != len(want) {
		t.Fatalf("command count = %d, want %d\ncommands=%v", len(commands), len(want), commands)
	}
	for i, command := range commands {
		fields := strings.Fields(command)
		if len(fields) < 2 {
			t.Fatalf("command %d malformed: %q", i, command)
		}
		if fields[1] != want[i] {
			t.Fatalf("command %d = %q, want %q\ncommands=%v", i, fields[1], want[i], commands)
		}
	}
}

func countDriveCommands(commands []string, name string) int {
	count := 0
	for _, command := range commands {
		fields := strings.Fields(command)
		if len(fields) >= 2 && fields[1] == name {
			count++
		}
	}
	return count
}

func assertNoTransferCommands(t *testing.T, commands []string) {
	t.Helper()

	for _, command := range commands {
		for _, forbidden := range []string{"bridge", "upload", "download", "init", "auth"} {
			if countDriveCommands([]string{command}, forbidden) > 0 {
				t.Fatalf("unexpected transfer/auth command %q in %v", forbidden, commands)
			}
		}
	}
}

func replaceEnv(env []string, key, value string) []string {
	out := append([]string(nil), env...)
	prefix := key + "="
	for i, item := range out {
		if strings.HasPrefix(item, prefix) {
			out[i] = prefix + value
			return out
		}
	}
	return append(out, prefix+value)
}
