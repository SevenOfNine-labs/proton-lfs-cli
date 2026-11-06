//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// lfs.activitytimeout applies to the git-lfs HTTP client only (git-lfs
// docs/man/git-lfs-config.adoc); a custom adapter that exits mid-transfer is
// detected when its stdout closes.

const (
	// adapterExitDelayMS is how long the helper adapter waits after the
	// transfer request before exiting.
	adapterExitDelayMS = 3000
	// maxFailureDuration bounds how long git-lfs may take to report the
	// failure once the adapter has exited.
	maxFailureDuration = 20 * time.Second
)

func buildMidTransferExitAdapter(t *testing.T, root string) string {
	t.Helper()

	fileName := "midtransfer-exit-adapter"
	if runtime.GOOS == "windows" {
		fileName += ".exe"
	}
	outPath := filepath.Join(t.TempDir(), fileName)

	cmd := exec.Command(goBinary(), "build", "-trimpath", "-o", outPath, "./tests/integration/testdata/midtransfer-exit-adapter")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".cache", "go-build"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build mid-transfer exit adapter helper: %v\n%s", err, string(output))
	}
	return outPath
}

func configureMidTransferExitCustomTransfer(t *testing.T, repoPath string, env []string, gitBin, adapterPath, exitOn string) {
	t.Helper()

	args := "--exit-on=" + exitOn + " --delay-ms=" + strconv.Itoa(adapterExitDelayMS)

	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.path", adapterPath)
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.args", args)
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.concurrent", "false")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.direction", "both")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.standalonetransferagent", "proton")
}

func assertMidTransferExitFailure(t *testing.T, out string, err error, elapsed time.Duration, phase string) {
	t.Helper()

	if elapsed > maxFailureDuration {
		t.Fatalf(
			"expected %s to fail promptly after the adapter exited; elapsed=%s max=%s\noutput:\n%s",
			phase,
			elapsed,
			maxFailureDuration,
			out,
		)
	}
	if err == nil {
		t.Fatalf("expected %s to fail when the adapter exits mid-transfer, output:\n%s", phase, out)
	}
	if !containsAnyFold(out, "failed", "error", "not found", "cannot", "exit") {
		t.Fatalf("expected a transfer failure message for %s, got:\n%s", phase, out)
	}
}

func TestGitLFSCustomTransferAdapterExitsMidUpload(t *testing.T) {
	s := setupRepositoryForUpload(t)
	oid := mustReadSingleOID(t, s)

	adapter := buildMidTransferExitAdapter(t, s.root)
	configureMidTransferExitCustomTransfer(t, s.repoPath, s.env, s.gitBin, adapter, "upload")

	start := time.Now()
	out, err := runCmd(s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	assertMidTransferExitFailure(t, out, err, time.Since(start), "git push")

	storedPath := filepath.Join(s.storePath, oid[:2], oid[2:4], oid)
	if _, statErr := os.Stat(storedPath); statErr == nil {
		t.Fatalf("did not expect object to be stored after the adapter exited mid-upload: %s", storedPath)
	}
}

func TestGitLFSCustomTransferAdapterExitsMidDownload(t *testing.T) {
	s := setupRepositoryForUpload(t)
	mustRun(t, s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	mustRun(t, s.repoPath, s.env, s.gitLFSBin, "push", "origin", "main")

	cloneBase := t.TempDir()
	clonePath := filepath.Join(cloneBase, "clone")
	cloneEnv := append(s.env, "GIT_LFS_SKIP_SMUDGE=1")
	mustRun(t, cloneBase, cloneEnv, s.gitBin, "clone", s.remotePath, clonePath)
	mustRun(t, clonePath, s.env, s.gitLFSBin, "install", "--local")

	adapter := buildMidTransferExitAdapter(t, s.root)
	configureMidTransferExitCustomTransfer(t, clonePath, s.env, s.gitBin, adapter, "download")

	start := time.Now()
	out, err := runCmd(clonePath, s.env, s.gitLFSBin, "pull", "origin", "main")
	assertMidTransferExitFailure(t, out, err, time.Since(start), "git lfs pull")

	artifactPath := filepath.Join(clonePath, "artifact.bin")
	contents, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		return
	}
	if strings.TrimSpace(string(contents)) == "proton-lfs-cli-integration" {
		t.Fatalf("did not expect full artifact content after the adapter exited mid-download")
	}
}
