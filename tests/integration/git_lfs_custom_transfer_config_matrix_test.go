//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func containsAnyFold(value string, needles ...string) bool {
	lower := strings.ToLower(value)
	for _, needle := range needles {
		if strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func configureLocalCustomTransferDirection(t *testing.T, repoPath string, s integrationSetup, direction string) {
	t.Helper()

	mustRun(t, repoPath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.path", s.adapterPath)
	mustRun(t, repoPath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.args", "--local-store-dir="+s.storePath)
	mustRun(t, repoPath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.concurrent", "false")
	mustRun(t, repoPath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.direction", direction)
	mustRun(t, repoPath, s.env, s.gitBin, "config", "lfs.standalonetransferagent", "proton")
}

func mustReadSingleOID(t *testing.T, s integrationSetup) string {
	t.Helper()

	lsFilesOutput := mustRun(t, s.repoPath, s.env, s.gitLFSBin, "ls-files", "-l")
	fields := strings.Fields(strings.TrimSpace(lsFilesOutput))
	if len(fields) == 0 {
		t.Fatalf("expected oid in git lfs ls-files output, got:\n%s", lsFilesOutput)
	}
	oid := fields[0]
	if len(oid) != 64 {
		t.Fatalf("expected 64-char oid in git lfs ls-files output, got: %q", oid)
	}
	return oid
}

func TestGitLFSCustomTransferDirectionUploadOnlyAllowsPushAndRejectsPull(t *testing.T) {
	s := setupRepositoryForUpload(t)
	configureLocalCustomTransferDirection(t, s.repoPath, s, "upload")
	oid := mustReadSingleOID(t, s)

	// Upload path should work with upload-only direction.
	mustRun(t, s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	mustRun(t, s.repoPath, s.env, s.gitLFSBin, "push", "origin", "main")

	storedPath := filepath.Join(s.storePath, oid[:2], oid[2:4], oid)
	if _, err := os.Stat(storedPath); err != nil {
		t.Fatalf("expected uploaded object in local store, path=%s err=%v", storedPath, err)
	}

	cloneBase := t.TempDir()
	clonePath := filepath.Join(cloneBase, "clone")
	cloneEnv := append(s.env, "GIT_LFS_SKIP_SMUDGE=1")
	mustRun(t, cloneBase, cloneEnv, s.gitBin, "clone", s.remotePath, clonePath)
	mustRun(t, clonePath, s.env, s.gitLFSBin, "install", "--local")
	configureLocalCustomTransferDirection(t, clonePath, s, "upload")

	out, err := runCmd(clonePath, s.env, s.gitLFSBin, "pull", "origin", "main")
	if err == nil {
		t.Fatalf("expected lfs pull to fail when adapter direction is upload-only, output:\n%s", out)
	}
	if !containsAnyFold(out, "error", "failed", "not found", "cannot") {
		t.Fatalf("expected explicit pull failure output, got:\n%s", out)
	}
}

func TestGitLFSCustomTransferDirectionDownloadOnlyRejectsPush(t *testing.T) {
	s := setupRepositoryForUpload(t)
	configureLocalCustomTransferDirection(t, s.repoPath, s, "download")
	oid := mustReadSingleOID(t, s)

	out, err := runCmd(s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	if err == nil {
		t.Fatalf("expected git push to fail when adapter direction is download-only, output:\n%s", out)
	}
	if !containsAnyFold(out, "error", "failed", "not found", "cannot") {
		t.Fatalf("expected explicit push failure output, got:\n%s", out)
	}

	storedPath := filepath.Join(s.storePath, oid[:2], oid[2:4], oid)
	if _, statErr := os.Stat(storedPath); statErr == nil {
		t.Fatalf("did not expect uploaded object after download-only push failure: %s", storedPath)
	}
}

func TestGitLFSCustomTransferDirectionDownloadOnlyAllowsPull(t *testing.T) {
	s := setupRepositoryForUpload(t)
	mustRun(t, s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	mustRun(t, s.repoPath, s.env, s.gitLFSBin, "push", "origin", "main")

	cloneBase := t.TempDir()
	clonePath := filepath.Join(cloneBase, "clone")
	cloneEnv := append(s.env, "GIT_LFS_SKIP_SMUDGE=1")
	mustRun(t, cloneBase, cloneEnv, s.gitBin, "clone", s.remotePath, clonePath)
	mustRun(t, clonePath, s.env, s.gitLFSBin, "install", "--local")
	configureLocalCustomTransferDirection(t, clonePath, s, "download")

	out, err := runCmd(clonePath, s.env, s.gitLFSBin, "pull", "origin", "main")
	if err != nil {
		t.Fatalf("expected lfs pull to succeed with download-only direction, err: %v\noutput:\n%s", err, out)
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
