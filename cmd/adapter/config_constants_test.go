package main

import "testing"

func TestDefaultConstants(t *testing.T) {
	if DefaultDriveCLIBin != "submodules/proton-drive-cli/dist/index.js" {
		t.Fatalf("unexpected DefaultDriveCLIBin: %q", DefaultDriveCLIBin)
	}
	if DefaultStorageBase != "LFS" {
		t.Fatalf("unexpected DefaultStorageBase: %q", DefaultStorageBase)
	}
	if DefaultDataCredentialHost != "proton-data.proton-lfs-cli.local" {
		t.Fatalf("unexpected DefaultDataCredentialHost: %q", DefaultDataCredentialHost)
	}
}

func TestEnvVarNames(t *testing.T) {
	if EnvDriveCLIBin != "PROTON_DRIVE_CLI_BIN" {
		t.Fatalf("unexpected EnvDriveCLIBin: %q", EnvDriveCLIBin)
	}
	if EnvNodeBin != "NODE_BIN" {
		t.Fatalf("unexpected EnvNodeBin: %q", EnvNodeBin)
	}
	if EnvStorageBase != "LFS_STORAGE_BASE" {
		t.Fatalf("unexpected EnvStorageBase: %q", EnvStorageBase)
	}
	if EnvAppVersion != "PROTON_APP_VERSION" {
		t.Fatalf("unexpected EnvAppVersion: %q", EnvAppVersion)
	}
	if EnvDataCredentialProvider != "PROTON_DATA_CREDENTIAL_PROVIDER" {
		t.Fatalf("unexpected EnvDataCredentialProvider: %q", EnvDataCredentialProvider)
	}
	if EnvDataCredentialHost != "PROTON_DATA_CREDENTIAL_HOST" {
		t.Fatalf("unexpected EnvDataCredentialHost: %q", EnvDataCredentialHost)
	}
}

func TestEnvBoolOrDefault(t *testing.T) {
	// When env is not set, should return fallback
	if envBoolOrDefault("NONEXISTENT_TEST_VAR_12345", true) != true {
		t.Fatal("expected true fallback")
	}
	if envBoolOrDefault("NONEXISTENT_TEST_VAR_12345", false) != false {
		t.Fatal("expected false fallback")
	}
}
