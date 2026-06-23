package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"proton-lfs-cli/internal/config"
)

func saveFuncVars(t *testing.T) {
	t.Helper()
	origFindDriveCLI := findDriveCLI
	origFindAdapter := findAdapter
	origLoginDrive := loginDrive
	t.Cleanup(func() {
		findDriveCLI = origFindDriveCLI
		findAdapter = origFindAdapter
		loginDrive = origLoginDrive
	})
}

type fakeHomeOpts struct {
	sessionExists bool
	configJSON    string
	statusJSON    string
}

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

func setupGitConfig(t *testing.T, content string) string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", tmp)
	return tmp
}

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

func TestCliStatusErrorShowsRetryMetadata(t *testing.T) {
	saveFuncVars(t)
	ts := time.Now().Add(-2 * time.Minute).UTC()
	statusJSON, _ := json.Marshal(config.StatusReport{
		State:     config.StateError,
		LastOp:    "download",
		Error:     "drive service is unavailable",
		ErrorCode: "server_error",
		Retryable: true,
		Temporary: true,
		Timestamp: ts,
	})
	setupFakeHome(t, fakeHomeOpts{statusJSON: string(statusJSON)})
	setupGitConfig(t, "")

	var buf bytes.Buffer
	code := cliStatus(&buf)
	out := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	for _, want := range []string{
		"download",
		"drive service is unavailable",
		"code=server_error",
		"retryable",
		"temporary",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCliStatusAuthBlockerIsNotRetryable(t *testing.T) {
	saveFuncVars(t)
	statusJSON, _ := json.Marshal(config.StatusReport{
		State:     config.StateAuthRequired,
		LastOp:    "upload",
		Error:     "stored browser-fork key password required",
		ErrorCode: "key_password_required",
	})
	setupFakeHome(t, fakeHomeOpts{statusJSON: string(statusJSON)})
	setupGitConfig(t, "")

	var buf bytes.Buffer
	code := cliStatus(&buf)
	out := buf.String()

	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out, "stored browser-fork key password required") ||
		!strings.Contains(out, "code=key_password_required") {
		t.Errorf("output missing auth blocker details:\n%s", out)
	}
	if strings.Contains(out, "retryable") || strings.Contains(out, "temporary") {
		t.Errorf("auth blocker must not show retry metadata:\n%s", out)
	}
}

func TestCliConfigShowAndSetProvider(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"pass-cli"}`})

	var show bytes.Buffer
	if code := cliConfig(&show, nil); code != 0 {
		t.Fatalf("show config exit %d", code)
	}
	if got := strings.TrimSpace(show.String()); got != "pass-cli" {
		t.Fatalf("expected pass-cli, got %q", got)
	}

	var set bytes.Buffer
	if code := cliConfig(&set, []string{"git-credential"}); code != 0 {
		t.Fatalf("set config exit %d", code)
	}
	if !strings.Contains(set.String(), "Key-password provider set to git-credential") {
		t.Fatalf("unexpected output: %s", set.String())
	}
	if prefs := config.LoadPrefs(); prefs.CredentialProvider != "git-credential" {
		t.Fatalf("expected git-credential in config, got %q", prefs.CredentialProvider)
	}
}

func TestCliConfigHelpAndInvalid(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{})

	var help bytes.Buffer
	if code := cliConfig(&help, []string{"--help"}); code != 0 {
		t.Fatalf("help exit %d", code)
	}
	if !strings.Contains(help.String(), "browser-fork key-password provider") {
		t.Fatalf("help should describe key-password provider:\n%s", help.String())
	}

	var invalid bytes.Buffer
	if code := cliConfig(&invalid, []string{"keychain"}); code != 1 {
		t.Fatalf("invalid provider exit %d, want 1", code)
	}
	if !strings.Contains(invalid.String(), "unknown provider") {
		t.Fatalf("expected unknown provider error, got: %s", invalid.String())
	}
}

func TestCliRegisterSuccessDoesNotWriteAccountCredentialProvider(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"git-credential"}`})
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
		"provider: git-credential",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	data, err := os.ReadFile(gitCfg)
	if err != nil {
		t.Fatal(err)
	}
	cfgStr := string(data)
	for _, want := range []string{
		"standalonetransferagent = proton",
		"path = /tmp/test-adapter",
		"--backend sdk",
		"--drive-cli-bin /tmp/test-drive-cli",
	} {
		if !strings.Contains(cfgStr, want) {
			t.Errorf("git config missing %q:\n%s", want, cfgStr)
		}
	}
	if strings.Contains(cfgStr, "--credential-provider") {
		t.Fatalf("git config must not include account credential provider:\n%s", cfgStr)
	}
}

