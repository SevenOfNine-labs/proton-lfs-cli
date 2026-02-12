//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parseLFSOIDsByPath(output string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		oid := fields[0]
		path := fields[len(fields)-1]
		result[path] = oid
	}
	return result
}

func TestGitLFSCustomTransferConcurrentRoundTrip(t *testing.T) {
	s := setupRepositoryForUpload(t)

	artifacts := []struct {
		name string
		data []byte
	}{
		{name: "artifact-a.bin", data: bytes.Repeat([]byte("A"), 160*1024)},
		{name: "artifact-b.bin", data: bytes.Repeat([]byte("B"), 180*1024)},
		{name: "artifact-c.bin", data: bytes.Repeat([]byte("C"), 200*1024)},
		{name: "artifact-d.bin", data: bytes.Repeat([]byte("D"), 220*1024)},
	}

	addArgs := []string{"add"}
	for _, artifact := range artifacts {
		path := filepath.Join(s.repoPath, artifact.name)
		if err := os.WriteFile(path, artifact.data, 0o600); err != nil {
			t.Fatalf("failed to create %s: %v", artifact.name, err)
		}
		addArgs = append(addArgs, artifact.name)
	}
	mustRun(t, s.repoPath, s.env, s.gitBin, addArgs...)
	mustRun(t, s.repoPath, s.env, s.gitBin, "commit", "-m", "add concurrent lfs artifacts")

	mustRun(t, s.repoPath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.concurrent", "true")
	mustRun(t, s.repoPath, s.env, s.gitBin, "config", "lfs.concurrenttransfers", "4")

	lsFilesOutput := mustRun(t, s.repoPath, s.env, s.gitLFSBin, "ls-files", "-l")
	oidByPath := parseLFSOIDsByPath(lsFilesOutput)

	for _, artifact := range artifacts {
		oid := oidByPath[artifact.name]
		if len(oid) != 64 {
			t.Fatalf("expected 64-char oid for %s, got %q (ls-files output: %s)", artifact.name, oid, lsFilesOutput)
		}
	}

	mustRun(t, s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	mustRun(t, s.repoPath, s.env, s.gitLFSBin, "push", "origin", "main")

	for _, artifact := range artifacts {
		oid := oidByPath[artifact.name]
		storedPath := filepath.Join(s.storePath, oid[:2], oid[2:4], oid)
		if _, err := os.Stat(storedPath); err != nil {
			t.Fatalf("expected uploaded object for %s in local store, path=%s err=%v", artifact.name, storedPath, err)
		}
	}

	cloneBase := t.TempDir()
	clonePath := filepath.Join(cloneBase, "clone")
	cloneEnv := append(s.env, "GIT_LFS_SKIP_SMUDGE=1")
	mustRun(t, cloneBase, cloneEnv, s.gitBin, "clone", s.remotePath, clonePath)

	mustRun(t, clonePath, s.env, s.gitLFSBin, "install", "--local")
	mustRun(t, clonePath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.path", s.adapterPath)
	mustRun(t, clonePath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.args", fmt.Sprintf("--local-store-dir=%s", s.storePath))
	mustRun(t, clonePath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.concurrent", "true")
	mustRun(t, clonePath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.direction", "both")
	mustRun(t, clonePath, s.env, s.gitBin, "config", "lfs.standalonetransferagent", "proton")
	mustRun(t, clonePath, s.env, s.gitBin, "config", "lfs.concurrenttransfers", "4")

	out, err := runCmd(clonePath, s.env, s.gitLFSBin, "pull", "origin", "main")
	if err != nil {
		t.Fatalf("expected concurrent lfs pull to succeed, err: %v\noutput:\n%s", err, out)
	}

	for _, artifact := range artifacts {
		path := filepath.Join(clonePath, artifact.name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read pulled artifact %s: %v", artifact.name, err)
		}
		if !bytes.Equal(contents, artifact.data) {
			t.Fatalf("unexpected pulled bytes for %s", artifact.name)
		}
	}
}
