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

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/bridgeproto"
	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/preflight"
)

const liveCanaryAckValue = "I_UNDERSTAND_THIS_TOUCHES_A_REAL_PROTON_ACCOUNT"

// requireRealE2EPrereqs skips the test unless the live-canary acknowledgement
// is set, proton-drive-cli is built and the offline doctor reports ready.
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

	driveCliBin := strings.TrimSpace(os.Getenv("PROTON_DRIVE_CLI_BIN"))
	if driveCliBin == "" {
		driveCliBin = filepath.Join(root, "submodules", "proton-drive-cli", "dist", "index.js")
	}
	if _, err := os.Stat(driveCliBin); err != nil {
		t.Skipf("real E2E test skipped: proton-drive-cli not built at %s (run: make build-drive-cli)", driveCliBin)
	}

	requireLiveCanaryDoctor(t, driveCliBin, doctorArgs)
	requireLiveDriveScopeCanary(t, driveCliBin)

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

	// The doctor flags take values without spaces, so Fields splits them
	// without a shell.
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

func requireLiveDriveScopeCanary(t *testing.T, driveCliBin string) {
	t.Helper()

	nodeBin := strings.TrimSpace(os.Getenv("NODE_BIN"))
	if nodeBin == "" {
		nodeBin = "node"
	}

	cmd := exec.Command(nodeBin, driveCliBin, "bridge", "list")
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(`{"folder":"/"}`)
	out, err := cmd.CombinedOutput()
	if err != nil && strings.TrimSpace(string(out)) == "" {
		t.Fatalf("real E2E hard stop: live Drive scope canary failed before JSON response: %v", err)
	}

	var response struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Code    int    `json:"code"`
		Details string `json:"details"`
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

// parseBridgeDetails decodes the JSON-encoded details string of a failed
// bridge response (schemas/bridge/v1/response-envelope.schema.json).
func parseBridgeDetails(raw string) map[string]string {
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return stringifyJSONMap(out)
}

func stringifyJSONMap(in map[string]any) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = strings.TrimSpace(fmt.Sprint(value))
	}
	return out
}

// TestE2ERealProtonDrivePipeline pushes one canary object through the adapter
// to Proton Drive, pulls it into a fresh clone, compares the bytes and deletes
// the object; requireRealE2EPrereqs gates it.
func TestE2ERealProtonDrivePipeline(t *testing.T) {
	root, storageBase := requireRealE2EPrereqs(t)

	testDataPath := filepath.Join(root, "testdata-lfs", "tiny-1kb.bin")
	originalBytes, err := os.ReadFile(testDataPath)
	if err != nil {
		t.Fatalf("failed to read live canary fixture %s: %v", testDataPath, err)
	}

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

	driveCliBin := sdkDriveCliBin(t, root)

	sdkEnv := append(sdkPassCLIEnv(env), "LFS_STORAGE_BASE="+storageBase)
	t.Logf("real canary storage base: %s", storageBase)

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

	mustRun(t, repoPath, sdkEnv, gitLFSBin, "track", "*.canary")

	canaryDest := filepath.Join(repoPath, "proton-lfs.canary")
	if err := os.WriteFile(canaryDest, originalBytes, 0o600); err != nil {
		t.Fatalf("failed to write canary object to repo: %v", err)
	}

	mustRun(t, repoPath, sdkEnv, gitBin, "add", ".gitattributes", "proton-lfs.canary")
	mustRun(t, repoPath, sdkEnv, gitBin, "commit", "-m", "add tiny live canary via LFS")

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

	mustRun(t, repoPath, sdkEnv, gitBin, "push", "origin", "main")
	lfsPushOutput := mustRun(t, repoPath, sdkEnv, gitLFSBin, "push", "origin", "main")
	if strings.Contains(strings.ToLower(lfsPushOutput), "error") {
		t.Fatalf("unexpected error in lfs push output:\n%s", lfsPushOutput)
	}

	t.Logf("upload complete: oid-prefix=%s, fixture=%s, size=%d bytes", oid[:12], filepath.Base(testDataPath), len(originalBytes))

	cloneBase := t.TempDir()
	clonePath := filepath.Join(cloneBase, "clone")
	cloneEnv := append(sdkEnv, "GIT_LFS_SKIP_SMUDGE=1")
	mustRun(t, cloneBase, cloneEnv, gitBin, "clone", remotePath, clonePath)

	mustRun(t, clonePath, sdkEnv, gitLFSBin, "install", "--local")
	configureSDKCustomTransfer(t, clonePath, sdkEnv, gitBin, adapterPath, driveCliBin)

	out, err := runCmd(clonePath, sdkEnv, gitLFSBin, "pull", "origin", "main")
	if err != nil {
		t.Fatalf("expected lfs pull to succeed, err: %v\noutput:\n%s", err, out)
	}

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

func redactBridgeOutput(raw string) string {
	redacted := bridgeproto.RedactText(strings.TrimSpace(raw))
	return strings.ReplaceAll(bridgeproto.Truncate(redacted, 240), "\n", " ")
}
