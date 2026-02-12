package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAdapterCandidates(t *testing.T) {
	candidates := adapterCandidates("/test/dir")
	if len(candidates) < 1 {
		t.Fatal("expected at least 1 candidate")
	}

	expectedName := "git-lfs-proton-adapter"
	if runtime.GOOS == "windows" {
		expectedName += ".exe"
	}

	if candidates[0] != filepath.Join("/test/dir", expectedName) {
		t.Fatalf("expected same-dir candidate %q, got %q", filepath.Join("/test/dir", expectedName), candidates[0])
	}

	if runtime.GOOS == "darwin" {
		if len(candidates) != 2 {
			t.Fatalf("expected 2 candidates on darwin, got %d", len(candidates))
		}
		if !strings.Contains(candidates[1], "Helpers") {
			t.Fatalf("expected Helpers candidate on darwin, got %q", candidates[1])
		}
	} else if runtime.GOOS != "windows" {
		if len(candidates) != 1 {
			t.Fatalf("expected 1 candidate on %s, got %d", runtime.GOOS, len(candidates))
		}
	}
}

func TestDriveCLICandidates(t *testing.T) {
	candidates := driveCLICandidates("/test/dir")
	if len(candidates) < 1 {
		t.Fatal("expected at least 1 candidate")
	}

	if runtime.GOOS == "darwin" {
		if len(candidates) != 2 {
			t.Fatalf("expected 2 candidates on darwin, got %d", len(candidates))
		}
	} else if runtime.GOOS != "windows" {
		if len(candidates) != 1 {
			t.Fatalf("expected 1 candidate on %s, got %d", runtime.GOOS, len(candidates))
		}
	}
}

func TestDiscoverAdapterBinaryFindsRealFile(t *testing.T) {
	dir := t.TempDir()
	name := "git-lfs-proton-adapter"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	fakeBin := filepath.Join(dir, name)
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	candidates := adapterCandidates(dir)
	found := ""
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			found = c
			break
		}
	}
	if found == "" {
		t.Fatal("expected to discover adapter binary")
	}
	if found != fakeBin {
		t.Fatalf("expected %q, got %q", fakeBin, found)
	}
}

func TestLaunchAgentPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	p := launchAgentPath()
	if p == "" {
		t.Fatal("expected non-empty path")
	}
	if !strings.HasSuffix(p, "com.proton.git-lfs-tray.plist") {
		t.Fatalf("expected plist filename, got %q", p)
	}
	if !strings.Contains(p, filepath.Join("Library", "LaunchAgents")) {
		t.Fatalf("expected LaunchAgents dir, got %q", p)
	}
}

func TestSetAutoStartDarwinCreatesValidPlist(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := setAutoStartDarwin(true); err != nil {
		t.Fatalf("setAutoStartDarwin(true) failed: %v", err)
	}

	plistPath := filepath.Join(tmpHome, "Library", "LaunchAgents", "com.proton.git-lfs-tray.plist")
	content, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("plist not created: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "<?xml") {
		t.Fatal("expected XML header")
	}
	if !strings.Contains(s, "<key>Label</key>") {
		t.Fatal("expected Label key")
	}
	if !strings.Contains(s, "<key>RunAtLoad</key>") {
		t.Fatal("expected RunAtLoad key")
	}
	if !strings.Contains(s, "<key>ProgramArguments</key>") {
		t.Fatal("expected ProgramArguments key")
	}
	info, _ := os.Stat(plistPath)
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("expected 0644 perms, got %o", perm)
	}
}

func TestSetAutoStartDarwinDisableRemovesPlist(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := setAutoStartDarwin(true); err != nil {
		t.Fatalf("enable failed: %v", err)
	}
	if err := setAutoStartDarwin(false); err != nil {
		t.Fatalf("disable failed: %v", err)
	}

	plistPath := filepath.Join(tmpHome, "Library", "LaunchAgents", "com.proton.git-lfs-tray.plist")
	if _, err := os.Stat(plistPath); err == nil {
		t.Fatal("plist should be removed after disable")
	}
}

func TestSetAutoStartLinuxCreatesDesktopEntry(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only test")
	}
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := setAutoStartLinux(true); err != nil {
		t.Fatalf("setAutoStartLinux(true) failed: %v", err)
	}

	desktopPath := filepath.Join(tmpHome, ".config", "autostart", "proton-git-lfs.desktop")
	content, err := os.ReadFile(desktopPath)
	if err != nil {
		t.Fatalf("desktop entry not created: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "[Desktop Entry]") {
		t.Fatal("expected [Desktop Entry] header")
	}
	if !strings.Contains(s, "Type=Application") {
		t.Fatal("expected Type=Application")
	}
	if !strings.Contains(s, "Exec=") {
		t.Fatal("expected Exec key")
	}
}

func TestIsAutoStartEnabled(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if isAutoStartEnabled() {
		t.Fatal("expected false when no autostart file exists")
	}

	switch runtime.GOOS {
	case "darwin":
		plistDir := filepath.Join(tmpHome, "Library", "LaunchAgents")
		if err := os.MkdirAll(plistDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(plistDir, "com.proton.git-lfs-tray.plist"), []byte("<plist/>"), 0o644); err != nil {
			t.Fatal(err)
		}
	case "linux":
		autoDir := filepath.Join(tmpHome, ".config", "autostart")
		if err := os.MkdirAll(autoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(autoDir, "proton-git-lfs.desktop"), []byte("[Desktop Entry]"), 0o644); err != nil {
			t.Fatal(err)
		}
	default:
		t.Skip("unsupported OS for autostart test")
	}

	if !isAutoStartEnabled() {
		t.Fatal("expected true after creating autostart file")
	}
}
