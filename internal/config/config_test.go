package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvTrim(t *testing.T) {
	t.Setenv("TEST_TRIM_VAR", "  hello  ")
	if got := EnvTrim("TEST_TRIM_VAR"); got != "hello" {
		t.Fatalf("EnvTrim = %q, want %q", got, "hello")
	}
	if got := EnvTrim("NONEXISTENT_VAR_XYZ"); got != "" {
		t.Fatalf("EnvTrim(missing) = %q, want empty", got)
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Setenv("TEST_OR_DEFAULT", "value")
	if got := EnvOrDefault("TEST_OR_DEFAULT", "fallback"); got != "value" {
		t.Fatalf("got %q, want %q", got, "value")
	}
	if got := EnvOrDefault("NONEXISTENT_VAR_XYZ", "fallback"); got != "fallback" {
		t.Fatalf("got %q, want %q", got, "fallback")
	}
}

func TestEnvBoolOrDefault(t *testing.T) {
	t.Setenv("TEST_BOOL_TRUE", "true")
	t.Setenv("TEST_BOOL_FALSE", "false")
	t.Setenv("TEST_BOOL_INVALID", "notabool")

	if !EnvBoolOrDefault("TEST_BOOL_TRUE", false) {
		t.Fatal("expected true")
	}
	if EnvBoolOrDefault("TEST_BOOL_FALSE", true) {
		t.Fatal("expected false")
	}
	if !EnvBoolOrDefault("TEST_BOOL_INVALID", true) {
		t.Fatal("invalid should return fallback true")
	}
	if EnvBoolOrDefault("NONEXISTENT_VAR_XYZ", false) {
		t.Fatal("missing should return fallback false")
	}
}

func TestAppDirPath(t *testing.T) {
	p, err := AppDirPath()
	if err != nil || p == "" {
		t.Fatalf("AppDirPath() = %q, %v", p, err)
	}
}

func TestAppDirPathWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	if p, err := AppDirPath(); err == nil {
		t.Fatalf("AppDirPath() without HOME = %q, want error", p)
	}
	if p, err := StatusFilePath(); err == nil {
		t.Fatalf("StatusFilePath() without HOME = %q, want error", p)
	}
	if err := WriteStatus(StatusReport{State: StateOK}); err == nil {
		t.Fatal("WriteStatus without HOME must fail instead of writing into the working directory")
	}
}

func TestStatusFilePath_Default(t *testing.T) {
	t.Setenv(EnvStatusFile, "")
	p, err := StatusFilePath()
	if err != nil || p == "" {
		t.Fatalf("StatusFilePath() = %q, %v", p, err)
	}
}

func TestStatusFilePath_EnvOverride(t *testing.T) {
	t.Setenv(EnvStatusFile, "/tmp/test-status.json")
	if got, err := StatusFilePath(); err != nil || got != "/tmp/test-status.json" {
		t.Fatalf("got %q, %v, want /tmp/test-status.json", got, err)
	}
}

func TestPrefsFilePath(t *testing.T) {
	p, err := PrefsFilePath()
	if err != nil || p == "" {
		t.Fatalf("PrefsFilePath() = %q, %v", p, err)
	}
}

func TestConstants(t *testing.T) {
	if BackendLocal != "local" {
		t.Fatalf("BackendLocal = %q", BackendLocal)
	}
	if BackendSDK != "sdk" {
		t.Fatalf("BackendSDK = %q", BackendSDK)
	}
	if DefaultDriveCLIBin != "submodules/proton-drive-cli/dist/index.js" {
		t.Fatalf("DefaultDriveCLIBin = %q", DefaultDriveCLIBin)
	}
	if DefaultDriveCLIAppVersion != "external-drive-protonlfscli@0.1.2" {
		t.Fatalf("DefaultDriveCLIAppVersion = %q", DefaultDriveCLIAppVersion)
	}
	if EnvDriveCLIBin != "PROTON_DRIVE_CLI_BIN" {
		t.Fatalf("EnvDriveCLIBin = %q", EnvDriveCLIBin)
	}
}

func TestStatusRoundTrip(t *testing.T) {
	tmpFile := t.TempDir() + "/status.json"
	t.Setenv(EnvStatusFile, tmpFile)

	report := StatusReport{
		State:   StateOK,
		LastOID: "abc123",
		LastOp:  "upload",
	}
	if err := WriteStatus(report); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}

	got, err := ReadStatus()
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if got.State != StateOK {
		t.Fatalf("State = %q, want %q", got.State, StateOK)
	}
	if got.LastOID != "abc123" {
		t.Fatalf("LastOID = %q", got.LastOID)
	}
	if got.LastOp != "upload" {
		t.Fatalf("LastOp = %q", got.LastOp)
	}
	if got.Timestamp.IsZero() {
		t.Fatal("Timestamp should be auto-filled")
	}
}

func TestStatusReadMissing(t *testing.T) {
	t.Setenv(EnvStatusFile, t.TempDir()+"/nonexistent.json")
	_, err := ReadStatus()
	if err == nil {
		t.Fatal("expected error reading nonexistent status file")
	}
}

func TestStatusErrorState(t *testing.T) {
	tmpFile := t.TempDir() + "/status.json"
	t.Setenv(EnvStatusFile, tmpFile)

	report := StatusReport{
		State: StateError,
		Error: "something broke",
	}
	if err := WriteStatus(report); err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}
	got, err := ReadStatus()
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if got.State != StateError || got.Error != "something broke" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestPrefsRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	prefs := Preferences{CredentialStore: CredentialStoreGitCredential}
	if err := SavePrefs(prefs); err != nil {
		t.Fatalf("SavePrefs: %v", err)
	}

	got := LoadPrefs()
	if got.CredentialStore != CredentialStoreGitCredential {
		t.Fatalf("CredentialStore = %q", got.CredentialStore)
	}
}

func TestPrefsLoadMissingReturnsDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got := LoadPrefs()
	defaults := DefaultPreferences()
	if got.CredentialStore != defaults.CredentialStore {
		t.Fatalf("got %q, want default %q", got.CredentialStore, defaults.CredentialStore)
	}
}

func TestPrefsLoadCorruptReturnsDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	configDir := tmpDir + "/" + AppDir
	_ = os.MkdirAll(configDir, 0o700)
	_ = os.WriteFile(configDir+"/"+ConfigFileName, []byte("{invalid json"), 0o600)

	got := LoadPrefs()
	if got.CredentialStore != DefaultCredentialStore {
		t.Fatalf("corrupt file should return defaults, got %q", got.CredentialStore)
	}
}

func TestPrefsLoadWithoutCredentialStoreUsesDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, AppDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(`{"credentialStore":""}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got := LoadPrefs()
	if got.CredentialStore != DefaultCredentialStore {
		t.Fatalf("LoadPrefs() = %+v, want the default credential store", got)
	}
}
