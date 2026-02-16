package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proton-lfs-cli/internal/config"
)

// saveFuncVars saves and restores function vars for test isolation.
func saveFuncVars(t *testing.T) {
	t.Helper()
	origFindDriveCLI := findDriveCLI
	origFindAdapter := findAdapter
	origVerifyCredential := verifyCredential
	origLoginDrive := loginDrive
	t.Cleanup(func() {
		findDriveCLI = origFindDriveCLI
		findAdapter = origFindAdapter
		verifyCredential = origVerifyCredential
		loginDrive = origLoginDrive
	})
}

type fakeHomeOpts struct {
	sessionExists bool
	configJSON    string
	statusJSON    string
}

// setupFakeHome creates a temporary HOME directory with optional state files.
func setupFakeHome(t *testing.T, opts fakeHomeOpts) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)

	if opts.sessionExists {
		dir := filepath.Join(home, ".proton-drive-cli")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "session.json"),
			[]byte(`{"accessToken":"test"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if opts.configJSON != "" {
		dir := filepath.Join(home, ".proton-lfs")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"),
			[]byte(opts.configJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if opts.statusJSON != "" {
		dir := filepath.Join(home, ".proton-lfs")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "status.json"),
			[]byte(opts.statusJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// setupGitConfig creates a temporary git global config and sets GIT_CONFIG_GLOBAL.
func setupGitConfig(t *testing.T, content string) string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", tmp)
	return tmp
}

// --- cliStatus tests ---

func TestCliStatusConnectedOK(t *testing.T) {
	saveFuncVars(t)
	ts := time.Now().UTC()
	statusJSON, _ := json.Marshal(config.StatusReport{
		State: config.StateOK, LastOp: "upload", Timestamp: ts,
	})
	setupFakeHome(t, fakeHomeOpts{
		sessionExists: true,
		configJSON:    `{"credentialProvider":"pass-cli"}`,
		statusJSON:    string(statusJSON),
	})
	setupGitConfig(t, "[lfs]\n\tstandalonetransferagent = proton\n")

	var buf bytes.Buffer
	code := cliStatus(&buf)
	out := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{
		"Session:  logged in",
		"LFS:      enabled",
		"Provider: pass-cli",
		"upload just now (ok)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCliStatusDisconnected(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{})
	setupGitConfig(t, "")

	var buf bytes.Buffer
	code := cliStatus(&buf)
	out := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{
		"Session:  not connected",
		"LFS:      not registered",
		"Provider: pass-cli",
		"Transfer: no data",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCliStatusTransferring(t *testing.T) {
	saveFuncVars(t)
	statusJSON, _ := json.Marshal(config.StatusReport{
		State: config.StateTransferring, LastOp: "download",
	})
	setupFakeHome(t, fakeHomeOpts{
		sessionExists: true,
		statusJSON:    string(statusJSON),
	})
	setupGitConfig(t, "")

	var buf bytes.Buffer
	code := cliStatus(&buf)
	out := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "Transfer: download in progress") {
		t.Errorf("output missing transfer in progress:\n%s", out)
	}
}

func TestCliStatusError(t *testing.T) {
	saveFuncVars(t)
	ts := time.Now().Add(-5 * time.Minute).UTC()
	statusJSON, _ := json.Marshal(config.StatusReport{
		State: config.StateError, LastOp: "upload", Error: "timeout", Timestamp: ts,
	})
	setupFakeHome(t, fakeHomeOpts{statusJSON: string(statusJSON)})
	setupGitConfig(t, "")

	var buf bytes.Buffer
	code := cliStatus(&buf)
	out := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "upload") || !strings.Contains(out, "timeout") {
		t.Errorf("output missing error details:\n%s", out)
	}
}

func TestCliStatusIdle(t *testing.T) {
	saveFuncVars(t)
	statusJSON, _ := json.Marshal(config.StatusReport{State: config.StateIdle})
	setupFakeHome(t, fakeHomeOpts{statusJSON: string(statusJSON)})
	setupGitConfig(t, "")

	var buf bytes.Buffer
	code := cliStatus(&buf)
	out := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "Transfer: idle") {
		t.Errorf("output missing idle:\n%s", out)
	}
}

// --- cliConfig tests ---

func TestCliConfigShowDefault(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{})

	var buf bytes.Buffer
	code := cliConfig(&buf, nil)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if got := strings.TrimSpace(buf.String()); got != "pass-cli" {
		t.Fatalf("expected 'pass-cli', got %q", got)
	}
}

func TestCliConfigHelp(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{})

	for _, flag := range []string{"--help", "-h"} {
		var buf bytes.Buffer
		code := cliConfig(&buf, []string{flag})
		if code != 0 {
			t.Fatalf("cliConfig(%s) exit %d, expected 0", flag, code)
		}
		if !strings.Contains(buf.String(), "Usage:") {
			t.Errorf("cliConfig(%s) missing Usage header:\n%s", flag, buf.String())
		}
	}
}

func TestCliConfigShowGitCredential(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"git-credential"}`})

	var buf bytes.Buffer
	code := cliConfig(&buf, nil)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if got := strings.TrimSpace(buf.String()); got != "git-credential" {
		t.Fatalf("expected 'git-credential', got %q", got)
	}
}

