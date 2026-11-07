//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCredentialPassCLIReadiness checks that the configured pass-cli answers
// `pass-cli info`; it skips when pass-cli is absent or logged out.
func TestCredentialPassCLIReadiness(t *testing.T) {
	passCLIBin := sdkPassCLIPath()
	if _, err := exec.LookPath(passCLIBin); err != nil {
		t.Skipf("pass-cli not found: %v", err)
	}

	cmd := exec.Command(passCLIBin, "info")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("pass-cli not authenticated or unavailable: %v [%s]", err, strings.TrimSpace(string(out)))
	}
}

// TestCredentialSessionFilePermissions verifies the session file is only readable by the owner.
func TestCredentialSessionFilePermissions(t *testing.T) {
	sessionDir := os.Getenv("PROTON_DRIVE_CLI_SESSION_DIR")
	if sessionDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("cannot determine home directory: %v", err)
		}
		sessionDir = filepath.Join(homeDir, ".proton-drive-cli")
	}

	sessionFile := filepath.Join(sessionDir, "session.json")
	info, err := os.Stat(sessionFile)
	if err != nil {
		t.Skipf("session file not found at %s: %v (login first)", sessionFile, err)
	}

	mode := info.Mode().Perm()
	if mode&0o077 != 0 {
		t.Errorf("session file has overly permissive permissions: %04o (expected 0600)", mode)
	}
}

// adapterBinaryForCredentialTests returns the adapter under test: the
// binary from `make build` when present, otherwise a fresh build.
func adapterBinaryForCredentialTests(t *testing.T, root string) string {
	t.Helper()
	built := filepath.Join(root, "bin", "git-lfs-proton-adapter")
	if info, err := os.Stat(built); err == nil && !info.IsDir() {
		return built
	}
	return buildAdapter(t, root)
}

// bridgeInvocation is what the recording bridge writes for every spawn.
type bridgeInvocation struct {
	Command string            `json:"command"`
	Env     map[string]string `json:"env"`
	Request string            `json:"request"`
}

// writeRecordingBridge writes a proton-drive-cli stand-in that records its
// command and environment to recordDir and answers every command with ok.
// MOCK_BRIDGE_RECORD_DIR passes the adapter's MOCK_BRIDGE_ allowlist prefix.
func writeRecordingBridge(t *testing.T) (script, recordDir string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skipf("node not found: %v", err)
	}
	dir := t.TempDir()
	recordDir = filepath.Join(dir, "records")
	if err := os.MkdirAll(recordDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script = filepath.Join(dir, "recording-drive-cli.js")
	const body = `#!/usr/bin/env node
const fs = require('fs');
const path = require('path');
const command = process.argv[3] || '';
const request = fs.readFileSync(0, 'utf8');
const dir = process.env.MOCK_BRIDGE_RECORD_DIR;
if (dir) {
  const name = Date.now() + '-' + process.pid + '-' + command + '.json';
  fs.writeFileSync(path.join(dir, name), JSON.stringify({ command, env: process.env, request }));
}
const responses = {
  'auth-state': { ok: true, payload: { state: 'ready', hasSession: true, sessionValid: true, sessionExpired: false, sessionUidPresent: true, credentialsAvailable: true, willAttemptNetwork: false, errors: [], actions: [] } },
  init: { ok: true, payload: { initialized: true, storageBase: 'LFS' } },
  exists: { ok: true, payload: { exists: false } },
  upload: { ok: true, payload: { uploaded: true } },
  download: { ok: true, payload: { downloaded: true } },
};
const response = responses[command] || { ok: false, code: 400, error: 'unexpected bridge command ' + command };
process.stdout.write(JSON.stringify(response));
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return script, recordDir
}

// lastComplete returns the final complete message of a session, which
// follows the init response and any progress messages.
func lastComplete(t *testing.T, messages []adapterMessage, raw string) adapterMessage {
	t.Helper()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Event == "complete" {
			return messages[i]
		}
	}
	t.Fatalf("no complete message in %+v\n%s", messages, raw)
	return adapterMessage{}
}

func readBridgeInvocations(t *testing.T, recordDir string) []bridgeInvocation {
	t.Helper()
	entries, err := os.ReadDir(recordDir)
	if err != nil {
		t.Fatal(err)
	}
	var out []bridgeInvocation
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(recordDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var inv bridgeInvocation
		if err := json.Unmarshal(data, &inv); err != nil {
			t.Fatalf("record %s: %v", e.Name(), err)
		}
		out = append(out, inv)
	}
	return out
}

// adapterMessage is one line of the custom transfer protocol.
type adapterMessage struct {
	Event string `json:"event"`
	OID   string `json:"oid,omitempty"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// runAdapterSession drives the adapter through init, one upload and
// terminate, returning the parsed stdout lines and the raw combined output.
func runAdapterSession(t *testing.T, adapterPath, bridge string, env []string, uploadJSON string) ([]adapterMessage, string) {
	t.Helper()
	stdin := strings.Join([]string{
		`{"event":"init","operation":"upload","remote":"origin","concurrent":false,"concurrenttransfers":1}`,
		uploadJSON,
		`{"event":"terminate"}`,
	}, "\n") + "\n"

	cmd := exec.Command(adapterPath, "--backend=sdk", "--drive-cli-bin="+bridge)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	var messages []adapterMessage
	scanner := bufio.NewScanner(bytes.NewReader(stdout.Bytes()))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg adapterMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("adapter wrote a non-JSON line %q\nstderr:\n%s", line, stderr.String())
		}
		messages = append(messages, msg)
	}
	return messages, stdout.String() + stderr.String()
}

