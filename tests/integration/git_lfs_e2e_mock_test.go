//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EMockedPipeline exercises the full Git LFS pipeline through the
// adapter and mock proton-drive-cli bridge subprocess (direct subprocess,
// no HTTP bridge).
//
// Prerequisites:
//   - PROTON_DRIVE_CLI_BIN points at mock-proton-drive-cli.js
func TestE2EMockedPipeline(t *testing.T) {
	root := repoRoot(t)

	// Verify mock infrastructure is available.
	mockBridge := os.Getenv("PROTON_DRIVE_CLI_BIN")
	if mockBridge == "" {
		mockBridge = filepath.Join(root, "tests", "testdata", "mock-proton-drive-cli.js")
	}
	if _, err := os.Stat(mockBridge); err != nil {
		t.Skipf("mock bridge not found at %s: %v", mockBridge, err)
	}

	// Set up mock storage directory for the bridge.
	mockStorageDir := filepath.Join(t.TempDir(), "mock-bridge-storage")

	// Build adapter and set up repository.
	s := setupRepositoryForUpload(t)

	// Build bridge environment. Account auth is represented by mock auth-state.
	sdkEnv := append(
		s.env,
		"PROTON_DRIVE_CLI_BIN="+mockBridge,
		"MOCK_BRIDGE_STORAGE_DIR="+mockStorageDir,
	)

	// Configure Git LFS to use the SDK backend pointing at mock bridge.
	configureSDKCustomTransfer(t, s.repoPath, sdkEnv, s.gitBin, s.adapterPath, mockBridge)

	// Verify the OID from the tracked file.
	lsFilesOutput := mustRun(t, s.repoPath, sdkEnv, s.gitLFSBin, "ls-files", "-l")
	fields := strings.Fields(strings.TrimSpace(lsFilesOutput))
	if len(fields) == 0 {
		t.Fatalf("expected oid in git lfs ls-files output, got:\n%s", lsFilesOutput)
	}
	oid := fields[0]
	if len(oid) != 64 {
		t.Fatalf("expected 64-char oid, got: %q", oid)
	}

	// Push commits and LFS objects.
	mustRun(t, s.repoPath, sdkEnv, s.gitBin, "push", "origin", "main")
	lfsPushOutput := mustRun(t, s.repoPath, sdkEnv, s.gitLFSBin, "push", "origin", "main")
	if strings.Contains(strings.ToLower(lfsPushOutput), "error") {
		t.Fatalf("unexpected error in lfs push output:\n%s", lfsPushOutput)
	}

	// Clone into a fresh directory, skipping LFS smudge.
	cloneBase := t.TempDir()
	clonePath := filepath.Join(cloneBase, "clone")
	cloneEnv := append(sdkEnv, "GIT_LFS_SKIP_SMUDGE=1")
	mustRun(t, cloneBase, cloneEnv, s.gitBin, "clone", s.remotePath, clonePath)

	// Install LFS and configure the clone to use our adapter.
	mustRun(t, clonePath, sdkEnv, s.gitLFSBin, "install", "--local")
	configureSDKCustomTransfer(t, clonePath, sdkEnv, s.gitBin, s.adapterPath, mockBridge)

	// Pull LFS objects.
	out, err := runCmd(clonePath, sdkEnv, s.gitLFSBin, "pull", "origin", "main")
	if err != nil {
		t.Fatalf("expected lfs pull to succeed, err: %v\noutput:\n%s", err, out)
	}

	// Verify downloaded content matches the original.
	artifactPath := filepath.Join(clonePath, "artifact.bin")
	contents, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("failed to read pulled artifact: %v", err)
	}
	if string(contents) != "proton-lfs-cli-integration" {
		t.Fatalf("content mismatch: expected %q, got %q (len=%d)", "proton-lfs-cli-integration", string(contents), len(contents))
	}

	t.Logf("E2E mocked pipeline: upload OID=%s, download verified, content matches", oid)
}

func TestE2EMockedPipelineBlocksMissingBrowserForkKeyPassword(t *testing.T) {
	root := repoRoot(t)

	mockBridge := os.Getenv("PROTON_DRIVE_CLI_BIN")
	if mockBridge == "" {
		mockBridge = filepath.Join(root, "tests", "testdata", "mock-proton-drive-cli.js")
	}
	if _, err := os.Stat(mockBridge); err != nil {
		t.Skipf("mock bridge not found at %s: %v", mockBridge, err)
	}

	mockStorageDir := filepath.Join(t.TempDir(), "mock-bridge-storage")
	s := setupRepositoryForUpload(t)
	sdkEnv := append(
		s.env,
		"PROTON_DRIVE_CLI_BIN="+mockBridge,
		"MOCK_BRIDGE_STORAGE_DIR="+mockStorageDir,
		"MOCK_BRIDGE_AUTH_STATE=needs_key_password",
	)

	configureSDKCustomTransfer(t, s.repoPath, sdkEnv, s.gitBin, s.adapterPath, mockBridge)

	out, err := runCmd(s.repoPath, sdkEnv, s.gitLFSBin, "push", "origin", "main")
	if err == nil {
		t.Fatalf("expected lfs push to fail when browser-fork key password is missing, output:\n%s", out)
	}
	logOut, _ := runCmd(s.repoPath, sdkEnv, s.gitLFSBin, "logs", "last")
	combined := out + "\n" + logOut
	if !containsAnyFold(combined, "key_password_required", "stored browser-fork key password", "missing its stored key password", "needs_key_password") {
		t.Fatalf("expected missing key-password failure, got:\n%s", combined)
	}
}
