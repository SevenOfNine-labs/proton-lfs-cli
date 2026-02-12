package main

import "testing"

func TestNormalizePassRefRoot(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "pass://Personal/Proton Git LFS", want: "pass://Personal/Proton Git LFS"},
		{in: "pass://Personal/Proton Git LFS/", want: "pass://Personal/Proton Git LFS"},
		{in: "  pass://Personal/Proton Git LFS/  ", want: "pass://Personal/Proton Git LFS"},
	}

	for _, tc := range cases {
		got := normalizePassRefRoot(tc.in)
		if got != tc.want {
			t.Fatalf("normalizePassRefRoot(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDefaultPassRefs(t *testing.T) {
	root := "pass://Personal/Proton Git LFS"
	if got := defaultPassUsernameRef(root); got != "pass://Personal/Proton Git LFS/username" {
		t.Fatalf("unexpected username ref %q", got)
	}
	if got := defaultPassPasswordRef(root); got != "pass://Personal/Proton Git LFS/password" {
		t.Fatalf("unexpected password ref %q", got)
	}
}

func TestDefaultConstants(t *testing.T) {
	if DefaultDriveCLIBin != "submodules/proton-drive-cli/dist/index.js" {
		t.Fatalf("unexpected DefaultDriveCLIBin: %q", DefaultDriveCLIBin)
	}
	if DefaultStorageBase != "LFS" {
		t.Fatalf("unexpected DefaultStorageBase: %q", DefaultStorageBase)
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