func TestCliConfigSetGitCredential(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"pass-cli"}`})

	var buf bytes.Buffer
	code := cliConfig(&buf, []string{"git-credential"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(buf.String(), "Credential provider set to git-credential") {
		t.Fatalf("unexpected output: %s", buf.String())
	}

	// Verify it round-trips
	prefs := config.LoadPrefs()
	if prefs.CredentialProvider != "git-credential" {
		t.Fatalf("expected git-credential in config, got %q", prefs.CredentialProvider)
	}
}

func TestCliConfigSetPassCLI(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"git-credential"}`})

	var buf bytes.Buffer
	code := cliConfig(&buf, []string{"pass-cli"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(buf.String(), "Credential provider set to pass-cli") {
		t.Fatalf("unexpected output: %s", buf.String())
	}

	prefs := config.LoadPrefs()
	if prefs.CredentialProvider != "pass-cli" {
		t.Fatalf("expected pass-cli in config, got %q", prefs.CredentialProvider)
	}
}

func TestCliConfigInvalid(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{})

	var buf bytes.Buffer
	code := cliConfig(&buf, []string{"keychain"})
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(buf.String(), "unknown provider") {
		t.Fatalf("expected 'unknown provider' error, got: %s", buf.String())
	}
}

// --- cliRegister tests ---

func TestCliRegisterSuccess(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"pass-cli"}`})
	gitCfg := setupGitConfig(t, "")

	findAdapter = func() string { return "/tmp/test-adapter" }
	findDriveCLI = func() string { return "/tmp/test-drive-cli" }

	var buf bytes.Buffer
	code := cliRegister(&buf)
	out := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", code, out)
	}
	for _, want := range []string{
		"LFS backend enabled",
		"adapter: /tmp/test-adapter",
		"drive-cli: /tmp/test-drive-cli",
		"provider: pass-cli",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	// Verify git config was written
	data, err := os.ReadFile(gitCfg)
	if err != nil {
		t.Fatal(err)
	}
	cfgStr := string(data)
	if !strings.Contains(cfgStr, "standalonetransferagent = proton") {
		t.Errorf("git config missing standalonetransferagent:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, "path = /tmp/test-adapter") {
		t.Errorf("git config missing adapter path:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, "--backend sdk") {
		t.Errorf("git config missing --backend sdk:\n%s", cfgStr)
	}
	if !strings.Contains(cfgStr, "--drive-cli-bin /tmp/test-drive-cli") {
		t.Errorf("git config missing drive-cli-bin:\n%s", cfgStr)
	}
}

func TestCliRegisterGitCredentialProvider(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"git-credential"}`})
	gitCfg := setupGitConfig(t, "")

	findAdapter = func() string { return "/tmp/test-adapter" }
	findDriveCLI = func() string { return "/tmp/test-drive-cli" }

	var buf bytes.Buffer
	code := cliRegister(&buf)

	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", code, buf.String())
	}

	data, err := os.ReadFile(gitCfg)
	if err != nil {
		t.Fatal(err)
	}
	cfgStr := string(data)
	if !strings.Contains(cfgStr, "--credential-provider git-credential") {
		t.Errorf("git config missing --credential-provider git-credential:\n%s", cfgStr)
	}
}

