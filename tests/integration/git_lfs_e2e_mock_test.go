//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2EMockedPipeline pushes and pulls one object through the adapter and
// tests/testdata/mock-proton-drive-cli.js (or PROTON_DRIVE_CLI_BIN).
func TestE2EMockedPipeline(t *testing.T) {
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
	)

	configureSDKCustomTransfer(t, s.repoPath, sdkEnv, s.gitBin, s.adapterPath, mockBridge)

	lsFilesOutput := mustRun(t, s.repoPath, sdkEnv, s.gitLFSBin, "ls-files", "-l")
	fields := strings.Fields(strings.TrimSpace(lsFilesOutput))
	if len(fields) == 0 {
		t.Fatalf("expected oid in git lfs ls-files output, got:\n%s", lsFilesOutput)
	}
	oid := fields[0]
	if len(oid) != 64 {
		t.Fatalf("expected 64-char oid, got: %q", oid)
	}

	mustRun(t, s.repoPath, sdkEnv, s.gitBin, "push", "origin", "main")
	lfsPushOutput := mustRun(t, s.repoPath, sdkEnv, s.gitLFSBin, "push", "origin", "main")
	if strings.Contains(strings.ToLower(lfsPushOutput), "error") {
		t.Fatalf("unexpected error in lfs push output:\n%s", lfsPushOutput)
	}

	cloneBase := t.TempDir()
	clonePath := filepath.Join(cloneBase, "clone")
	cloneEnv := append(sdkEnv, "GIT_LFS_SKIP_SMUDGE=1")
	mustRun(t, cloneBase, cloneEnv, s.gitBin, "clone", s.remotePath, clonePath)

	mustRun(t, clonePath, sdkEnv, s.gitLFSBin, "install", "--local")
	configureSDKCustomTransfer(t, clonePath, sdkEnv, s.gitBin, s.adapterPath, mockBridge)

	out, err := runCmd(clonePath, sdkEnv, s.gitLFSBin, "pull", "origin", "main")
	if err != nil {
		t.Fatalf("expected lfs pull to succeed, err: %v\noutput:\n%s", err, out)
	}

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

func TestE2EMockedPipelineBlocksUnavailableCredentials(t *testing.T) {
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
		"MOCK_BRIDGE_AUTH_STATE=credentials_unavailable",
	)

	configureSDKCustomTransfer(t, s.repoPath, sdkEnv, s.gitBin, s.adapterPath, mockBridge)

	out, err := runCmd(s.repoPath, sdkEnv, s.gitLFSBin, "push", "origin", "main")
	if err == nil {
		t.Fatalf("expected lfs push to fail when the session credentials are unavailable, output:\n%s", out)
	}
	logOut, _ := runCmd(s.repoPath, sdkEnv, s.gitLFSBin, "logs", "last")
	combined := out + "\n" + logOut
	if !containsAnyFold(combined, "credentials_unavailable", "credentials are unavailable from the credential store") {
		t.Fatalf("expected unavailable-credentials failure, got:\n%s", combined)
	}
}
