//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sdkPassCLIPath() string {
	passCLIBin := strings.TrimSpace(os.Getenv("PROTON_PASS_CLI_BIN"))
	if passCLIBin == "" {
		passCLIBin = "pass-cli"
	}
	return passCLIBin
}

func sdkProviderEnv(base []string) []string {
	passCLIBin := strings.TrimSpace(os.Getenv("PROTON_PASS_CLI_BIN"))
	if passCLIBin == "" {
		return base
	}
	return append(base, "PROTON_PASS_CLI_BIN="+passCLIBin)
}

// sdkDriveCliBin returns the path to proton-drive-cli, skipping the test if unavailable.
func sdkDriveCliBin(t *testing.T, root string) string {
	t.Helper()

	driveCliBin := strings.TrimSpace(os.Getenv("PROTON_DRIVE_CLI_BIN"))
	if driveCliBin == "" {
		driveCliBin = filepath.Join(root, "submodules", "proton-drive-cli", "dist", "index.js")
	}
	if _, err := os.Stat(driveCliBin); err != nil {
		t.Skipf("sdk integration test skipped: proton-drive-cli not available at %s: %v", driveCliBin, err)
	}
	return driveCliBin
}

// configureSDKCustomTransfer configures Git LFS to use the SDK backend
// with the adapter pointing directly at the proton-drive-cli binary.
func configureSDKCustomTransfer(t *testing.T, repoPath string, env []string, gitBin, adapterPath, driveCliBin string) {
	t.Helper()

	sdkArgs := fmt.Sprintf("--backend=sdk --drive-cli-bin=%s", driveCliBin)
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.path", adapterPath)
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.args", sdkArgs)
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.concurrent", "false")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.direction", "both")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.standalonetransferagent", "proton")
}

func TestGitLFSCustomTransferSDKBackendRoundTrip(t *testing.T) {
	if strings.TrimSpace(os.Getenv("PROTON_LFS_RUN_SDK_INTEGRATION")) != "1" {
		t.Skip("sdk integration test skipped: set PROTON_LFS_RUN_SDK_INTEGRATION=1 or run make test-integration-sdk")
	}

	s := setupRepositoryForUpload(t)

	driveCliBin := sdkDriveCliBin(t, s.root)
	sdkEnv := sdkProviderEnv(s.env)

	configureSDKCustomTransfer(t, s.repoPath, sdkEnv, s.gitBin, s.adapterPath, driveCliBin)

	lsFilesOutput := mustRun(t, s.repoPath, sdkEnv, s.gitLFSBin, "ls-files", "-l")
	fields := strings.Fields(strings.TrimSpace(lsFilesOutput))
	if len(fields) == 0 {
		t.Fatalf("expected oid in git lfs ls-files output, got:\n%s", lsFilesOutput)
	}
	oid := fields[0]
	if len(oid) != 64 {
		t.Fatalf("expected oid in git lfs ls-files output, got: %q", oid)
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
	configureSDKCustomTransfer(t, clonePath, sdkEnv, s.gitBin, s.adapterPath, driveCliBin)

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
		t.Fatalf("unexpected pulled artifact bytes: %q", string(contents))
	}
}
