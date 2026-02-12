package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExecutable(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("failed to write executable %s: %v", path, err)
	}
	return path
}

func TestResolvePassCLISecretJSONAndFallback(t *testing.T) {
	scriptDir := t.TempDir()
	script := writeExecutable(t, scriptDir, "fake-pass-cli", `#!/bin/sh
if [ "$1" = "item" ] && [ "$2" = "view" ] && [ "$3" = "--output" ] && [ "$4" = "json" ]; then
  case "$5" in
    pass://Vault/Creds/username)
      echo '"alice@example.com"'
      exit 0
      ;;
    pass://Vault/Creds/password)
      echo "not-supported" >&2
      exit 1
      ;;
  esac
fi

if [ "$1" = "item" ] && [ "$2" = "view" ]; then
  case "$3" in
    pass://Vault/Creds/password)
      echo "p@ssw0rd"
      exit 0
      ;;
  esac
fi

echo "missing reference" >&2
exit 1
`)

	username, err := resolvePassCLISecret(script, "pass://Vault/Creds/username")
	if err != nil {
		t.Fatalf("username resolution failed: %v", err)
	}
	if username != "alice@example.com" {
		t.Fatalf("unexpected username %q", username)
	}

	password, err := resolvePassCLISecret(script, "pass://Vault/Creds/password")
	if err != nil {
		t.Fatalf("password resolution failed: %v", err)
	}
	if password != "p@ssw0rd" {
		t.Fatalf("unexpected password %q", password)
	}
}

func TestAdapterResolveSDKCredentialsFromPassCLIRefs(t *testing.T) {
	scriptDir := t.TempDir()
	script := writeExecutable(t, scriptDir, "fake-pass-cli", `#!/bin/sh
if [ "$1" = "item" ] && [ "$2" = "view" ] && [ "$3" = "--output" ] && [ "$4" = "json" ]; then
  case "$5" in
    pass://Vault/Creds/username)
      echo '{"value":"user@proton.test"}'
      exit 0
      ;;
    pass://Vault/Creds/password)
      echo '{"secret":"super-secret"}'
      exit 0
      ;;
  esac
fi
echo "missing reference" >&2
exit 1
`)

	adapter := NewAdapter()
	adapter.backendKind = BackendSDK
	adapter.protonUsername = nil
	adapter.protonPassword = nil
	adapter.protonPassCLIBin = script
	adapter.protonPassUserRef = "pass://Vault/Creds/username"
	adapter.protonPassPassRef = "pass://Vault/Creds/password"

	if err := adapter.resolveSDKCredentials(); err != nil {
		t.Fatalf("resolveSDKCredentials returned error: %v", err)
	}
	if string(adapter.protonUsername) != "user@proton.test" {
		t.Fatalf("unexpected resolved username %q", adapter.protonUsername)
	}
	if string(adapter.protonPassword) != "super-secret" {
		t.Fatalf("unexpected resolved password %q", adapter.protonPassword)
	}
}

func TestAdapterResolveSDKCredentialsUsesPassUserInfoFallback(t *testing.T) {
	scriptDir := t.TempDir()
	script := writeExecutable(t, scriptDir, "fake-pass-cli", `#!/bin/sh
if [ "$1" = "item" ] && [ "$2" = "view" ] && [ "$3" = "--output" ] && [ "$4" = "json" ]; then
  case "$5" in
    pass://Vault/Creds/password)
      echo '{"secret":"from-pass"}'
      exit 0
      ;;
  esac
fi
if [ "$1" = "user" ] && [ "$2" = "info" ] && [ "$3" = "--output" ] && [ "$4" = "json" ]; then
  echo '{"email":"user-from-info@proton.test"}'
  exit 0
fi
echo "missing reference" >&2
exit 1
`)

	adapter := NewAdapter()
	adapter.backendKind = BackendSDK
	adapter.protonUsername = nil
	adapter.protonPassword = nil
	adapter.protonPassCLIBin = script
	adapter.protonPassUserRef = ""
	adapter.protonPassPassRef = "pass://Vault/Creds/password"

	if err := adapter.resolveSDKCredentials(); err != nil {
		t.Fatalf("resolveSDKCredentials returned error: %v", err)
	}
	if string(adapter.protonUsername) != "user-from-info@proton.test" {
		t.Fatalf("unexpected user-info resolved username %q", adapter.protonUsername)
	}
	if string(adapter.protonPassword) != "from-pass" {
		t.Fatalf("unexpected resolved password %q", adapter.protonPassword)
	}
}

func TestAdapterResolveSDKCredentialsFailsWithoutPassCLI(t *testing.T) {
	scriptDir := t.TempDir()
	script := writeExecutable(t, scriptDir, "fake-pass-cli", `#!/bin/sh
echo "not found" >&2
exit 1
`)

	adapter := NewAdapter()
	adapter.backendKind = BackendSDK
	adapter.protonUsername = nil
	adapter.protonPassword = nil
	adapter.protonPassCLIBin = script
	adapter.protonPassUserRef = "pass://Vault/Creds/username"
	adapter.protonPassPassRef = "pass://Vault/Creds/password"

	err := adapter.resolveSDKCredentials()
	if err == nil {
		t.Fatal("expected resolveSDKCredentials to return error when pass-cli is unavailable")
	}
	if !strings.Contains(err.Error(), "username reference") {
		t.Fatalf("expected error about username reference, got: %v", err)
	}
}

func TestAdapterResolveSDKCredentialsMandatory(t *testing.T) {
	adapter := NewAdapter()
	adapter.backendKind = BackendSDK
	adapter.protonUsername = nil
	adapter.protonPassword = nil
	adapter.protonPassCLIBin = "nonexistent-binary"
	adapter.protonPassUserRef = ""
	adapter.protonPassPassRef = ""

	err := adapter.resolveSDKCredentials()
	if err == nil {
		t.Fatal("expected resolveSDKCredentials to return error when no pass-cli refs configured")
	}
	if !strings.Contains(err.Error(), "could not resolve Proton username via pass-cli") {
		t.Fatalf("expected mandatory error message, got: %v", err)
	}
}

func TestParsePassCLISecretValue(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "json-string", raw: `"hello"`, want: "hello"},
		{name: "json-object", raw: `{"value":"hello"}`, want: "hello"},
		{name: "plain-single-line", raw: "hello\n", want: "hello"},
		{name: "value-prefix", raw: "Value: hello", want: "hello"},
		{name: "multi-line-last", raw: "line1\nline2", want: "line2"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePassCLISecretValue(tc.raw)
			if got != tc.want {
				t.Fatalf("unexpected parsed value: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestParsePassCLIUserEmail(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "json-email", raw: `{"email":"user@proton.test"}`, want: "user@proton.test"},
		{name: "json-username", raw: `{"username":"user@proton.test"}`, want: "user@proton.test"},
		{name: "plain-email", raw: "Email: user@proton.test", want: "user@proton.test"},
		{name: "plain-username", raw: "Username: user@proton.test", want: "user@proton.test"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parsePassCLIUserEmail(tc.raw)
			if got != tc.want {
				t.Fatalf("unexpected parsed email: got %q want %q", got, tc.want)
			}
		})
	}
}
