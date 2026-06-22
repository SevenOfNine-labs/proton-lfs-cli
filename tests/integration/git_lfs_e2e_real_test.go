//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proton-lfs-cli/internal/preflight"
)

const liveCanaryAckValue = "I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT"

// requireRealE2EPrereqs skips the test unless the environment is configured for
// real Proton Drive E2E: pass-cli resolves real credentials and proton-drive-cli is built.
func requireRealE2EPrereqs(t *testing.T) (root, storageBase string) {
	t.Helper()

	root = repoRoot(t)

	if strings.TrimSpace(os.Getenv("PROTON_LFS_LIVE_CANARY")) != liveCanaryAckValue {
		t.Skip("real E2E test skipped: set the exact PROTON_LFS_LIVE_CANARY acknowledgement")
	}
	doctorArgs := strings.TrimSpace(os.Getenv("LIVE_CANARY_DOCTOR_ARGS"))
	if doctorArgs == "" {
		t.Skip("real E2E test skipped: LIVE_CANARY_DOCTOR_ARGS is required")
	}

	// Verify proton-drive-cli is built.
	driveCliBin := strings.TrimSpace(os.Getenv("PROTON_DRIVE_CLI_BIN"))
	if driveCliBin == "" {
		driveCliBin = filepath.Join(root, "submodules", "proton-drive-cli", "dist", "index.js")
	}
	if _, err := os.Stat(driveCliBin); err != nil {
		t.Skipf("real E2E test skipped: proton-drive-cli not built at %s (run: make build-drive-cli)", driveCliBin)
	}

	requireLiveCanaryDoctor(t, driveCliBin, doctorArgs)

	// Verify pass-cli can resolve real credentials (will skip if not logged in).
	sdkResolvedCredentials(t)

	storageBase = strings.Trim(strings.TrimSpace(os.Getenv("PROTON_LFS_CANARY_STORAGE_BASE")), "/")
	if storageBase == "" {
		storageBase = fmt.Sprintf(
			"LFS/canary/proton-lfs-cli/%s-%d",
			time.Now().UTC().Format("20060102T150405Z"),
			os.Getpid(),
		)
	}

	return root, storageBase
}

func requireLiveCanaryDoctor(t *testing.T, driveCliBin, doctorArgs string) {
	t.Helper()

	nodeBin := strings.TrimSpace(os.Getenv("NODE_BIN"))
	if nodeBin == "" {
		nodeBin = "node"
	}

	// LIVE_CANARY_DOCTOR_ARGS is operator-supplied shell-style text in the
	// Makefile path. The supported canary flags do not contain spaces, so
	// Fields keeps the direct Go-test path simple and injection-free.
	args := append([]string{driveCliBin, "doctor", "--json"}, strings.Fields(doctorArgs)...)
	cmd := exec.Command(nodeBin, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("real E2E test skipped: offline doctor failed: %v [%s]", err, redactBridgeOutput(string(out)))
	}

	states, err := preflight.LoadBridgeAuthStates(filepath.Join(
		repoRoot(t),
		"submodules",
		"proton-drive-cli",
		"schemas",
		"bridge",
		"v1",
		"auth-state-payload.schema.json",
	))
	if err != nil {
		t.Skipf("real E2E test skipped: offline doctor schema unavailable: %v", err)
	}
	readiness, err := preflight.ValidateDoctorReadiness(
		out,
		states,
		preflight.DoctorReadinessRequirements{RequireLiveCanary: true},
	)
	if err != nil {
		t.Skipf("real E2E test skipped: %v [%s]", err, redactBridgeOutput(string(out)))
	}
	t.Logf("offline doctor passed: authState=%s", readiness.AuthState.State)
}

