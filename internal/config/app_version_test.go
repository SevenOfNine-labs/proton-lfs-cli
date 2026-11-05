package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultDriveCLIAppVersionMatchesSubmodule keeps the Go-side default in
// sync with proton-drive-cli, which derives its x-pm-appversion identity from
// its own package.json version.
func TestDefaultDriveCLIAppVersionMatchesSubmodule(t *testing.T) {
	pkgPath := filepath.Join("..", "..", "submodules", "proton-drive-cli", "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Skipf("proton-drive-cli submodule not available: %v", err)
	}

	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatalf("parse %s: %v", pkgPath, err)
	}
	if strings.TrimSpace(pkg.Version) == "" {
		t.Fatalf("%s has no version", pkgPath)
	}

	want := "external-drive-protonlfscli@" + pkg.Version
	if DefaultDriveCLIAppVersion != want {
		t.Fatalf("DefaultDriveCLIAppVersion = %q, want %q (sync with proton-drive-cli package.json)",
			DefaultDriveCLIAppVersion, want)
	}
}
