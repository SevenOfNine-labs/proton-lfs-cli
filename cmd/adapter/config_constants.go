package main

import (
	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/config"
)

// Backend modes — re-exported from internal/config for package main usage.
const (
	BackendLocal = config.BackendLocal
	BackendSDK   = config.BackendSDK
)

// Credential provider kinds (mailbox/data password provider)
const (
	CredentialProviderPassCLI       = config.CredentialProviderPassCLI
	CredentialProviderGitCredential = config.CredentialProviderGitCredential
)

// Default values
const (
	DefaultDriveCLIBin        = config.DefaultDriveCLIBin
	DefaultStorageBase        = config.DefaultStorageBase
	DefaultDataCredentialHost = config.DefaultDataCredentialHost
)

// Environment variable names
const (
	EnvDriveCLIBin            = config.EnvDriveCLIBin
	EnvNodeBin                = config.EnvNodeBin
	EnvStorageBase            = config.EnvStorageBase
	EnvAppVersion             = config.EnvAppVersion
	EnvBackend                = config.EnvBackend
	EnvAllowMockTransfers     = config.EnvAllowMockTransfers
	EnvLocalStoreDir          = config.EnvLocalStoreDir
	EnvDataCredentialProvider = config.EnvDataCredentialProvider
	EnvDataCredentialHost     = config.EnvDataCredentialHost
	EnvBridgeTimeout          = config.EnvBridgeTimeout
)

func envTrim(key string) string {
	return config.EnvTrim(key)
}

func envOrDefault(key, fallback string) string {
	return config.EnvOrDefault(key, fallback)
}

func envBoolOrDefault(key string, fallback bool) bool {
	return config.EnvBoolOrDefault(key, fallback)
}
