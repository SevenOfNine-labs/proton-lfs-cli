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
	KeyPasswordProvider string `json:"keyPasswordProvider"`
}

// DefaultPreferences returns the default preferences.
func DefaultPreferences() Preferences {
	return Preferences{KeyPasswordProvider: DefaultKeyPasswordProvider}
}

// LoadPrefs reads preferences from the config file. It returns the defaults
// when the file is missing or unreadable, and the default key-password
// provider when the file names none.
func LoadPrefs() Preferences {
	path, err := PrefsFilePath()
	if err != nil {
		return DefaultPreferences()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultPreferences()
	}
	var prefs Preferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		return DefaultPreferences()
	}
	if prefs.KeyPasswordProvider == "" {
		prefs.KeyPasswordProvider = DefaultKeyPasswordProvider
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
