//go:build integration

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "cmd", "adapter", "main.go")); err != nil {
		t.Fatalf("unable to resolve repository root from %s: %v", wd, err)
	}
	return root
}

func findToolBinary(root, envName, defaultName string) (string, error) {
	if v := os.Getenv(envName); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v, nil
		}
		return "", fmt.Errorf("%s is set but not usable: %s", envName, v)
	}

	if p, err := exec.LookPath(defaultName); err == nil {
		return p, nil
	}

	candidate := filepath.Join(root, "submodules", "git-lfs", "bin", defaultName)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	return "", fmt.Errorf("unable to find %s binary", defaultName)
}

func buildAdapter(t *testing.T, root string) string {
	t.Helper()

	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "git-lfs-proton-adapter")
	cmd := exec.Command("go", "build", "-trimpath", "-o", outPath, "./cmd/adapter")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".cache", "go-build"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build adapter: %v\n%s", err, string(output))
	}
	return outPath
}

func runCmd(dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustRun(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()

	out, err := runCmd(dir, env, name, args...)
	if err != nil {
		t.Fatalf("command failed: %s %s\nerr: %v\noutput:\n%s", name, strings.Join(args, " "), err, out)
	}
	return out
}

func envWithPath(pathPrefix string) []string {
	env := os.Environ()
	current := os.Getenv("PATH")
	return append(env, "PATH="+pathPrefix+string(os.PathListSeparator)+current)
}

type integrationSetup struct {
	root        string
	env         []string
	gitBin      string
	gitLFSBin   string
	adapterPath string
	storePath   string
	remotePath  string
	repoPath    string
}

func setupRepositoryForUpload(t *testing.T) integrationSetup {
	t.Helper()

	root := repoRoot(t)

	gitBin, err := findToolBinary(root, "GIT_BIN", "git")
	if err != nil {
		t.Skipf("integration test skipped: %v", err)
	}

	gitLFSBin, err := findToolBinary(root, "GIT_LFS_BIN", "git-lfs")
	if err != nil {
		t.Skipf("integration test skipped: %v", err)
	}

	adapterPath := buildAdapter(t, root)
	env := envWithPath(filepath.Dir(gitLFSBin))

	base := t.TempDir()
	storePath := filepath.Join(base, "lfs-store")
	remotePath := filepath.Join(base, "remote.git")
	repoPath := filepath.Join(base, "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	mustRun(t, base, env, gitBin, "init", "--bare", remotePath)
	mustRun(t, remotePath, env, gitBin, "symbolic-ref", "HEAD", "refs/heads/main")
	mustRun(t, repoPath, env, gitBin, "init")
	mustRun(t, repoPath, env, gitBin, "checkout", "-b", "main")
	mustRun(t, repoPath, env, gitBin, "config", "user.name", "Integration Test")
	mustRun(t, repoPath, env, gitBin, "config", "user.email", "integration@example.com")
	mustRun(t, repoPath, env, gitBin, "config", "commit.gpgsign", "false")
	mustRun(t, repoPath, env, gitBin, "remote", "add", "origin", remotePath)

	mustRun(t, repoPath, env, gitLFSBin, "install", "--local")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.path", adapterPath)
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.args", fmt.Sprintf("--local-store-dir=%s", storePath))
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.concurrent", "false")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.direction", "both")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.standalonetransferagent", "proton")

	mustRun(t, repoPath, env, gitLFSBin, "track", "*.bin")

	filePath := filepath.Join(repoPath, "artifact.bin")
	data := []byte("proton-git-lfs-integration")
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		t.Fatalf("failed to create test LFS file: %v", err)
	}

	mustRun(t, repoPath, env, gitBin, "add", ".gitattributes", "artifact.bin")
	mustRun(t, repoPath, env, gitBin, "commit", "-m", "add lfs artifact")

	return integrationSetup{
		root:        root,
		env:         env,
		gitBin:      gitBin,
		gitLFSBin:   gitLFSBin,
		adapterPath: adapterPath,
		storePath:   storePath,
		remotePath:  remotePath,
		repoPath:    repoPath,
	}
}

func TestGitLFSCustomTransferStandaloneUpload(t *testing.T) {
	s := setupRepositoryForUpload(t)

	lsFilesOutput := mustRun(t, s.repoPath, s.env, s.gitLFSBin, "ls-files", "-l")
	if !strings.Contains(lsFilesOutput, "artifact.bin") {
		t.Fatalf("expected artifact.bin in git lfs ls-files output, got:\n%s", lsFilesOutput)
	}
	oid := strings.Fields(strings.TrimSpace(lsFilesOutput))[0]
	if len(oid) != 64 {
		t.Fatalf("expected oid in git lfs ls-files output, got: %q", oid)
	}

	mustRun(t, s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	lfsPushOutput := mustRun(t, s.repoPath, s.env, s.gitLFSBin, "push", "origin", "main")

	if strings.Contains(strings.ToLower(lfsPushOutput), "error") {
		t.Fatalf("unexpected error in lfs push output:\n%s", lfsPushOutput)
	}
	storedPath := filepath.Join(s.storePath, oid[:2], oid[2:4], oid)
	if _, err := os.Stat(storedPath); err != nil {
		t.Fatalf("expected uploaded object in local store, path=%s err=%v", storedPath, err)
	}
}

func TestGitLFSCustomTransferDownloadRoundTrip(t *testing.T) {
	s := setupRepositoryForUpload(t)

	// Upload succeeds in the source repository and pushes pointers.
	mustRun(t, s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	mustRun(t, s.repoPath, s.env, s.gitLFSBin, "push", "origin", "main")

	cloneBase := t.TempDir()
	clonePath := filepath.Join(cloneBase, "clone")
	cloneEnv := append(s.env, "GIT_LFS_SKIP_SMUDGE=1")
	mustRun(t, cloneBase, cloneEnv, s.gitBin, "clone", s.remotePath, clonePath)

	mustRun(t, clonePath, s.env, s.gitLFSBin, "install", "--local")
	mustRun(t, clonePath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.path", s.adapterPath)
	mustRun(t, clonePath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.args", fmt.Sprintf("--local-store-dir=%s", s.storePath))
	mustRun(t, clonePath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.concurrent", "false")
	mustRun(t, clonePath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.direction", "both")
	mustRun(t, clonePath, s.env, s.gitBin, "config", "lfs.standalonetransferagent", "proton")

	out, err := runCmd(clonePath, s.env, s.gitLFSBin, "pull", "origin", "main")
	if err != nil {
		t.Fatalf("expected lfs pull to succeed, err: %v\noutput:\n%s", err, out)
	}

	artifactPath := filepath.Join(clonePath, "artifact.bin")
	contents, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("failed to read pulled artifact: %v", err)
	}
	if string(contents) != "proton-git-lfs-integration" {
		t.Fatalf("unexpected pulled artifact bytes: %q", string(contents))
	}
}