// TestE2ERealProtonDrivePipeline exercises the full Git LFS pipeline through
// Proton Drive: commit one tiny canary object, push via the adapter, clone into
// a fresh repo, pull, verify byte-for-byte fidelity, and attempt cleanup.
//
// Prerequisites:
//   - PROTON_LFS_LIVE_CANARY is set to the exact acknowledgement
//   - LIVE_CANARY_DOCTOR_ARGS is set and make live-canary-preflight passes
//   - pass-cli logged in with disposable Proton credentials
//   - proton-drive-cli built (make build-drive-cli)
func TestE2ERealProtonDrivePipeline(t *testing.T) {
	root, storageBase := requireRealE2EPrereqs(t)

	originalBytes := []byte(fmt.Sprintf(
		"proton-lfs-cli-live-canary\ncreated=%s\nnonce=%d\n",
		time.Now().UTC().Format(time.RFC3339),
		time.Now().UnixNano(),
	))

	// Build adapter.
	adapterPath := buildAdapter(t, root)

	gitBin, err := findToolBinary(root, "GIT_BIN", "git")
	if err != nil {
		t.Skipf("real E2E test skipped: %v", err)
	}
	gitLFSBin, err := findToolBinary(root, "GIT_LFS_BIN", "git-lfs")
	if err != nil {
		t.Skipf("real E2E test skipped: %v", err)
	}

	env := envWithPath(filepath.Dir(gitLFSBin))

	// Resolve proton-drive-cli binary path.
	driveCliBin := sdkDriveCliBin(t, root)

	// Build credential env.
	sdkEnv := append(sdkCredentialEnv(t, env), "LFS_STORAGE_BASE="+storageBase)
	t.Logf("real canary storage base: %s", storageBase)

	// Set up source repository.
	base := t.TempDir()
	remotePath := filepath.Join(base, "remote.git")
	repoPath := filepath.Join(base, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	mustRun(t, base, sdkEnv, gitBin, "init", "--bare", remotePath)
	mustRun(t, remotePath, sdkEnv, gitBin, "symbolic-ref", "HEAD", "refs/heads/main")
	mustRun(t, repoPath, sdkEnv, gitBin, "init")
	mustRun(t, repoPath, sdkEnv, gitBin, "checkout", "-b", "main")
	mustRun(t, repoPath, sdkEnv, gitBin, "config", "user.name", "E2E Real Test")
	mustRun(t, repoPath, sdkEnv, gitBin, "config", "user.email", "e2e-real@example.com")
	mustRun(t, repoPath, sdkEnv, gitBin, "config", "commit.gpgsign", "false")
	mustRun(t, repoPath, sdkEnv, gitBin, "remote", "add", "origin", remotePath)

	mustRun(t, repoPath, sdkEnv, gitLFSBin, "install", "--local")
	configureSDKCustomTransfer(t, repoPath, sdkEnv, gitBin, adapterPath, driveCliBin)

	// Track only the one tiny canary object with LFS.
	mustRun(t, repoPath, sdkEnv, gitLFSBin, "track", "*.canary")

	canaryDest := filepath.Join(repoPath, "proton-lfs.canary")
	if err := os.WriteFile(canaryDest, originalBytes, 0o600); err != nil {
		t.Fatalf("failed to write canary object to repo: %v", err)
	}

	mustRun(t, repoPath, sdkEnv, gitBin, "add", ".gitattributes", "proton-lfs.canary")
	mustRun(t, repoPath, sdkEnv, gitBin, "commit", "-m", "add tiny live canary via LFS")

	// Verify LFS is tracking the file.
	lsFilesOutput := mustRun(t, repoPath, sdkEnv, gitLFSBin, "ls-files", "-l")
	fields := strings.Fields(strings.TrimSpace(lsFilesOutput))
	if len(fields) == 0 {
		t.Fatalf("expected oid in git lfs ls-files output, got:\n%s", lsFilesOutput)
	}
	oid := fields[0]
	if len(oid) != 64 {
		t.Fatalf("expected 64-char oid, got: %q", oid)
	}
	defer cleanupRealCanaryObject(t, sdkEnv, driveCliBin, oid, storageBase)

	// Push commits and LFS objects to Proton Drive.
	mustRun(t, repoPath, sdkEnv, gitBin, "push", "origin", "main")
	lfsPushOutput := mustRun(t, repoPath, sdkEnv, gitLFSBin, "push", "origin", "main")
	if strings.Contains(strings.ToLower(lfsPushOutput), "error") {
		t.Fatalf("unexpected error in lfs push output:\n%s", lfsPushOutput)
	}

	t.Logf("upload complete: oid-prefix=%s, size=%d bytes", oid[:12], len(originalBytes))

	// Clone into a fresh directory, skipping LFS smudge.
	cloneBase := t.TempDir()
	clonePath := filepath.Join(cloneBase, "clone")
	cloneEnv := append(sdkEnv, "GIT_LFS_SKIP_SMUDGE=1")
	mustRun(t, cloneBase, cloneEnv, gitBin, "clone", remotePath, clonePath)

	// Install LFS and configure the clone to use our adapter.
	mustRun(t, clonePath, sdkEnv, gitLFSBin, "install", "--local")
	configureSDKCustomTransfer(t, clonePath, sdkEnv, gitBin, adapterPath, driveCliBin)

	// Pull LFS objects from Proton Drive.
	out, err := runCmd(clonePath, sdkEnv, gitLFSBin, "pull", "origin", "main")
	if err != nil {
		t.Fatalf("expected lfs pull to succeed, err: %v\noutput:\n%s", err, out)
	}

	// Verify downloaded content matches the original byte-for-byte.
	downloadedPath := filepath.Join(clonePath, "proton-lfs.canary")
	downloadedBytes, err := os.ReadFile(downloadedPath)
	if err != nil {
		t.Fatalf("failed to read pulled canary object: %v", err)
	}
	if !bytes.Equal(downloadedBytes, originalBytes) {
		t.Fatalf("content mismatch: original=%d bytes, downloaded=%d bytes", len(originalBytes), len(downloadedBytes))
	}

	t.Logf("E2E real pipeline: oid-prefix=%s, download verified, %d bytes match", oid[:12], len(originalBytes))
}

func cleanupRealCanaryObject(t *testing.T, env []string, driveCliBin, oid, storageBase string) {
	t.Helper()

	nodeBin := strings.TrimSpace(os.Getenv("NODE_BIN"))
	if nodeBin == "" {
		nodeBin = "node"
	}
	if _, err := exec.LookPath(nodeBin); err != nil && !strings.Contains(nodeBin, string(os.PathSeparator)) {
		t.Logf("canary cleanup skipped: node binary unavailable: %v", err)
		return
	}

	request := map[string]any{
		"oid":                oid,
		"storageBase":        storageBase,
		"credentialProvider": envValue(env, "PROTON_CREDENTIAL_PROVIDER", "pass-cli"),
		"allowLogin":         false,
	}
	if provider := envValue(env, "PROTON_DATA_CREDENTIAL_PROVIDER", ""); provider != "" {
		request["dataCredentialProvider"] = provider
		request["dataCredentialHost"] = envValue(env, "PROTON_DATA_CREDENTIAL_HOST", "proton-data.proton-lfs-cli.local")
	}

	body, err := json.Marshal(request)
	if err != nil {
		t.Logf("canary cleanup skipped: request marshal failed: %v", err)
		return
	}

	cmd := exec.Command(nodeBin, driveCliBin, "bridge", "delete")
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(body)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("canary cleanup warning: oid-prefix=%s delete failed: %v [%s]", oid[:12], err, redactBridgeOutput(string(out)))
		return
	}
	t.Logf("canary cleanup attempted: oid-prefix=%s", oid[:12])
}

func envValue(env []string, key, fallback string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimSpace(strings.TrimPrefix(env[i], prefix))
		}
	}
	return fallback
}

func redactBridgeOutput(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) > 240 {
		trimmed = trimmed[:240] + "...[truncated]"
	}
	return strings.ReplaceAll(trimmed, "\n", " ")
}
