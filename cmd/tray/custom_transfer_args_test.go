package main

import (
	"strings"
	"testing"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
)

func TestBuildProtonTransferArgsFromPrefs(t *testing.T) {
	cases := []struct {
		name     string
		prefs    config.Preferences
		driveCLI string
		want     string
	}{
		{
			name:     "pass-cli key password provider is not a transfer argument",
			prefs:    config.Preferences{KeyPasswordProvider: config.CredentialProviderPassCLI},
			driveCLI: "/tmp/proton-drive-cli",
			want:     "--backend sdk --drive-cli-bin /tmp/proton-drive-cli",
		},
		{
			name:     "git-credential key password provider is not a transfer argument",
			prefs:    config.Preferences{KeyPasswordProvider: config.CredentialProviderGitCredential},
			driveCLI: "/tmp/proton-drive-cli",
			want:     "--backend sdk --drive-cli-bin /tmp/proton-drive-cli",
		},
		{
			name:     "no drive cli path",
			prefs:    config.Preferences{KeyPasswordProvider: config.CredentialProviderPassCLI},
			driveCLI: "",
			want:     "--backend sdk",
		},
		{
			name:     "path with spaces is single-quoted",
			prefs:    config.Preferences{KeyPasswordProvider: config.CredentialProviderGitCredential},
			driveCLI: "/Applications/Proton Drive CLI/proton-drive-cli",
			want:     "--backend sdk --drive-cli-bin '/Applications/Proton Drive CLI/proton-drive-cli'",
		},
		{
			name:     "embedded single quote",
			prefs:    config.Preferences{KeyPasswordProvider: config.CredentialProviderPassCLI},
			driveCLI: "/tmp/Proton's Drive/proton-drive-cli",
			want:     "--backend sdk --drive-cli-bin '/tmp/Proton'\\''s Drive/proton-drive-cli'",
		},
		{
			name: "data credential provider with default host",
			prefs: config.Preferences{
				KeyPasswordProvider:    config.CredentialProviderPassCLI,
				DataCredentialProvider: config.CredentialProviderPassCLI,
			},
			driveCLI: "/tmp/proton-drive-cli",
			want:     "--backend sdk --drive-cli-bin /tmp/proton-drive-cli --data-credential-provider pass-cli --data-credential-host proton-data.proton-lfs-cli.local",
		},
		{
			name: "data credential provider with custom host",
			prefs: config.Preferences{
				KeyPasswordProvider:    config.CredentialProviderGitCredential,
				DataCredentialProvider: config.CredentialProviderGitCredential,
				DataCredentialHost:     "proton-data.example.test",
			},
			driveCLI: "/tmp/proton-drive-cli",
			want:     "--backend sdk --drive-cli-bin /tmp/proton-drive-cli --data-credential-provider git-credential --data-credential-host proton-data.example.test",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildProtonTransferArgsFromPrefs(tc.prefs, tc.driveCLI)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
			if strings.Contains(got, "--credential-provider") {
				t.Fatalf("transfer args must never carry --credential-provider: %q", got)
			}
		})
	}
}
