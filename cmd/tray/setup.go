package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
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

// discoverPassCLIBinary returns the absolute path of pass-cli from PATH or,
// for launchd's minimal PATH, from ~/.local/bin and the Homebrew prefixes;
// it returns "" when none exists.
func discoverPassCLIBinary() string {
	path, err := exec.LookPath("pass-cli")
	if err == nil {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	candidates := []string{
		filepath.Join(home, ".local", "bin", "pass-cli"),
		"/opt/homebrew/bin/pass-cli",
		"/usr/local/bin/pass-cli",
	}

	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if info.Mode()&0111 != 0 {
				return candidate
			}
		}
	}

	return ""
}

// launchAgentLabel is the launchd label of the autostart agent and the
// CFBundleIdentifier of the .app bundle (scripts/ensure-info-plist.sh and
// scripts/package-bundle.sh use the same identifier).
const launchAgentLabel = "com.proton.lfs-tray"

// autostartDesktopFileName is the XDG autostart entry written on Linux;
// scripts/package-bundle.sh ships a file of the same name in the bundle.
const autostartDesktopFileName = "proton-lfs.desktop"

// launchAgentPath returns the path to the macOS LaunchAgent plist.
func launchAgentPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

// linuxAutostartPath returns the XDG autostart entry path:
// $XDG_CONFIG_HOME/autostart (os.UserConfigDir, default ~/.config) per the
// Desktop Application Autostart Specification.
func linuxAutostartPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "autostart", autostartDesktopFileName), nil
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
		p, err := linuxAutostartPath()
		if err != nil {
			return false
		}
		_, err = os.Stat(p)
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

// launchAgentTemplate is a launchd.plist(5) property list. String values
// are escaped with xml.EscapeText so a path containing &, < or " stays a
// valid document.
var launchAgentTemplate = template.Must(template.New("launchagent").Funcs(template.FuncMap{
	"xml": xmlEscape,
}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{xml .Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{xml .Program}}</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`))

func xmlEscape(s string) (string, error) {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(s)); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// launchAgentPlist renders the LaunchAgent that starts program at login.
func launchAgentPlist(label, program string) ([]byte, error) {
	var buf bytes.Buffer
	err := launchAgentTemplate.Execute(&buf, struct{ Label, Program string }{label, program})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func setAutoStartDarwin(enable bool) error {
	p := launchAgentPath()
	if p == "" {
		return fmt.Errorf("cannot determine LaunchAgent path")
	}
	if !enable {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	plist, err := launchAgentPlist(launchAgentLabel, exe)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, plist, 0o644)
}

// desktopEntry renders a Desktop Entry Specification 1.5 file that runs
// program with no arguments.
func desktopEntry(program string) string {
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=Proton LFS\n" +
		"Exec=" + desktopExecValue([]string{program}) + "\n" +
		"Comment=System tray for Proton LFS\n" +
		"Categories=Development;\n" +
		"StartupNotify=false\n"
}

// desktopExecReserved lists the characters that the Desktop Entry
// Specification 1.5 ("The Exec key") reserves; an argument containing any
// of them must be quoted.
const desktopExecReserved = " \t\n\"'\\><~|&;$*?#()`"

// desktopExecValue renders argv as an Exec value: Desktop Entry
// Specification 1.5 ("The Exec key") argument quoting and %% for a literal %,
// then the string escaping of the file format (desktopStringValue).
func desktopExecValue(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, desktopExecArg(arg))
	}
	return desktopStringValue(strings.Join(quoted, " "))
}

func desktopExecArg(arg string) string {
	arg = strings.ReplaceAll(arg, "%", "%%")
	if arg != "" && !strings.ContainsAny(arg, desktopExecReserved) {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range arg {
		switch r {
		case '"', '`', '$', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}

// desktopStringValue applies the escape sequences of values of type string
// (\s, \n, \t, \r and \\) from the Desktop Entry Specification 1.5.
func desktopStringValue(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		"\n", `\n`,
		"\t", `\t`,
		"\r", `\r`,
	).Replace(s)
}

func setAutoStartLinux(enable bool) error {
	p, err := linuxAutostartPath()
	if err != nil {
		return err
	}
	if !enable {
		return os.Remove(p)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(desktopEntry(exe)), 0o644)
}
