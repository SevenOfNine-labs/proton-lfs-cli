// Package config provides shared constants, environment helpers, status
// reporting, and user preference persistence for the Proton LFS adapter
// and tray application.
package config

import (
	"fmt"
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

// Credential stores that hold the session tokens and key password of a
// proton-drive-cli login (proton-drive-cli src/credentials/types.ts).
const (
	CredentialStorePassCLI       = "pass-cli"
	CredentialStoreGitCredential = "git-credential"
)

// Default values
const (
	DefaultDriveCLIBin     = "submodules/proton-drive-cli/dist/index.js"
	DefaultStorageBase     = "LFS"
	DefaultCredentialStore = CredentialStorePassCLI
	// DefaultDriveCLIAppVersion equals proton-drive-cli's x-pm-appversion
	// identity, external-drive-protonlfscli@<package.json version>.
	DefaultDriveCLIAppVersion = "external-drive-protonlfscli@0.2.0"
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
	EnvStatusFile         = "PROTON_LFS_STATUS_FILE"
	EnvBridgeTimeout      = "PROTON_LFS_BRIDGE_TIMEOUT"
)

// AppDir is the base directory for Proton LFS runtime files.
const AppDir = ".proton-lfs"

// StatusFileName is the filename for status reporting inside AppDir.
const StatusFileName = "status.json"

// ConfigFileName is the filename for user preferences inside AppDir.
const ConfigFileName = "config.json"

// AppDirPath returns the absolute path to ~/.proton-lfs. It fails when the
// home directory cannot be determined (for example HOME unset under cron or
// systemd); callers must not fall back to the working directory, which is the
// Git repository when git-lfs runs the adapter.
func AppDirPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory for %s: %w", AppDir, err)
	}
	return filepath.Join(home, AppDir), nil
}

// StatusFilePath returns the path to the status file, respecting EnvStatusFile.
func StatusFilePath() (string, error) {
	if p := EnvTrim(EnvStatusFile); p != "" {
		return p, nil
	}
	dir, err := AppDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, StatusFileName), nil
}

// PrefsFilePath returns the path to the user preferences file.
func PrefsFilePath() (string, error) {
	dir, err := AppDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ConfigFileName), nil
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
