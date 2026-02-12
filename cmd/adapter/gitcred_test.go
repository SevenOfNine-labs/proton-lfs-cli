package main

import (
	"strings"
	"testing"
	"time"
)

func TestDriveCLIBackendGitCredentialModeInitialize(t *testing.T) {
	bc := helperBridgeClient(t)
	backend := &DriveCLIBackend{
		bridge:             bc,
		credentialProvider: CredentialProviderGitCredential,
	}

	session := &Session{Initialized: true, CreatedAt: time.Now()}
	err := backend.Initialize(session)
	if err != nil {
		t.Fatalf("Initialize with git-credential failed: %v", err)
	}

	if session.Token != "direct-bridge" {
		t.Errorf("expected token 'direct-bridge', got %q", session.Token)
	}
	if !backend.authenticated {
		t.Error("expected authenticated=true after Initialize")
	}
}

func TestDriveCLIBackendGitCredentialModeOperationCredentials(t *testing.T) {
	backend := &DriveCLIBackend{
		credentialProvider: CredentialProviderGitCredential,
	}

	creds := backend.operationCredentials()
	if creds.CredentialProvider != CredentialProviderGitCredential {
		t.Errorf("expected credentialProvider=%q, got %q", CredentialProviderGitCredential, creds.CredentialProvider)
	}
	if creds.Username != "" {
		t.Errorf("username should be empty in git-credential mode, got %q", creds.Username)
	}
	if creds.Password != "" {
		t.Errorf("password should be empty in git-credential mode, got %q", creds.Password)
	}
}

func TestCredentialProviderConstants(t *testing.T) {
	if CredentialProviderPassCLI != "pass-cli" {
		t.Errorf("expected 'pass-cli', got %q", CredentialProviderPassCLI)
	}
	if CredentialProviderGitCredential != "git-credential" {
		t.Errorf("expected 'git-credential', got %q", CredentialProviderGitCredential)
	}
	if DefaultCredentialProvider != CredentialProviderPassCLI {
		t.Errorf("expected default provider to be pass-cli, got %q", DefaultCredentialProvider)
	}
}

func TestDriveCLIBackendFallsBackToPassCLI(t *testing.T) {
	// Without credentialProvider set, it should require username/password
	bc := helperBridgeClient(t)
	backend := &DriveCLIBackend{
		bridge: bc,
	}

	session := &Session{Initialized: true, CreatedAt: time.Now()}
	err := backend.Initialize(session)
	if err == nil {
		t.Fatal("expected error when no credentials and no credential provider")
	}
	if !strings.Contains(err.Error(), "credentials are required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestBuildCredentialsGitCredentialMode(t *testing.T) {
	creds := OperationCredentials{CredentialProvider: "git-credential"}
	m := buildCredentials(creds, "LFS", "v1")
	if m["credentialProvider"] != "git-credential" {
		t.Errorf("expected credentialProvider='git-credential', got %v", m)
	}
	if _, ok := m["username"]; ok {
		t.Error("username should not be present in git-credential mode")
	}
	if _, ok := m["password"]; ok {
		t.Error("password should not be present in git-credential mode")
	}
}
