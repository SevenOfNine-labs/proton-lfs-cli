package main

import (
	"strings"
	"testing"

	"proton-lfs-cli/internal/config"
)

func TestShouldOpenInteractiveCredentialStore(t *testing.T) {
	if shouldOpenInteractiveCredentialStore(config.CredentialProviderPassCLI) {
		t.Fatal("pass-cli connect must not open an interactive credential prompt")
	}
	if !shouldOpenInteractiveCredentialStore(config.CredentialProviderGitCredential) {
		t.Fatal("git-credential connect should keep interactive credential setup")
	}
}

func TestWithAuthTraceEnv(t *testing.T) {
	env := withAuthTraceEnv([]string{"EXISTING=1"}, "trace-123")
	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"EXISTING=1",
		"PROTON_AUTH_TRACE=1",
		"PROTON_AUTH_TRACE_ID=trace-123",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected auth trace env %q in:\n%s", want, joined)
		}
	}
}

func TestWithAuthTraceEnvSkipsEmptyTraceID(t *testing.T) {
	env := withAuthTraceEnv([]string{"EXISTING=1"}, " ")
	if len(env) != 1 || env[0] != "EXISTING=1" {
		t.Fatalf("expected unchanged env for empty trace id, got %#v", env)
	}
}
