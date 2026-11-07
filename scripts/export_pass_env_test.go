package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportPassEnvEmitsOnlyPassCLIBin(t *testing.T) {
	fakePass := filepath.Join(t.TempDir(), "fake-pass-cli.sh")
	if err := os.WriteFile(fakePass, []byte(`#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "info" ]]; then
  exit 0
fi
echo "unexpected pass-cli command: $*" >&2
exit 9
`), 0o700); err != nil {
		t.Fatalf("write fake pass-cli: %v", err)
	}

	out, err := runExportPassEnv(t, "--pass-cli", fakePass)
	if err != nil {
		t.Fatalf("export-pass-env failed: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "export PROTON_PASS_CLI_BIN=") || !strings.Contains(lines[0], "fake-pass-cli.sh") {
		t.Fatalf("export-pass-env must print only the PROTON_PASS_CLI_BIN export:\n%s", out)
	}
}

func TestExportPassEnvRejectsUnknownOption(t *testing.T) {
	out, err := runExportPassEnv(t, "--no-such-option", "--skip-check")
	if err == nil {
		t.Fatalf("expected unknown option to fail:\n%s", out)
	}
	if !strings.Contains(out, "Unknown argument: --no-such-option") {
		t.Fatalf("expected unknown argument error, got:\n%s", out)
	}
}

func TestExportPassEnvRejectsMissingPassCliValue(t *testing.T) {
	out, err := runExportPassEnv(t, "--pass-cli")
	if err == nil {
		t.Fatalf("expected missing pass-cli value to fail:\n%s", out)
	}
	if !strings.Contains(out, "--pass-cli requires a binary path") {
		t.Fatalf("expected missing pass-cli value error, got:\n%s", out)
	}
}

func runExportPassEnv(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmdArgs := append([]string{filepath.Join(repoRoot(t), "scripts", "export-pass-env.sh")}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
