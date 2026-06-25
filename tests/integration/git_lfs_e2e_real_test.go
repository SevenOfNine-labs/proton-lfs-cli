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
// real Proton Drive E2E: offline doctor reports a browser-fork-ready session
// and proton-drive-cli is built.
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
	requireLiveDriveScopeCanary(t, driveCliBin, doctorArgs)

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

func requireLiveDriveScopeCanary(t *testing.T, driveCliBin, doctorArgs string) {
	t.Helper()

	nodeBin := strings.TrimSpace(os.Getenv("NODE_BIN"))
	if nodeBin == "" {
		nodeBin = "node"
	}

	request, err := liveDriveScopeRequestFromDoctorArgs(doctorArgs)
	if err != nil {
		t.Fatalf("real E2E hard stop: %v", err)
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("real E2E hard stop: live scope request marshal failed: %v", err)
	}

	cmd := exec.Command(nodeBin, driveCliBin, "bridge", "list")
	cmd.Env = os.Environ()
	cmd.Stdin = bytes.NewReader(body)
	out, err := cmd.CombinedOutput()
	if err != nil && strings.TrimSpace(string(out)) == "" {
		t.Fatalf("real E2E hard stop: live Drive scope canary failed before JSON response: %v", err)
	}

	var response struct {
		OK      bool            `json:"ok"`
		Error   string          `json:"error"`
		Code    int             `json:"code"`
		Details json.RawMessage `json:"details"`
	}
	if parseErr := json.Unmarshal(bytes.TrimSpace(out), &response); parseErr != nil {
		t.Fatalf("real E2E hard stop: live Drive scope canary returned invalid JSON: %v [%s]", parseErr, redactBridgeOutput(string(out)))
	}
	if response.OK {
		t.Log("live Drive scope canary passed: one read-only metadata request succeeded")
		return
	}

	details := parseBridgeDetails(response.Details)
	errorCode := strings.TrimSpace(details["errorCode"])
	protonCode := strings.TrimSpace(details["protonCode"])
	if errorCode == "INSUFFICIENT_SCOPE" || protonCode == "9101" || strings.Contains(response.Error, "9101") || strings.Contains(strings.ToLower(response.Error), "sufficient scope") {
		t.Fatalf("real E2E hard stop: live Drive scope canary returned INSUFFICIENT_SCOPE / Proton API 9101; refusing LFS transfer [%s]", redactBridgeOutput(string(out)))
	}
	t.Fatalf("real E2E hard stop: live Drive scope canary failed; refusing LFS transfer [%s]", redactBridgeOutput(string(out)))
}

func liveDriveScopeRequestFromDoctorArgs(doctorArgs string) (map[string]string, error) {
	request := map[string]string{"folder": "/"}
	tokens := strings.Fields(doctorArgs)

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		option, value, hasInlineValue := strings.Cut(token, "=")

		switch option {
		case "--require-data-password":
			continue
		case "--key-password-provider", "--key-password-host":
			if hasInlineValue {
				if value == "" {
					return nil, fmt.Errorf("empty value for %s", option)
				}
				continue
			}
			i++
			if i >= len(tokens) || strings.HasPrefix(tokens[i], "--") {
				return nil, fmt.Errorf("missing value after %s", option)
			}
		case "--data-credential-provider", "--data-credential-host":
			if !hasInlineValue {
				i++
				if i >= len(tokens) || strings.HasPrefix(tokens[i], "--") {
					return nil, fmt.Errorf("missing value after %s", option)
				}
				value = tokens[i]
			}
			if value == "" {
				return nil, fmt.Errorf("empty value for %s", option)
			}
			if option == "--data-credential-provider" {
				request["dataCredentialProvider"] = value
			} else {
				request["dataCredentialHost"] = value
			}
		default:
			return nil, fmt.Errorf("unsupported LIVE_CANARY_DOCTOR_ARGS option for live scope canary: %s", option)
		}
	}

	return request, nil
}

func parseBridgeDetails(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil && strings.TrimSpace(asString) != "" {
		var out map[string]any
		if err := json.Unmarshal([]byte(asString), &out); err == nil {
			return stringifyJSONMap(out)
		}
	}

	var asObject map[string]any
	if err := json.Unmarshal(raw, &asObject); err == nil {
		return stringifyJSONMap(asObject)
	}

	return nil
}

func stringifyJSONMap(in map[string]any) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = strings.TrimSpace(fmt.Sprint(value))
	}
	return out
}

// TestE2ERealProtonDrivePipeline exercises the full Git LFS pipeline through
// Proton Drive: commit one tiny canary object, push via the adapter, clone into
// a fresh repo, pull, verify byte-for-byte fidelity, and attempt cleanup.
//
// Prerequisites:
//   - PROTON_LFS_LIVE_CANARY is set to the exact acknowledgement
//   - LIVE_CANARY_DOCTOR_ARGS is set
//   - offline doctor and one read-only Drive scope canary pass
//   - browser-fork login already completed for a disposable Proton account
//   - proton-drive-cli built (make build-drive-cli)
func TestE2ERealProtonDrivePipeline(t *testing.T) {
	root, storageBase := requireRealE2EPrereqs(t)

	testDataPath := filepath.Join(root, "testdata-lfs", "tiny-1kb.bin")
	originalBytes, err := os.ReadFile(testDataPath)
	if err != nil {
		t.Fatalf("failed to read live canary fixture %s: %v", testDataPath, err)
	}

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

	// Preserve provider-specific env only; account auth came from browser-fork.
	sdkEnv := append(sdkProviderEnv(env), "LFS_STORAGE_BASE="+storageBase)
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

	t.Logf("upload complete: oid-prefix=%s, fixture=%s, size=%d bytes", oid[:12], filepath.Base(testDataPath), len(originalBytes))

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
		"oid":         oid,
		"storageBase": storageBase,
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
