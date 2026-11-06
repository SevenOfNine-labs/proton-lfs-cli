package main

import "testing"

func TestBuildProtonTransferArgs(t *testing.T) {
	cases := []struct {
		name     string
		driveCLI string
		want     string
	}{
		{
			name:     "drive cli path",
			driveCLI: "/tmp/proton-drive-cli",
			want:     "--backend sdk --drive-cli-bin /tmp/proton-drive-cli",
		},
		{
			name:     "no drive cli path",
			driveCLI: "",
			want:     "--backend sdk",
		},
		{
			name:     "path with spaces is single-quoted",
			driveCLI: "/Applications/Proton Drive CLI/proton-drive-cli",
			want:     "--backend sdk --drive-cli-bin '/Applications/Proton Drive CLI/proton-drive-cli'",
		},
		{
			name:     "embedded single quote",
			driveCLI: "/tmp/Proton's Drive/proton-drive-cli",
			want:     "--backend sdk --drive-cli-bin '/tmp/Proton'\\''s Drive/proton-drive-cli'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildProtonTransferArgs(tc.driveCLI); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