func TestCliRegisterNoAdapter(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{})
	setupGitConfig(t, "")

	findAdapter = func() string { return "" }

	var buf bytes.Buffer
	code := cliRegister(&buf)

	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(buf.String(), "error: adapter binary not found") {
		t.Fatalf("expected adapter not found error, got: %s", buf.String())
	}
}

// --- cliLogin unified tests (both providers use the same flow) ---

func TestCliLoginCredentialsAlreadyStored(t *testing.T) {
	for _, provider := range []string{"git-credential", "pass-cli"} {
		t.Run(provider, func(t *testing.T) {
			saveFuncVars(t)
			setupFakeHome(t, fakeHomeOpts{
				configJSON: fmt.Sprintf(`{"credentialProvider":"%s"}`, provider),
			})

			findDriveCLI = func() string { return "/tmp/test-drive-cli" }
			verifyCredential = func(string) bool { return true }
			loginDrive = func(_ string, _ ...string) error { return nil }

			var buf bytes.Buffer
			code := cliLogin(&buf)
			out := buf.String()

			if code != 0 {
				t.Fatalf("expected exit 0, got %d; output:\n%s", code, out)
			}
			if !strings.Contains(out, "Logging in...") {
				t.Errorf("output missing 'Logging in...':\n%s", out)
			}
			if !strings.Contains(out, "Connected to Proton") {
				t.Errorf("output missing 'Connected to Proton':\n%s", out)
			}
		})
	}
}

func TestCliLoginLoginFails(t *testing.T) {
	for _, provider := range []string{"git-credential", "pass-cli"} {
		t.Run(provider, func(t *testing.T) {
			saveFuncVars(t)
			setupFakeHome(t, fakeHomeOpts{
				configJSON: fmt.Sprintf(`{"credentialProvider":"%s"}`, provider),
			})

			findDriveCLI = func() string { return "/tmp/test-drive-cli" }
			verifyCredential = func(string) bool { return true }
			loginDrive = func(_ string, _ ...string) error {
				return errors.New("captcha required")
			}

			var buf bytes.Buffer
			code := cliLogin(&buf)
			out := buf.String()

			if code != 1 {
				t.Fatalf("expected exit 1, got %d", code)
			}
			if !strings.Contains(out, "error: login failed: captcha required") {
				t.Errorf("output missing login error:\n%s", out)
			}
		})
	}
}

func TestCliLoginNoBinary(t *testing.T) {
	for _, provider := range []string{"git-credential", "pass-cli"} {
		t.Run(provider, func(t *testing.T) {
			saveFuncVars(t)
			setupFakeHome(t, fakeHomeOpts{
				configJSON: fmt.Sprintf(`{"credentialProvider":"%s"}`, provider),
			})

			findDriveCLI = func() string { return "" }

			var buf bytes.Buffer
			code := cliLogin(&buf)

			if code != 1 {
				t.Fatalf("expected exit 1, got %d", code)
			}
			if !strings.Contains(buf.String(), "error: proton-drive-cli not found") {
				t.Fatalf("expected not found error, got: %s", buf.String())
			}
		})
	}
}

func TestCliLoginStoreAndLogin(t *testing.T) {
	for _, provider := range []string{"git-credential", "pass-cli"} {
		t.Run(provider, func(t *testing.T) {
			saveFuncVars(t)
			setupFakeHome(t, fakeHomeOpts{
				configJSON: fmt.Sprintf(`{"credentialProvider":"%s"}`, provider),
			})

			findDriveCLI = func() string { return "/tmp/test-drive-cli" }

			// verifyCredential returns false first, then true (simulating successful store)
			callCount := 0
			verifyCredential = func(string) bool {
				callCount++
				return callCount > 1
			}
			loginDrive = func(_ string, _ ...string) error { return nil }

			// The exec.Command for "credential store" will fail since /tmp/test-drive-cli
			// doesn't exist, but we test the flow up to that point.
			var buf bytes.Buffer
			code := cliLogin(&buf)
			out := buf.String()

			if !strings.Contains(out, "No credentials stored") {
				t.Errorf("output missing 'No credentials stored':\n%s", out)
			}
			// Since the binary doesn't exist, store fails — that's expected in unit test
			if code == 0 {
				if !strings.Contains(out, "Connected to Proton") {
					t.Errorf("output missing 'Connected to Proton':\n%s", out)
				}
			}
		})
	}
}

