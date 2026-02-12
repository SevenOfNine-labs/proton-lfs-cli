//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	defaultStressFileCount   = 24
	defaultStressRounds      = 3
	defaultStressConcurrency = 8
	stressMinFileSize        = 96 * 1024
	stressFileSizeStep       = 8 * 1024
	stressFileSizeJitter     = 5
)

func parseEnvIntWithMin(name string, fallback, minValue int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minValue {
		return fallback
	}
	return parsed
}

func stressArtifactData(index, round int) []byte {
	size := stressMinFileSize + ((index % stressFileSizeJitter) * stressFileSizeStep)
	data := bytes.Repeat([]byte{byte('a' + (index+round)%26)}, size)
	header := []byte(fmt.Sprintf("stress-round=%d artifact=%d\n", round, index))
	copy(data, header)
	return data
}

func configureConcurrentTransfer(t *testing.T, repoPath string, s integrationSetup, concurrency int) {
	t.Helper()

	mustRun(t, repoPath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.path", s.adapterPath)
	mustRun(t, repoPath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.args", fmt.Sprintf("--local-store-dir=%s", s.storePath))
	mustRun(t, repoPath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.concurrent", "true")
	mustRun(t, repoPath, s.env, s.gitBin, "config", "lfs.customtransfer.proton.direction", "both")
	mustRun(t, repoPath, s.env, s.gitBin, "config", "lfs.standalonetransferagent", "proton")
	mustRun(t, repoPath, s.env, s.gitBin, "config", "lfs.concurrenttransfers", strconv.Itoa(concurrency))
}

func TestGitLFSCustomTransferConcurrentStressSoak(t *testing.T) {
	fileCount := parseEnvIntWithMin("PROTON_LFS_STRESS_FILE_COUNT", defaultStressFileCount, 8)
	rounds := parseEnvIntWithMin("PROTON_LFS_STRESS_ROUNDS", defaultStressRounds, 1)
	concurrency := parseEnvIntWithMin("PROTON_LFS_STRESS_CONCURRENCY", defaultStressConcurrency, 2)

	s := setupRepositoryForUpload(t)
	configureConcurrentTransfer(t, s.repoPath, s, concurrency)

	artifacts := make([]string, 0, fileCount)
	for i := 0; i < fileCount; i++ {
		artifacts = append(artifacts, fmt.Sprintf("stress-%03d.bin", i))
	}

	for round := 1; round <= rounds; round++ {
		addArgs := []string{"add"}
		expectedByName := make(map[string][]byte, len(artifacts))

		for i, name := range artifacts {
			data := stressArtifactData(i, round)
			expectedByName[name] = data

			path := filepath.Join(s.repoPath, name)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatalf("failed to write %s for round %d: %v", name, round, err)
			}
			addArgs = append(addArgs, name)
		}

		mustRun(t, s.repoPath, s.env, s.gitBin, addArgs...)
		mustRun(t, s.repoPath, s.env, s.gitBin, "commit", "-m", fmt.Sprintf("stress round %d (%d files)", round, fileCount))
		mustRun(t, s.repoPath, s.env, s.gitBin, "push", "origin", "main")
		mustRun(t, s.repoPath, s.env, s.gitLFSBin, "push", "origin", "main")

		lsFilesOutput := mustRun(t, s.repoPath, s.env, s.gitLFSBin, "ls-files", "-l")
		oidByPath := parseLFSOIDsByPath(lsFilesOutput)
		for _, name := range artifacts {
			oid := oidByPath[name]
			if len(oid) != 64 {
				t.Fatalf("expected 64-char oid for %s in round %d, got %q", name, round, oid)
			}
			storedPath := filepath.Join(s.storePath, oid[:2], oid[2:4], oid)
			if _, err := os.Stat(storedPath); err != nil {
				t.Fatalf("expected stored object for %s round %d at %s: %v", name, round, storedPath, err)
			}
		}

		cloneBase := t.TempDir()
		clonePath := filepath.Join(cloneBase, fmt.Sprintf("clone-round-%d", round))
		cloneEnv := append(s.env, "GIT_LFS_SKIP_SMUDGE=1")
		mustRun(t, cloneBase, cloneEnv, s.gitBin, "clone", s.remotePath, clonePath)
		mustRun(t, clonePath, s.env, s.gitLFSBin, "install", "--local")
		configureConcurrentTransfer(t, clonePath, s, concurrency)

		var (
			pullOut string
			pullErr error
		)
		for attempt := 1; attempt <= 2; attempt++ {
			pullOut, pullErr = runCmd(clonePath, s.env, s.gitLFSBin, "pull", "origin", "main")
			if pullErr == nil {
				break
			}
			lfsLogOut, _ := runCmd(clonePath, s.env, s.gitLFSBin, "logs", "last")
			t.Logf("round %d: lfs pull attempt %d failed: %v\noutput:\n%s\nlfs log:\n%s", round, attempt, pullErr, pullOut, lfsLogOut)
		}
		if pullErr != nil {
			lfsLogOut, _ := runCmd(clonePath, s.env, s.gitLFSBin, "logs", "last")
			t.Fatalf("round %d: expected lfs pull to succeed after retry, err: %v\noutput:\n%s\nlfs log:\n%s", round, pullErr, pullOut, lfsLogOut)
		}

		for _, name := range artifacts {
			path := filepath.Join(clonePath, name)
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("round %d: failed to read pulled artifact %s: %v", round, name, err)
			}
			if !bytes.Equal(contents, expectedByName[name]) {
				t.Fatalf("round %d: unexpected pulled bytes for %s", round, name)
			}
		}
	}
}
