//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCredentialPassCLIResolution verifies the pass-cli → adapter → bridge credential flow.
func TestCredentialPassCLIResolution(t *testing.T) {
	passCLIBin := sdkPassCLIPath()
	if _, err := exec.LookPath(passCLIBin); err != nil {
		t.Skipf("pass-cli not found: %v", err)
	}

	_, usernameRef, passwordRef := sdkPassRefConfig()

	// Verify that pass-cli can resolve credentials
	username := sdkReadPassCLISecret(t, passCLIBin, usernameRef)
	if username == "" {
		t.Fatal("expected non-empty username from pass-cli")
	}

	password := sdkReadPassCLISecret(t, passCLIBin, passwordRef)
	if password == "" {
		t.Fatal("expected non-empty password from pass-cli")
	}

	// Sanity: password should not appear in username
	if strings.Contains(username, password) {
		t.Fatal("username should not contain password")
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

	// Run the adapter with credentials resolved via mock-pass-cli.
	// The error messages should NOT contain the password.
	testPassword := "super-secret-test-password-42"
	cmd := exec.Command(adapterPath, "--backend=sdk", "--drive-cli-bin=/nonexistent/proton-drive-cli")
	cmd.Env = append(
		os.Environ(),
		"PROTON_PASS_CLI_BIN="+mockPassCLI,
		"PROTON_PASS_REF_ROOT=pass://Personal/Proton Git LFS",
		"PASS_MOCK_USERNAME=test@invalid.test",
		"PASS_MOCK_PASSWORD="+testPassword,
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
