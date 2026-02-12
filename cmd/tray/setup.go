package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// discoverAdapterBinary finds the bundled git-lfs-proton-adapter binary
// relative to the running tray executable.
func discoverAdapterBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, _ = filepath.EvalSymlinks(exe)
	dir := filepath.Dir(exe)

	candidates := adapterCandidates(dir)
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}

// discoverDriveCLIBinary finds the bundled proton-drive-cli binary.
func discoverDriveCLIBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, _ = filepath.EvalSymlinks(exe)
	dir := filepath.Dir(exe)

	candidates := driveCLICandidates(dir)
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c
		}
	}
	return ""
}

func adapterCandidates(exeDir string) []string {
	name := "git-lfs-proton-adapter"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidates := []string{
		filepath.Join(exeDir, name), // same directory (Linux/Windows)
	}
	if runtime.GOOS == "darwin" {
		// macOS .app bundle: Contents/MacOS/tray → Contents/Helpers/adapter
		candidates = append(candidates, filepath.Join(exeDir, "..", "Helpers", name))
	}
	return candidates
}

func driveCLICandidates(exeDir string) []string {
	name := "proton-drive-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	candidates := []string{
		filepath.Join(exeDir, name),
	}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates, filepath.Join(exeDir, "..", "Helpers", name))
	}
	return candidates
}

const launchAgentLabel = "com.proton.git-lfs-tray"

// launchAgentPath returns the path to the macOS LaunchAgent plist.
func launchAgentPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

// isAutoStartEnabled checks if the LaunchAgent plist exists (macOS) or the
// desktop autostart file exists (Linux).
func isAutoStartEnabled() bool {
	switch runtime.GOOS {
	case "darwin":
		p := launchAgentPath()
		if p == "" {
			return false
		}
		_, err := os.Stat(p)
		return err == nil
	case "linux":
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		_, err = os.Stat(filepath.Join(home, ".config", "autostart", "proton-git-lfs.desktop"))
		return err == nil
	default:
		return false
	}
}

// setAutoStart enables or disables launch-at-login.
func setAutoStart(enable bool) error {
	switch runtime.GOOS {
	case "darwin":
		return setAutoStartDarwin(enable)
	case "linux":
		return setAutoStartLinux(enable)
	default:
		return fmt.Errorf("autostart not supported on %s", runtime.GOOS)
	}
}

func setAutoStartDarwin(enable bool) error {
	p := launchAgentPath()
	if p == "" {
		return fmt.Errorf("cannot determine LaunchAgent path")
	}
	if !enable {
		return os.Remove(p)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <false/>
  <key>ProcessType</key>
  <string>Interactive</string>
</dict>
</plist>
`, launchAgentLabel, exe)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(plist), 0o644)
}

func setAutoStartLinux(enable bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	p := filepath.Join(home, ".config", "autostart", "proton-git-lfs.desktop")
	if !enable {
		return os.Remove(p)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Proton Git LFS
Exec=%s
Comment=System tray for Proton Git LFS
Categories=Development;
StartupNotify=false
`, exe)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(entry), 0o644)
}
