//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	defaultPassRefRoot = "pass://Personal/Proton Git LFS"
)

func sdkPassCLIPath() string {
	passCLIBin := strings.TrimSpace(os.Getenv("PROTON_PASS_CLI_BIN"))
	if passCLIBin == "" {
		passCLIBin = "pass-cli"
	}
	return passCLIBin
}

func sdkPassRefConfig() (passRefRoot, usernameRef, passwordRef string) {
	passRefRoot = strings.TrimRight(strings.TrimSpace(os.Getenv("PROTON_PASS_REF_ROOT")), "/")
	if passRefRoot == "" {
		passRefRoot = defaultPassRefRoot
	}

	usernameRef = strings.TrimSpace(os.Getenv("PROTON_PASS_USERNAME_REF"))
	if usernameRef == "" {
		usernameRef = passRefRoot + "/username"
	}
	passwordRef = strings.TrimSpace(os.Getenv("PROTON_PASS_PASSWORD_REF"))
	if passwordRef == "" {
		passwordRef = passRefRoot + "/password"
	}

	return passRefRoot, usernameRef, passwordRef
}

func sdkCredentialEnv(t *testing.T, base []string) []string {
	t.Helper()

	passCLIBin := sdkPassCLIPath()
	if strings.Contains(passCLIBin, string(os.PathSeparator)) {
		if _, err := os.Stat(passCLIBin); err != nil {
			t.Skipf("sdk integration test skipped: PROTON_PASS_CLI_BIN=%s is not usable: %v", passCLIBin, err)
		}
	} else if _, err := exec.LookPath(passCLIBin); err != nil {
		t.Skipf("sdk integration test skipped: pass-cli binary not found: %s", passCLIBin)
	}

	passRefRoot, usernameRef, passwordRef := sdkPassRefConfig()

	return append(
		base,
		"PROTON_PASS_CLI_BIN="+passCLIBin,
		"PROTON_PASS_REF_ROOT="+passRefRoot,
		"PROTON_PASS_USERNAME_REF="+usernameRef,
		"PROTON_PASS_PASSWORD_REF="+passwordRef,
	)
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

func parsePassCLISecret(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		if value := jsonStringValue(decoded); value != "" {
			return value
		}
	}

	lines := strings.Split(trimmed, "\n")
	nonEmpty := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			if key == "value" || key == "secret" {
				line = strings.TrimSpace(parts[1])
			}
		}
		nonEmpty = append(nonEmpty, line)
	}

	if len(nonEmpty) == 0 {
		return ""
	}
	if len(nonEmpty) == 1 {
		return strings.TrimSpace(nonEmpty[0])
	}
	return strings.TrimSpace(nonEmpty[len(nonEmpty)-1])
}

func jsonStringValue(v any) string {
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"value", "secret", "content", "data", "text"} {
			if raw, ok := typed[key]; ok {
				if value := jsonStringValue(raw); value != "" {
					return value
				}
			}
		}
	}
	return ""
}

func sdkReadPassCLISecret(t *testing.T, passCLIBin, reference string) string {
	t.Helper()

	cmd := exec.Command(passCLIBin, "item", "view", "--output", "json", reference)
	out, err := cmd.CombinedOutput()
	if err == nil {
		if value := parsePassCLISecret(string(out)); value != "" {
			return value
		}
	}

	cmd = exec.Command(passCLIBin, "item", "view", reference)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Skipf("sdk integration test skipped: unable to resolve pass reference %q: %v [%s]", reference, err, strings.TrimSpace(string(out)))
	}
	value := parsePassCLISecret(string(out))
	if value == "" {
		t.Skipf("sdk integration test skipped: empty secret value for pass reference %q", reference)
	}
	return value
}

func sdkResolvedCredentials(t *testing.T) (string, string) {
	t.Helper()

	passCLIBin := sdkPassCLIPath()
	if strings.Contains(passCLIBin, string(os.PathSeparator)) {
		if _, err := os.Stat(passCLIBin); err != nil {
			t.Skipf("sdk integration test skipped: PROTON_PASS_CLI_BIN=%s is not usable: %v", passCLIBin, err)
		}
	} else if _, err := exec.LookPath(passCLIBin); err != nil {
		t.Skipf("sdk integration test skipped: pass-cli binary not found: %s", passCLIBin)
	}

	_, usernameRef, passwordRef := sdkPassRefConfig()
	username := sdkReadPassCLISecret(t, passCLIBin, usernameRef)
	password := sdkReadPassCLISecret(t, passCLIBin, passwordRef)
	return username, password
}

func TestGitLFSCustomTransferSDKBackendRoundTrip(t *testing.T) {
	s := setupRepositoryForUpload(t)

	driveCliBin := sdkDriveCliBin(t, s.root)
	sdkEnv := sdkCredentialEnv(t, s.env)

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
