// Package config provides shared constants, environment helpers, status
// reporting, and user preference persistence for the Proton LFS adapter
// and tray application.
package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Backend modes
const (
	BackendLocal = "local"
	BackendSDK   = "sdk"
)

// Credential providers
const (
	CredentialProviderPassCLI       = "pass-cli"
	CredentialProviderGitCredential = "git-credential"
)

// ProtonCredentialHost is the host used for git credential fill/approve and
// Proton Pass URL matching. Must match PROTON_CREDENTIAL_HOST in proton-drive-cli.
const ProtonCredentialHost = "proton.me"

// Default values
const (
	DefaultDriveCLIBin        = "submodules/proton-drive-cli/dist/index.js"
	DefaultStorageBase        = "LFS"
	DefaultCredentialProvider = CredentialProviderPassCLI
)

// Environment variable names
const (
	EnvDriveCLIBin        = "PROTON_DRIVE_CLI_BIN"
	EnvNodeBin            = "NODE_BIN"
	EnvStorageBase        = "LFS_STORAGE_BASE"
	EnvAppVersion         = "PROTON_APP_VERSION"
	EnvBackend            = "PROTON_LFS_BACKEND"
	EnvAllowMockTransfers = "ADAPTER_ALLOW_MOCK_TRANSFERS"
	EnvLocalStoreDir      = "PROTON_LFS_LOCAL_STORE_DIR"
	EnvCredentialProvider = "PROTON_CREDENTIAL_PROVIDER"
	EnvStatusFile         = "PROTON_LFS_STATUS_FILE"
)

// AppDir is the base directory for Proton LFS runtime files.
const AppDir = ".proton-lfs"

// StatusFileName is the filename for status reporting inside AppDir.
const StatusFileName = "status.json"

// ConfigFileName is the filename for user preferences inside AppDir.
const ConfigFileName = "config.json"

// AppDirPath returns the absolute path to ~/.proton-lfs.
func AppDirPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", AppDir)
	}
	return filepath.Join(home, AppDir)
}

// StatusFilePath returns the path to the status file, respecting EnvStatusFile.
func StatusFilePath() string {
	if p := EnvTrim(EnvStatusFile); p != "" {
		return p
	}
	return filepath.Join(AppDirPath(), StatusFileName)
}

// PrefsFilePath returns the path to the user preferences file.
func PrefsFilePath() string {
	return filepath.Join(AppDirPath(), ConfigFileName)
}

// EnvTrim reads an environment variable and trims whitespace.
func EnvTrim(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// EnvOrDefault reads an environment variable; returns fallback if empty.
func EnvOrDefault(key, fallback string) string {
	if value := EnvTrim(key); value != "" {
		return value
	}
	return fallback
}

// EnvBoolOrDefault reads an environment variable as a bool; returns fallback
// if the variable is empty or cannot be parsed.
func EnvBoolOrDefault(key string, fallback bool) bool {
	value := EnvTrim(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
