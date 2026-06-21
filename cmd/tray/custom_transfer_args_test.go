package main

import (
	"testing"

	"proton-lfs-cli/internal/config"
)

func TestBuildProtonTransferArgsPassCLI(t *testing.T) {
	got := buildProtonTransferArgs(config.CredentialProviderPassCLI, "/tmp/proton-drive-cli")
	want := "--backend sdk --drive-cli-bin /tmp/proton-drive-cli"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBuildProtonTransferArgsGitCredential(t *testing.T) {
	got := buildProtonTransferArgs(config.CredentialProviderGitCredential, "/tmp/proton-drive-cli")
	want := "--backend sdk --credential-provider git-credential --drive-cli-bin /tmp/proton-drive-cli"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBuildProtonTransferArgsQuotesDriveCLIPath(t *testing.T) {
	got := buildProtonTransferArgs(
		config.CredentialProviderGitCredential,
		"/Applications/Proton Drive CLI/proton-drive-cli",
	)
	want := "--backend sdk --credential-provider git-credential --drive-cli-bin '/Applications/Proton Drive CLI/proton-drive-cli'"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBuildProtonTransferArgsQuotesSingleQuotes(t *testing.T) {
	got := buildProtonTransferArgs(config.CredentialProviderPassCLI, "/tmp/Proton's Drive/proton-drive-cli")
	want := "--backend sdk --drive-cli-bin '/tmp/Proton'\\''s Drive/proton-drive-cli'"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
