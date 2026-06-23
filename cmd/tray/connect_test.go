package main

import (
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
