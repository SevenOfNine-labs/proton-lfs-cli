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

const (
	stallAdapterDurationMS = 3000
	maxFailureDuration     = 20 * time.Second
)

func buildStalledAdapter(t *testing.T, root string) string {
	t.Helper()

	fileName := "stalled-adapter"
	if runtime.GOOS == "windows" {
		fileName += ".exe"
	}
	outPath := filepath.Join(t.TempDir(), fileName)

	cmd := exec.Command("go", "build", "-trimpath", "-o", outPath, "./tests/integration/testdata/stalled-adapter")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".cache", "go-build"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build stalled adapter helper: %v\n%s", err, string(output))
	}
	return outPath
}

func configureStalledCustomTransfer(t *testing.T, repoPath string, env []string, gitBin, adapterPath, storePath, stallOn string) {
	t.Helper()

	args := "--local-store-dir=" + storePath +
		" --stall-on=" + stallOn +
		" --stall-ms=" + strconv.Itoa(stallAdapterDurationMS)

	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.path", adapterPath)
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.args", args)
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.concurrent", "false")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.direction", "both")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.standalonetransferagent", "proton")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.activitytimeout", "1")
}

func assertTimeoutFailure(t *testing.T, out string, err error, elapsed time.Duration, phase string) {
	t.Helper()

	if elapsed > maxFailureDuration {
		t.Fatalf(
			"expected %s to fail within bounded timeout window; elapsed=%s max=%s\noutput:\n%s",
			phase,
			elapsed,
			maxFailureDuration,
			out,
		)
	}
	if err == nil {
		t.Fatalf("expected %s to fail on stalled adapter, output:\n%s", phase, out)
	}
	if !containsAnyFold(out, "timeout", "timed out", "failed", "error", "not found", "cannot") {
		t.Fatalf("expected timeout/failure output for %s, got:\n%s", phase, out)
	}
}

func TestGitLFSCustomTransferTimeoutUploadFailsFastOnStall(t *testing.T) {
	s := setupRepositoryForUpload(t)
	oid := mustReadSingleOID(t, s)

	stalledAdapter := buildStalledAdapter(t, s.root)
	configureStalledCustomTransfer(t, s.repoPath, s.env, s.gitBin, stalledAdapter, s.storePath, "upload")

	start := time.Now()
	out, err := runCmd(s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	assertTimeoutFailure(t, out, err, time.Since(start), "git push")

	storedPath := filepath.Join(s.storePath, oid[:2], oid[2:4], oid)
	if _, statErr := os.Stat(storedPath); statErr == nil {
		t.Fatalf("did not expect object to be stored after stalled upload: %s", storedPath)
	}
}

func TestGitLFSCustomTransferTimeoutDownloadFailsFastOnStall(t *testing.T) {
	s := setupRepositoryForUpload(t)
	mustRun(t, s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	mustRun(t, s.repoPath, s.env, s.gitLFSBin, "push", "origin", "main")

	cloneBase := t.TempDir()
	clonePath := filepath.Join(cloneBase, "clone")
	cloneEnv := append(s.env, "GIT_LFS_SKIP_SMUDGE=1")
	mustRun(t, cloneBase, cloneEnv, s.gitBin, "clone", s.remotePath, clonePath)
	mustRun(t, clonePath, s.env, s.gitLFSBin, "install", "--local")

	stalledAdapter := buildStalledAdapter(t, s.root)
	configureStalledCustomTransfer(t, clonePath, s.env, s.gitBin, stalledAdapter, s.storePath, "download")

	start := time.Now()
	out, err := runCmd(clonePath, s.env, s.gitLFSBin, "pull", "origin", "main")
	assertTimeoutFailure(t, out, err, time.Since(start), "git lfs pull")

	artifactPath := filepath.Join(clonePath, "artifact.bin")
	contents, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		return
	}
	if strings.TrimSpace(string(contents)) == "proton-lfs-cli-integration" {
		t.Fatalf("did not expect full artifact content after stalled download")
	}
}
