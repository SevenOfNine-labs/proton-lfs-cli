package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Preferences stores user-facing settings managed by the tray application.
type Preferences struct {
	// KeyPasswordProvider stores the browser-fork key password: pass-cli or
	// git-credential.
	KeyPasswordProvider    string `json:"keyPasswordProvider"`
	DataCredentialProvider string `json:"dataCredentialProvider,omitempty"`
	DataCredentialHost     string `json:"dataCredentialHost,omitempty"`
	Enabled                bool   `json:"enabled"`
}

// prefsFile is the on-disk form of Preferences. Files written before
// 2026-11 stored the key-password provider under "credentialProvider"; the
// value is read from there once and saved under the current key on the next
// SavePrefs.
type prefsFile struct {
	Preferences
	FormerKeyPasswordProvider string `json:"credentialProvider,omitempty"`
}

// DefaultPreferences returns the default preferences.
func DefaultPreferences() Preferences {
	return Preferences{
		KeyPasswordProvider: DefaultKeyPasswordProvider,
		Enabled:             true,
	}
}

// LoadPrefs reads preferences from the config file.
// Returns defaults if the file is missing or unreadable.
func LoadPrefs() Preferences {
	path, err := PrefsFilePath()
	if err != nil {
		return DefaultPreferences()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultPreferences()
	}
	var file prefsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return DefaultPreferences()
	}
	prefs := file.Preferences
	if prefs.KeyPasswordProvider == "" {
		prefs.KeyPasswordProvider = file.FormerKeyPasswordProvider
	}
	if prefs.KeyPasswordProvider == "" {
		prefs.KeyPasswordProvider = DefaultKeyPasswordProvider
	}
	if prefs.DataCredentialProvider != "" && prefs.DataCredentialHost == "" {
		prefs.DataCredentialHost = DefaultDataCredentialHost
	}
	return prefs
}

// SavePrefs atomically writes preferences to the config file.
func SavePrefs(prefs Preferences) error {
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal prefs: %w", err)
	}

	path, err := PrefsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := WriteFileAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write prefs: %w", err)
	}
	return nil
}