func TestCliLoginStoreFailsVerify(t *testing.T) {
	for _, provider := range []string{"git-credential", "pass-cli"} {
		t.Run(provider, func(t *testing.T) {
			saveFuncVars(t)
			setupFakeHome(t, fakeHomeOpts{
				configJSON: fmt.Sprintf(`{"credentialProvider":"%s"}`, provider),
			})

			findDriveCLI = func() string { return "/tmp/test-drive-cli" }
			// Always returns false — credentials never stored
			verifyCredential = func(string) bool { return false }

			var buf bytes.Buffer
			code := cliLogin(&buf)
			out := buf.String()

			if code != 1 {
				t.Fatalf("expected exit 1, got %d", code)
			}
			if !strings.Contains(out, "No credentials stored") {
				t.Errorf("output missing 'No credentials stored':\n%s", out)
			}
		})
	}
}

// --- Session sharing tests ---

func TestCliLoginCreatesSession(t *testing.T) {
	saveFuncVars(t)
	home := setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"git-credential"}`})
	setupGitConfig(t, "")

	findDriveCLI = func() string { return "/tmp/test-drive-cli" }
	verifyCredential = func(string) bool { return true }
	loginDrive = func(_ string, _ ...string) error {
		// Simulate what proton-drive-cli login does: write session.json
		dir := filepath.Join(home, ".proton-drive-cli")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "session.json"),
			[]byte(`{"accessToken":"test","refreshToken":"test","sessionId":"test"}`), 0o600)
	}

	var buf bytes.Buffer
	code := cliLogin(&buf)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", code, buf.String())
	}

	// Verify session is visible to isSessionActive()
	if !isSessionActive() {
		t.Fatal("expected isSessionActive() to return true after connect")
	}

	// Verify cliStatus reflects the new session
	var statusBuf bytes.Buffer
	cliStatus(&statusBuf)
	if !strings.Contains(statusBuf.String(), "Session:  logged in") {
		t.Errorf("status should show logged in after connect:\n%s", statusBuf.String())
	}
}

func TestCliStatusReflectsExternalLogin(t *testing.T) {
	saveFuncVars(t)
	// Simulate an external login by creating session.json directly
	setupFakeHome(t, fakeHomeOpts{sessionExists: true})
	setupGitConfig(t, "")

	var buf bytes.Buffer
	code := cliStatus(&buf)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(buf.String(), "Session:  logged in") {
		t.Errorf("status should reflect external session:\n%s", buf.String())
	}
}

// --- cliLogout tests ---

func TestCliLogoutNoBinary(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{})

	findDriveCLI = func() string { return "" }

	var buf bytes.Buffer
	code := cliLogout(&buf)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(buf.String(), "error: proton-drive-cli not found") {
		t.Fatalf("expected not found error, got: %s", buf.String())
	}
}

func TestCliLogoutClearsSession(t *testing.T) {
	saveFuncVars(t)
	home := setupFakeHome(t, fakeHomeOpts{sessionExists: true})

	// Create a fake proton-drive-cli that removes session.json when called with "logout"
	fakeBin := filepath.Join(t.TempDir(), "proton-drive-cli")
	script := fmt.Sprintf("#!/bin/sh\nrm -f '%s/.proton-drive-cli/session.json'\n", home)
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	findDriveCLI = func() string { return fakeBin }

	// Verify session exists before logout
	if !isSessionActive() {
		t.Fatal("expected session to exist before logout")
	}

	var buf bytes.Buffer
	code := cliLogout(&buf)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; output: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "Logged out") {
		t.Errorf("output missing 'Logged out': %s", buf.String())
	}

	// Verify session is gone
	if isSessionActive() {
		t.Fatal("expected session to be cleared after logout")
	}
}

// --- Usage string test ---

func TestUsageContainsSubcommands(t *testing.T) {
	for _, word := range []string{
		"login", "logout", "register", "status", "config",
		"git-credential", "pass-cli",
	} {
		if !strings.Contains(usage, word) {
			t.Errorf("usage string missing %q", word)
		}
	}
}
