package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportPassEnvEmitsOnlyProviderEnv(t *testing.T) {
	fakePass := filepath.Join(t.TempDir(), "fake-pass-cli.sh")
	if err := os.WriteFile(fakePass, []byte(`#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "test" ]]; then
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
	for _, want := range []string{
		"export PROTON_PASS_CLI_BIN=",
		"unset PROTON_PASS_REF_ROOT",
		"unset PROTON_PASS_USERNAME_REF",
		"unset PROTON_PASS_PASSWORD_REF",
		"unset PROTON_USERNAME",
		"unset PROTON_PASSWORD",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output:\n%s", want, out)
		}
	}
	for _, forbidden := range []string{
		"export PROTON_PASS_USERNAME_REF",
		"export PROTON_PASS_PASSWORD_REF",
		"pass://Personal",
	} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("export-pass-env must not emit %q:\n%s", forbidden, out)
		}
	}
}

func TestExportPassEnvRejectsLegacyAccountRefs(t *testing.T) {
	out, err := runExportPassEnv(t, "--username-ref", "pass://Personal/Proton/username", "--skip-check")
	if err == nil {
		t.Fatalf("expected legacy username ref to fail:\n%s", out)
	}
	if !strings.Contains(out, "Account credential reference options were removed") {
		t.Fatalf("expected removed account ref error, got:\n%s", out)
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
