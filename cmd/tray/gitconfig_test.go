package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func countGitConfigQueries(t *testing.T) *int {
	t.Helper()
	orig := queryGitLFSConfigFn
	count := 0
	queryGitLFSConfigFn = func() gitLFSConfig {
		count++
		return orig()
	}
	t.Cleanup(func() { queryGitLFSConfigFn = orig })
	return &count
}

func TestReadGitLFSConfigQueriesGitOnlyWhenFileChanges(t *testing.T) {
	setupFakeHome(t, fakeHomeOpts{})
	gitCfg := setupGitConfig(t, "[lfs]\n\tstandalonetransferagent = proton\n")
	queries := countGitConfigQueries(t)
	invalidateGitLFSConfig()

	first := readGitLFSConfig()
	if first.StandaloneAgent != "proton" {
		t.Fatalf("StandaloneAgent = %q, want proton", first.StandaloneAgent)
	}
	for range 5 {
		readGitLFSConfig()
	}
	if *queries != 1 {
		t.Fatalf("git queried %d times for an unchanged config file, want 1", *queries)
	}

	// The size change alone updates the stamp when mtime resolution is coarse.
	if err := os.WriteFile(gitCfg, []byte("[lfs]\n\tstandalonetransferagent = basic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := readGitLFSConfig()
	if *queries != 2 {
		t.Fatalf("git queried %d times after the file changed, want 2", *queries)
	}
	if second.StandaloneAgent != "basic" {
		t.Fatalf("StandaloneAgent = %q, want basic", second.StandaloneAgent)
	}
}

func TestReadGitLFSConfigUnregistered(t *testing.T) {
	setupFakeHome(t, fakeHomeOpts{})
	setupGitConfig(t, "")
	invalidateGitLFSConfig()

	if cfg := readGitLFSConfig(); cfg.StandaloneAgent != "" {
		t.Fatalf("expected empty config, got %#v", cfg)
	}
	if isLFSEnabled() {
		t.Fatal("isLFSEnabled must be false without registration")
	}
}

func TestWriteGitLFSRegistrationInvalidatesCache(t *testing.T) {
	setupFakeHome(t, fakeHomeOpts{})
	setupGitConfig(t, "")
	invalidateGitLFSConfig()
	if isLFSEnabled() {
		t.Fatal("precondition: not registered")
	}

	if err := writeGitLFSRegistration("/tmp/adapter", "--backend sdk"); err != nil {
		t.Fatal(err)
	}
	cfg := readGitLFSConfig()
	if cfg.StandaloneAgent != "proton" {
		t.Fatalf("registration not visible after write: %#v", cfg)
	}
}

func TestGlobalGitConfigFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GIT_CONFIG_GLOBAL", "")

	files := globalGitConfigFiles()
	want := []string{filepath.Join(home, ".config", "git", "config"), filepath.Join(home, ".gitconfig")}
	if len(files) != 2 || files[0] != want[0] || files[1] != want[1] {
		t.Fatalf("files = %q, want %q", files, want)
	}

	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	if files := globalGitConfigFiles(); files[0] != filepath.Join(home, "xdg", "git", "config") {
		t.Fatalf("XDG_CONFIG_HOME not honoured: %q", files)
	}

	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "override"))
	if files := globalGitConfigFiles(); len(files) != 1 || files[0] != filepath.Join(home, "override") {
		t.Fatalf("GIT_CONFIG_GLOBAL not honoured: %q", files)
	}
}

func TestGitConfigStampTracksSizeAndModTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gitconfig")
	missing := gitConfigStamp([]string{path})
	if err := os.WriteFile(path, []byte("[lfs]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := gitConfigStamp([]string{path})
	if created == missing {
		t.Fatal("stamp must change when the file appears")
	}
	if err := os.Chtimes(path, time.Now(), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if touched := gitConfigStamp([]string{path}); touched == created {
		t.Fatal("stamp must change when mtime changes")
	}
}