func TestCliRegisterQuotesDriveCLIPath(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"pass-cli"}`})
	gitCfg := setupGitConfig(t, "")

	findAdapter = func() string { return "/tmp/test-adapter" }
	findDriveCLI = func() string { return "/Applications/Proton Drive CLI/proton-drive-cli" }

	var buf bytes.Buffer
	if code := cliRegister(&buf); code != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", code, buf.String())
	}

	data, err := os.ReadFile(gitCfg)
	if err != nil {
		t.Fatal(err)
	}
	want := "--drive-cli-bin '/Applications/Proton Drive CLI/proton-drive-cli'"
	if !strings.Contains(string(data), want) {
		t.Errorf("git config missing quoted drive-cli path %q:\n%s", want, data)
	}
}

func TestCliRegisterNoAdapter(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{})
	setupGitConfig(t, "")

	findAdapter = func() string { return "" }

	var buf bytes.Buffer
	if code := cliRegister(&buf); code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(buf.String(), "error: adapter binary not found") {
		t.Fatalf("expected adapter not found error, got: %s", buf.String())
	}
}

func TestCliLoginUsesBrowserForkKeyPasswordProviderOnly(t *testing.T) {
	for _, provider := range []string{"git-credential", "pass-cli"} {
		t.Run(provider, func(t *testing.T) {
			saveFuncVars(t)
			setupFakeHome(t, fakeHomeOpts{
				configJSON: fmt.Sprintf(`{"credentialProvider":"%s"}`, provider),
			})

			findDriveCLI = func() string { return "/tmp/test-drive-cli" }
			var gotDriveCLI string
			var gotArgs []string
			loginDrive = func(driveCLI string, args ...string) error {
				gotDriveCLI = driveCLI
				gotArgs = append([]string(nil), args...)
				return nil
			}

			var buf bytes.Buffer
			code := cliLogin(&buf)
			out := buf.String()

			if code != 0 {
				t.Fatalf("expected exit 0, got %d; output:\n%s", code, out)
			}
			if gotDriveCLI != "/tmp/test-drive-cli" {
				t.Fatalf("drive cli = %q", gotDriveCLI)
			}
			wantArgs := []string{"--key-password-provider", provider}
			if !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Fatalf("login args = %#v, want %#v", gotArgs, wantArgs)
			}
			if !strings.Contains(out, "Connected to Proton") {
				t.Errorf("output missing success:\n%s", out)
			}
		})
	}
}

func TestCliLoginLoginFails(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"pass-cli"}`})

	findDriveCLI = func() string { return "/tmp/test-drive-cli" }
	loginDrive = func(_ string, _ ...string) error {
		return errors.New("captcha required")
	}

	var buf bytes.Buffer
	code := cliLogin(&buf)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(buf.String(), "error: login failed: captcha required") {
		t.Errorf("output missing login error:\n%s", buf.String())
	}
}

func TestCliLoginNoBinary(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"pass-cli"}`})

	findDriveCLI = func() string { return "" }

	var buf bytes.Buffer
	if code := cliLogin(&buf); code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(buf.String(), "error: proton-drive-cli not found") {
		t.Fatalf("expected not found error, got: %s", buf.String())
	}
}

func TestCliLoginCreatesSession(t *testing.T) {
	saveFuncVars(t)
	home := setupFakeHome(t, fakeHomeOpts{configJSON: `{"credentialProvider":"git-credential"}`})
	setupGitConfig(t, "")

	findDriveCLI = func() string { return "/tmp/test-drive-cli" }
	loginDrive = func(_ string, _ ...string) error {
		dir := filepath.Join(home, ".proton-drive-cli")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, "session.json"),
			[]byte(`{"accessToken":"test","refreshToken":"test","sessionId":"test"}`), 0o600)
	}

	var buf bytes.Buffer
	if code := cliLogin(&buf); code != 0 {
		t.Fatalf("expected exit 0, got %d; output:\n%s", code, buf.String())
	}

	if !isSessionActive() {
		t.Fatal("expected isSessionActive() to return true after connect")
	}

	var statusBuf bytes.Buffer
	cliStatus(&statusBuf)
	if !strings.Contains(statusBuf.String(), "Session:  logged in") {
		t.Errorf("status should show logged in after connect:\n%s", statusBuf.String())
	}
}

func TestCliStatusReflectsExternalLogin(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{sessionExists: true})
	setupGitConfig(t, "")

	var buf bytes.Buffer
	if code := cliStatus(&buf); code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(buf.String(), "Session:  logged in") {
		t.Errorf("status should reflect external session:\n%s", buf.String())
	}
}

func TestCliLogoutNoBinary(t *testing.T) {
	saveFuncVars(t)
	setupFakeHome(t, fakeHomeOpts{})

	findDriveCLI = func() string { return "" }

	var buf bytes.Buffer
	if code := cliLogout(&buf); code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(buf.String(), "error: proton-drive-cli not found") {
		t.Fatalf("expected not found error, got: %s", buf.String())
	}
}

func TestCliLogoutClearsSession(t *testing.T) {
	saveFuncVars(t)
	home := setupFakeHome(t, fakeHomeOpts{sessionExists: true})

	fakeBin := filepath.Join(t.TempDir(), "proton-drive-cli")
	script := fmt.Sprintf("#!/bin/sh\nrm -f '%s/.proton-drive-cli/session.json'\n", home)
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	findDriveCLI = func() string { return fakeBin }

	if !isSessionActive() {
		t.Fatal("expected session to exist before logout")
	}

	var buf bytes.Buffer
	if code := cliLogout(&buf); code != 0 {
		t.Fatalf("expected exit 0, got %d; output: %s", code, buf.String())
	}
	if !strings.Contains(buf.String(), "Logged out") {
		t.Errorf("output missing 'Logged out': %s", buf.String())
	}
	if isSessionActive() {
		t.Fatal("expected session to be cleared after logout")
	}
}

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
