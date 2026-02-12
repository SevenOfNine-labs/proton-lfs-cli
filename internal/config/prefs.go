package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Preferences stores user-facing settings managed by the tray application.
type Preferences struct {
	CredentialProvider string `json:"credentialProvider"`
	Enabled            bool   `json:"enabled"`
}

// DefaultPreferences returns the default preferences.
func DefaultPreferences() Preferences {
	return Preferences{
		CredentialProvider: DefaultCredentialProvider,
		Enabled:            true,
	}
}

// LoadPrefs reads preferences from the config file.
// Returns defaults if the file is missing or unreadable.
func LoadPrefs() Preferences {
	path := PrefsFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultPreferences()
	}
	var prefs Preferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return DefaultPreferences()
	}
	if prefs.CredentialProvider == "" {
		prefs.CredentialProvider = DefaultCredentialProvider
	}
	return prefs
}

// SavePrefs atomically writes preferences to the config file.
func SavePrefs(prefs Preferences) error {
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal prefs: %w", err)
	}

	path := PrefsFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tmp := fmt.Sprintf("%s.tmp-%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write prefs tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename prefs: %w", err)
	}
	return nil
}
