package main

import (
	"testing"
	"time"
)

func TestDriveCLIBackendGitCredentialModeInitialize(t *testing.T) {
	bc := helperBridgeClient(t)
	backend := &DriveCLIBackend{
		bridge: bc,
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

func TestDriveCLIBackendDataCredentialModeOperationCredentials(t *testing.T) {
	backend := &DriveCLIBackend{
		dataCredentialProvider: CredentialProviderGitCredential,
		dataCredentialHost:     DefaultDataCredentialHost,
	}

	creds := backend.operationCredentials()
	if creds.DataCredentialProvider != CredentialProviderGitCredential {
		t.Errorf("expected dataCredentialProvider=%q, got %q", CredentialProviderGitCredential, creds.DataCredentialProvider)
	}
	if creds.DataCredentialHost != DefaultDataCredentialHost {
		t.Errorf("expected dataCredentialHost=%q, got %q", DefaultDataCredentialHost, creds.DataCredentialHost)
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
	if DefaultDataCredentialHost == "" {
		t.Error("expected non-empty default data credential host")
	}
}

func TestDriveCLIBackendEmptyProvider(t *testing.T) {
	// Account authentication is session-only; initialization succeeds when the
	// offline auth-state reports ready.
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
	m := buildCredentials(OperationCredentials{}, "LFS", "v1")
	if _, ok := m["credentialProvider"]; ok {
		t.Errorf("credentialProvider must not be sent to bridge, got %v", m)
	}
	if m["storageBase"] != "LFS" {
		t.Errorf("expected storageBase=LFS, got %v", m["storageBase"])
	}
}
