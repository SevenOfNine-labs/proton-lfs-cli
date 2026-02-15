package main

import (
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

func TestDriveCLIBackendEmptyProvider(t *testing.T) {
	// Without credentialProvider set, auth is delegated to proton-drive-cli
	// which will attempt resolution itself (mock bridge returns ok)
	bc := helperBridgeClient(t)
	backend := &DriveCLIBackend{
		bridge: bc,
	}

	session := &Session{Initialized: true, CreatedAt: time.Now()}
	err := backend.Initialize(session)
	if err != nil {
		t.Fatalf("Initialize with empty provider should succeed (delegated): %v", err)
	}
}

func TestBuildCredentialsGitCredentialMode(t *testing.T) {
	creds := OperationCredentials{CredentialProvider: CredentialProviderGitCredential}
	m := buildCredentials(creds, "LFS", "v1")
	if m["credentialProvider"] != CredentialProviderGitCredential {
		t.Errorf("expected credentialProvider='git-credential', got %v", m)
	}
	if m["storageBase"] != "LFS" {
		t.Errorf("expected storageBase=LFS, got %v", m["storageBase"])
	}
}
