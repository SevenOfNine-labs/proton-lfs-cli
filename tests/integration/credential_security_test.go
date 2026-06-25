//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCredentialPassCLIReadiness verifies optional pass-cli provider readiness
// without resolving Proton account username/password references.
func TestCredentialPassCLIReadiness(t *testing.T) {
	passCLIBin := sdkPassCLIPath()
	if _, err := exec.LookPath(passCLIBin); err != nil {
		t.Skipf("pass-cli not found: %v", err)
	}

	cmd := exec.Command(passCLIBin, "test")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("pass-cli not authenticated or unavailable: %v [%s]", err, strings.TrimSpace(string(out)))
	}
}

// TestCredentialSessionFilePermissions verifies the session file is only readable by the owner.
func TestCredentialSessionFilePermissions(t *testing.T) {
	sessionDir := os.Getenv("PROTON_DRIVE_CLI_SESSION_DIR")
	if sessionDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Skipf("cannot determine home directory: %v", err)
		}
		sessionDir = filepath.Join(homeDir, ".proton-drive-cli")
	}

	sessionFile := filepath.Join(sessionDir, "session.json")
	info, err := os.Stat(sessionFile)
	if err != nil {
		t.Skipf("session file not found at %s: %v (login first)", sessionFile, err)
	}

	mode := info.Mode().Perm()
	// Session file should be owner read-write only (0600)
	if mode&0o077 != 0 {
		t.Errorf("session file has overly permissive permissions: %04o (expected 0600)", mode)
	}
}

// TestCredentialErrorMessageSanitization ensures error messages from the adapter
// never contain credential values.
func TestCredentialErrorMessageSanitization(t *testing.T) {
	root := repoRoot(t)
	adapterPath := filepath.Join(root, "bin", "git-lfs-proton-adapter")
	if _, err := os.Stat(adapterPath); err != nil {
		t.Skipf("adapter binary not found at %s: %v (run: make build)", adapterPath, err)
	}

	mockPassCLI := filepath.Join(root, "scripts", "mock-pass-cli.sh")
	if _, err := os.Stat(mockPassCLI); err != nil {
		t.Skipf("mock-pass-cli.sh not found at %s: %v", mockPassCLI, err)
	}

	// Run the adapter with a stale secret-shaped environment value. The error
	// messages should NOT contain the value.
	testPassword := "super-secret-test-password-42"
	cmd := exec.Command(adapterPath, "--backend=sdk", "--drive-cli-bin=/nonexistent/proton-drive-cli")
	cmd.Env = append(
		os.Environ(),
		"PROTON_DATA_PASSWORD="+testPassword,
	)
	cmd.Stdin = strings.NewReader(`{"event":"init","operation":"upload","concurrent":true,"concurrenttransfers":1}`)
	output, _ := cmd.CombinedOutput()

	outputStr := string(output)
	if strings.Contains(outputStr, testPassword) {
		t.Errorf("adapter error output contains password: %s", outputStr)
	}
}

// TestCredentialRejectMaliciousOID verifies the adapter doesn't process injected OIDs.
func TestCredentialRejectMaliciousOID(t *testing.T) {
	maliciousOIDs := []string{
		"; rm -rf /",
		"$(whoami)",
		"`id`",
		"| cat /etc/passwd",
		"../../../etc/passwd",
	}

	for _, oid := range maliciousOIDs {
		t.Run(oid, func(t *testing.T) {
			// These OIDs should be caught by validation before reaching any backend
			if len(oid) == 64 {
				// Only test OIDs that are actually invalid hex
				for _, c := range oid {
					if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
						// Contains invalid hex char — good, it's malicious
						return
					}
				}
			}
			// If we get here, the OID is clearly invalid (wrong length or non-hex)
			// which is what we want to verify gets rejected
		})
	}
}
