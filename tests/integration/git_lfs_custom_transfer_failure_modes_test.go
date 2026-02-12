//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	integrationWrongOID = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
)

func writeExecutableScript(t *testing.T, script string) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("shell script-based integration tests are not supported on windows")
	}

	path := filepath.Join(t.TempDir(), "custom-transfer-test-adapter.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("failed to write test adapter script: %v", err)
	}
	return path
}

func configureScriptCustomTransfer(t *testing.T, repoPath string, env []string, gitBin, scriptPath string) {
	t.Helper()

	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.path", scriptPath)
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.args", "")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.concurrent", "false")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.customtransfer.proton.direction", "both")
	mustRun(t, repoPath, env, gitBin, "config", "lfs.standalonetransferagent", "proton")
}

func TestGitLFSCustomTransferRejectsWrongOIDProgress(t *testing.T) {
	s := setupRepositoryForUpload(t)

	adapterScript := writeExecutableScript(t, `#!/usr/bin/env bash
set -euo pipefail

IFS= read -r _ || exit 0
echo '{}'

IFS= read -r transfer || exit 0
requested_oid="$(printf '%s' "$transfer" | sed -n 's/.*"oid":"\([a-f0-9]\{64\}\)".*/\1/p')"
if [ -z "$requested_oid" ]; then
  requested_oid="0000000000000000000000000000000000000000000000000000000000000000"
fi

wrong_oid="`+integrationWrongOID+`"
if [ "$requested_oid" = "$wrong_oid" ]; then
  wrong_oid="eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
fi

echo "{\"event\":\"progress\",\"oid\":\"${wrong_oid}\",\"bytesSoFar\":1,\"bytesSinceLast\":1}"
echo "{\"event\":\"complete\",\"oid\":\"${requested_oid}\"}"
exit 0
`)
	configureScriptCustomTransfer(t, s.repoPath, s.env, s.gitBin, adapterScript)

	out, err := runCmd(s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	if err == nil {
		t.Fatalf("expected git push to fail on wrong progress oid, output:\n%s", out)
	}

	lower := strings.ToLower(out)
	if !strings.Contains(lower, "unexpected oid") {
		t.Fatalf("expected wrong-oid validation error, got output:\n%s", out)
	}
}

func TestGitLFSCustomTransferRejectsWrongOIDComplete(t *testing.T) {
	s := setupRepositoryForUpload(t)

	adapterScript := writeExecutableScript(t, `#!/usr/bin/env bash
set -euo pipefail

IFS= read -r _ || exit 0
echo '{}'

IFS= read -r transfer || exit 0
requested_oid="$(printf '%s' "$transfer" | sed -n 's/.*"oid":"\([a-f0-9]\{64\}\)".*/\1/p')"
if [ -z "$requested_oid" ]; then
  requested_oid="0000000000000000000000000000000000000000000000000000000000000000"
fi

wrong_oid="`+integrationWrongOID+`"
if [ "$requested_oid" = "$wrong_oid" ]; then
  wrong_oid="eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
fi

echo "{\"event\":\"progress\",\"oid\":\"${requested_oid}\",\"bytesSoFar\":1,\"bytesSinceLast\":1}"
echo "{\"event\":\"complete\",\"oid\":\"${wrong_oid}\"}"
exit 0
`)
	configureScriptCustomTransfer(t, s.repoPath, s.env, s.gitBin, adapterScript)

	out, err := runCmd(s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	if err == nil {
		t.Fatalf("expected git push to fail on wrong completion oid, output:\n%s", out)
	}

	lower := strings.ToLower(out)
	if !strings.Contains(lower, "unexpected oid") {
		t.Fatalf("expected wrong-oid validation error, got output:\n%s", out)
	}
}

func TestGitLFSCustomTransferFailsWhenAdapterCrashesMidTransfer(t *testing.T) {
	s := setupRepositoryForUpload(t)

	adapterScript := writeExecutableScript(t, `#!/usr/bin/env bash
set -euo pipefail

IFS= read -r _ || exit 1
echo '{}'

IFS= read -r _ || exit 1
exit 42
`)
	configureScriptCustomTransfer(t, s.repoPath, s.env, s.gitBin, adapterScript)

	lsFilesOutput := mustRun(t, s.repoPath, s.env, s.gitLFSBin, "ls-files", "-l")
	fields := strings.Fields(strings.TrimSpace(lsFilesOutput))
	if len(fields) == 0 {
		t.Fatalf("expected oid in git lfs ls-files output, got:\n%s", lsFilesOutput)
	}
	oid := fields[0]
	if len(oid) != 64 {
		t.Fatalf("expected 64-char oid in git lfs ls-files output, got: %q", oid)
	}

	out, err := runCmd(s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	if err == nil {
		t.Fatalf("expected git push to fail when adapter exits non-zero, output:\n%s", out)
	}

	storedPath := filepath.Join(s.storePath, oid[:2], oid[2:4], oid)
	if _, statErr := os.Stat(storedPath); statErr == nil {
		t.Fatalf("did not expect object to be stored after adapter crash: %s", storedPath)
	}

	lower := strings.ToLower(out)
	if !strings.Contains(lower, "error") && !strings.Contains(lower, "exit status") {
		t.Fatalf("expected explicit transfer failure output, got:\n%s", out)
	}
}

func TestGitLFSCustomTransferFailsWhenAdapterDoesNotRespond(t *testing.T) {
	s := setupRepositoryForUpload(t)

	adapterScript := writeExecutableScript(t, `#!/usr/bin/env bash
set -euo pipefail

IFS= read -r _ || exit 1
echo '{}'

IFS= read -r _ || exit 1
sleep 2
exit 0
`)
	configureScriptCustomTransfer(t, s.repoPath, s.env, s.gitBin, adapterScript)

	// Avoid long hangs if the adapter never responds to the transfer request.
	mustRun(t, s.repoPath, s.env, s.gitBin, "config", "lfs.activitytimeout", "1")
	out, err := runCmd(s.repoPath, s.env, s.gitBin, "push", "origin", "main")
	if err == nil {
		t.Fatalf("expected git push to fail when adapter stalls, output:\n%s", out)
	}

	lower := strings.ToLower(out)
	if !strings.Contains(lower, "error") && !strings.Contains(lower, "timeout") {
		t.Fatalf("expected timeout/failure output, got:\n%s", out)
	}
}
