package main

import (
	"proton-git-lfs/internal/config"
)

// Backend modes — re-exported from internal/config for package main usage.
const (
	BackendLocal = config.BackendLocal
	BackendSDK   = config.BackendSDK
)

// Credential providers
const (
	CredentialProviderPassCLI       = config.CredentialProviderPassCLI
	CredentialProviderGitCredential = config.CredentialProviderGitCredential
)

// Default values
const (
	DefaultDriveCLIBin        = config.DefaultDriveCLIBin
	DefaultStorageBase        = config.DefaultStorageBase
	DefaultPassCLIBinary      = config.DefaultPassCLIBinary
	DefaultPassRefRoot        = config.DefaultPassRefRoot
	DefaultCredentialProvider = config.DefaultCredentialProvider
)

// Environment variable names
const (
	EnvDriveCLIBin        = config.EnvDriveCLIBin
	EnvNodeBin            = config.EnvNodeBin
	EnvStorageBase        = config.EnvStorageBase
	EnvAppVersion         = config.EnvAppVersion
	EnvBackend            = config.EnvBackend
	EnvAllowMockTransfers = config.EnvAllowMockTransfers
	EnvLocalStoreDir      = config.EnvLocalStoreDir
	EnvPassCLIBin         = config.EnvPassCLIBin
	EnvPassRefRoot        = config.EnvPassRefRoot
	EnvPassUsernameRef    = config.EnvPassUsernameRef
	EnvPassPasswordRef    = config.EnvPassPasswordRef
	EnvCredentialProvider = config.EnvCredentialProvider
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

func passRefRootFromEnv() string {
	return config.PassRefRootFromEnv()
}

func normalizePassRefRoot(root string) string {
	return config.NormalizePassRefRoot(root)
}

func defaultPassUsernameRef(root string) string {
	return config.DefaultPassUsernameRef(root)
}

func defaultPassPasswordRef(root string) string {
	return config.DefaultPassPasswordRef(root)
}