// credentialTestEnv is a minimal environment for the adapter plus secret-
// shaped variables that must never reach the bridge subprocess.
func credentialTestEnv(t *testing.T, recordDir string, secrets map[string]string) []string {
	t.Helper()
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"PROTON_LFS_STATUS_FILE=" + filepath.Join(t.TempDir(), "status.json"),
		"MOCK_BRIDGE_RECORD_DIR=" + recordDir,
	}
	for _, key := range []string{"NODE_BIN", "NVM_DIR", "TMPDIR"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	for k, v := range secrets {
		env = append(env, k+"="+v)
	}
	return env
}

func maliciousOIDs() map[string]string {
	return map[string]string{
		"path traversal":       "../../etc/passwd",
		"shell command":        "; rm -rf /",
		"command substitution": "$(whoami)",
		"backticks":            "`id`",
		"pipe":                 "| cat /etc/passwd",
		"short":                "abc123",
		"63 hex":               strings.Repeat("a", 63),
		"65 hex":               strings.Repeat("a", 65),
		"non-hex 64":           strings.Repeat("g", 64),
		"empty":                "",
	}
}

// TestCredentialAdapterRejectsMaliciousOIDBeforeSpawning drives the real
// adapter binary with invalid OIDs and asserts the protocol error envelope
// and that no bridge subprocess was spawned for the transfer.
func TestCredentialAdapterRejectsMaliciousOIDBeforeSpawning(t *testing.T) {
	root := repoRoot(t)
	adapterPath := adapterBinaryForCredentialTests(t, root)
	source := filepath.Join(t.TempDir(), "object.bin")
	if err := os.WriteFile(source, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, oid := range maliciousOIDs() {
		t.Run(name, func(t *testing.T) {
			bridge, recordDir := writeRecordingBridge(t)
			upload, _ := json.Marshal(map[string]any{"event": "upload", "oid": oid, "size": 7, "path": source})
			messages, raw := runAdapterSession(t, adapterPath, bridge, credentialTestEnv(t, recordDir, nil), string(upload))

			complete := lastComplete(t, messages, raw)
			if complete.Error == nil {
				t.Fatalf("expected complete envelope with error for oid %q, got %+v\n%s", oid, complete, raw)
			}
			if complete.Error.Code != 400 || complete.Error.Message != "invalid oid format" {
				t.Fatalf("expected 400 invalid oid format, got code=%d message=%q", complete.Error.Code, complete.Error.Message)
			}
			if complete.OID != oid {
				t.Fatalf("error envelope must echo the request oid; got %q want %q", complete.OID, oid)
			}
			for _, inv := range readBridgeInvocations(t, recordDir) {
				switch inv.Command {
				case "auth-state", "init":
					// Session initialization runs before any transfer.
				default:
					t.Fatalf("bridge %q must not be spawned for an invalid oid; request %s", inv.Command, inv.Request)
				}
				if strings.Contains(inv.Request, oid) && oid != "" {
					t.Fatalf("invalid oid %q reached bridge %q: %s", oid, inv.Command, inv.Request)
				}
			}
		})
	}
}

// TestCredentialAdapterNormalizesOIDCaseBeforeBridge: an OID is the lowercase
// hex SHA-256 of the content (Git LFS specification, git-lfs docs/spec.md);
// the bridge receives the lowercase form of any accepted spelling.
func TestCredentialAdapterNormalizesOIDCaseBeforeBridge(t *testing.T) {
	root := repoRoot(t)
	adapterPath := adapterBinaryForCredentialTests(t, root)
	source := filepath.Join(t.TempDir(), "object.bin")
	content := []byte("payload")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	lowerOID := hex.EncodeToString(sum[:])
	oid := strings.ToUpper(lowerOID)

	bridge, recordDir := writeRecordingBridge(t)
	upload, _ := json.Marshal(map[string]any{"event": "upload", "oid": oid, "size": len(content), "path": source})
	messages, raw := runAdapterSession(t, adapterPath, bridge, credentialTestEnv(t, recordDir, nil), string(upload))
	complete := lastComplete(t, messages, raw)
	if complete.Error != nil || complete.OID != lowerOID {
		t.Fatalf("expected a successful complete with the lowercase oid, got %+v\n%s", complete, raw)
	}
	var sawUpload bool
	for _, inv := range readBridgeInvocations(t, recordDir) {
		if inv.Command != "upload" {
			continue
		}
		sawUpload = true
		if strings.Contains(inv.Request, oid) || !strings.Contains(inv.Request, lowerOID) {
			t.Fatalf("bridge upload must receive the lowercase oid %s, got request %s", lowerOID, inv.Request)
		}
	}
	if !sawUpload {
		t.Fatalf("upload never reached the bridge\n%s", raw)
	}
}

// TestCredentialAdapterDoesNotForwardSecretsToBridge: secret-shaped
// environment variables reach neither the bridge environment, its request,
// nor the adapter's output.
func TestCredentialAdapterDoesNotForwardSecretsToBridge(t *testing.T) {
	root := repoRoot(t)
	adapterPath := adapterBinaryForCredentialTests(t, root)
	content := []byte("proton-lfs-cli credential security")
	source := filepath.Join(t.TempDir(), "object.bin")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	oid := hex.EncodeToString(sum[:])

	secrets := map[string]string{
		"PROTON_LFS_TEST_SECRET": "test-secret-LEAK-1",
		"PROTON_KEY_PASSWORD":    "key-password-LEAK-2",
		"PROTON_PASS_TOKEN":      "pass-token-LEAK-3",
		"NODE_AUTH_TOKEN":        "node-auth-token-LEAK-4",
		"GITHUB_TOKEN":           "github-token-LEAK-5",
		"AWS_SECRET_ACCESS_KEY":  "aws-LEAK-6",
	}
	bridge, recordDir := writeRecordingBridge(t)
	upload, _ := json.Marshal(map[string]any{"event": "upload", "oid": oid, "size": len(content), "path": source})
	messages, raw := runAdapterSession(t, adapterPath, bridge, credentialTestEnv(t, recordDir, secrets), string(upload))

	invocations := readBridgeInvocations(t, recordDir)
	var sawUpload bool
	for _, inv := range invocations {
		if inv.Command == "upload" {
			sawUpload = true
		}
		for key, value := range secrets {
			if got, present := inv.Env[key]; present {
				t.Errorf("bridge %q received %s=%q; secret-shaped variables must not be forwarded", inv.Command, key, got)
			}
			if strings.Contains(inv.Request, value) {
				t.Errorf("bridge %q request contains the value of %s", inv.Command, key)
			}
		}
	}
	if !sawUpload {
		t.Fatalf("the upload never reached the bridge, so the environment check did not run; messages=%+v\n%s", messages, raw)
	}
	for key, value := range secrets {
		if strings.Contains(raw, value) {
			t.Errorf("adapter output contains the value of %s", key)
		}
	}
	if complete := lastComplete(t, messages, raw); complete.Error != nil {
		t.Fatalf("expected a successful complete message, got %+v\n%s", complete, raw)
	}
	t.Logf("bridge invocations: %d (%s)", len(invocations), invocationCommands(invocations))
}

func invocationCommands(invocations []bridgeInvocation) string {
	names := make([]string, 0, len(invocations))
	for _, inv := range invocations {
		names = append(names, inv.Command)
	}
	return fmt.Sprintf("%v", names)
}
