//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireRealE2EPrereqs skips the test unless the environment is configured for
// real Proton Drive E2E: pass-cli resolves real credentials and proton-drive-cli is built.
func requireRealE2EPrereqs(t *testing.T) (root string) {
	t.Helper()

	root = repoRoot(t)

	// Verify proton-drive-cli is built.
	driveCliBin := strings.TrimSpace(os.Getenv("PROTON_DRIVE_CLI_BIN"))
	if driveCliBin == "" {
		driveCliBin = filepath.Join(root, "submodules", "proton-drive-cli", "dist", "index.js")
	}
	if _, err := os.Stat(driveCliBin); err != nil {
		t.Skipf("real E2E test skipped: proton-drive-cli not built at %s (run: make build-drive-cli)", driveCliBin)
	}

	// Verify pass-cli can resolve real credentials (will skip if not logged in).
	sdkResolvedCredentials(t)

	return root
}

// TestE2ERealProtonDrivePipeline exercises the full Git LFS pipeline through
// Proton Drive: commit a test image, push via the adapter, clone into a fresh
// repo, pull, and verify byte-for-byte fidelity.
//
// Prerequisites:
//   - pass-cli logged in with valid Proton credentials
//   - proton-drive-cli built (make build-drive-cli)
//   - Proton Drive has a top-level folder named "LFS"
func TestE2ERealProtonDrivePipeline(t *testing.T) {
	root := requireRealE2EPrereqs(t)

	// Read the test image and append a timestamp nonce to create unique content.
	// This avoids "file already exists" errors on Proton Drive when re-running.
	testImagePath := filepath.Join(root, "tests", "testdata", "test-image.png")
	imageBytes, err := os.ReadFile(testImagePath)
	if err != nil {
		t.Fatalf("failed to read test image: %v", err)
	}
	if len(imageBytes) == 0 {
		t.Fatal("test image is empty")
	}
	nonce := []byte(fmt.Sprintf("\n# e2e-pipeline-nonce:%d", time.Now().UnixNano()))
	originalBytes := append(imageBytes, nonce...)

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
	sdkEnv := sdkCredentialEnv(t, env)

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

	// Track PNG files with LFS.
	mustRun(t, repoPath, sdkEnv, gitLFSBin, "track", "*.png")

	// Copy test image into the repo.
	imageDest := filepath.Join(repoPath, "test-image.png")
	if err := os.WriteFile(imageDest, originalBytes, 0o644); err != nil {
		t.Fatalf("failed to write test image to repo: %v", err)
	}

	mustRun(t, repoPath, sdkEnv, gitBin, "add", ".gitattributes", "test-image.png")
	mustRun(t, repoPath, sdkEnv, gitBin, "commit", "-m", "add test image via LFS")

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

	// Push commits and LFS objects to Proton Drive.
	mustRun(t, repoPath, sdkEnv, gitBin, "push", "origin", "main")
	lfsPushOutput := mustRun(t, repoPath, sdkEnv, gitLFSBin, "push", "origin", "main")
	if strings.Contains(strings.ToLower(lfsPushOutput), "error") {
		t.Fatalf("unexpected error in lfs push output:\n%s", lfsPushOutput)
	}

	t.Logf("upload complete: OID=%s, size=%d bytes", oid, len(originalBytes))

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
	downloadedPath := filepath.Join(clonePath, "test-image.png")
	downloadedBytes, err := os.ReadFile(downloadedPath)
	if err != nil {
		t.Fatalf("failed to read pulled test image: %v", err)
	}
	if !bytes.Equal(downloadedBytes, originalBytes) {
		t.Fatalf("content mismatch: original=%d bytes, downloaded=%d bytes", len(originalBytes), len(downloadedBytes))
	}

	t.Logf("E2E real pipeline: upload OID=%s, download verified, %d bytes match", oid, len(originalBytes))
}
