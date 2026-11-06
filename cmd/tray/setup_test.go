package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
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

func TestLaunchAgentPathUsesBundleIdentifier(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := launchAgentPath()
	if p == "" {
		t.Fatal("expected non-empty path")
	}
	if !strings.HasSuffix(p, "com.proton.lfs-tray.plist") {
		t.Fatalf("expected plist filename, got %q", p)
	}
	if !strings.Contains(p, filepath.Join("Library", "LaunchAgents")) {
		t.Fatalf("expected LaunchAgents dir, got %q", p)
	}
}

// hostilePaths are executable paths with characters that break naive
// string interpolation into XML or a desktop entry.
var hostilePaths = []string{
	"/Applications/ProtonLFS.app/Contents/MacOS/proton-lfs-tray",
	"/Applications/Proton LFS.app/Contents/MacOS/proton-lfs-tray",
	"/opt/tools & more/proton-lfs-tray",
	"/home/o'brien/bin/proton-lfs-tray",
	`/srv/"quoted"/proton-lfs-tray`,
	"/srv/<tag>/proton-lfs-tray",
	"/srv/$HOME/`id`/proton-lfs-tray",
	`/srv/back\slash/proton-lfs-tray`,
	"/srv/100%/proton-lfs-tray",
}

// plistStrings returns the text of every <string> element in document
// order, decoded by encoding/xml.
func plistStrings(t *testing.T, doc []byte) []string {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(doc))
	var out []string
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("plist is not well-formed XML: %v\n%s", err, doc)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "string" {
			continue
		}
		var text string
		if err := dec.DecodeElement(&text, &start); err != nil {
			t.Fatalf("decode <string>: %v", err)
		}
		out = append(out, text)
	}
}

func TestLaunchAgentPlistEscapesProgramPath(t *testing.T) {
	for _, program := range hostilePaths {
		t.Run(program, func(t *testing.T) {
			doc, err := launchAgentPlist(launchAgentLabel, program)
			if err != nil {
				t.Fatal(err)
			}
			strs := plistStrings(t, doc)
			if len(strs) != 3 || strs[0] != launchAgentLabel || strs[1] != program || strs[2] != "Interactive" {
				t.Fatalf("decoded strings = %q, want label, program and process type", strs)
			}
			if !bytes.Contains(doc, []byte("<key>RunAtLoad</key>")) || !bytes.Contains(doc, []byte("<key>ProgramArguments</key>")) {
				t.Fatalf("plist missing launchd keys:\n%s", doc)
			}
			lintPlist(t, doc)
		})
	}
}

// lintPlist runs plutil -lint when it is available (macOS).
func lintPlist(t *testing.T, doc []byte) {
	t.Helper()
	plutil, err := exec.LookPath("plutil")
	if err != nil {
		return
	}
	path := filepath.Join(t.TempDir(), "agent.plist")
	if err := os.WriteFile(path, doc, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(plutil, "-lint", path).CombinedOutput(); err != nil {
		t.Fatalf("plutil -lint: %v\n%s", err, out)
	}
}

func TestSetAutoStartDarwinWritesAndRemovesPlist(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only: setAutoStartDarwin uses the running executable")
	}
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := setAutoStartDarwin(true); err != nil {
		t.Fatalf("setAutoStartDarwin(true) failed: %v", err)
	}
	plistPath := filepath.Join(tmpHome, "Library", "LaunchAgents", "com.proton.lfs-tray.plist")
	content, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("plist not created: %v", err)
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	if strs := plistStrings(t, content); len(strs) < 2 || strs[1] != exe {
		t.Fatalf("plist program = %q, want %q", strs, exe)
	}
	info, _ := os.Stat(plistPath)
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("expected 0644 perms, got %o", perm)
	}
	lintPlist(t, content)

	if err := setAutoStartDarwin(false); err != nil {
		t.Fatalf("disable failed: %v", err)
	}
	if _, err := os.Stat(plistPath); err == nil {
		t.Fatal("plist should be removed after disable")
	}
}

// parseDesktopExec is the inverse of desktopExecValue for a single-argument
// Exec line: it undoes the string escaping, then the Exec quoting, so the
// test verifies the round trip rather than the literal text.
func parseDesktopExec(t *testing.T, value string) []string {
	t.Helper()
	unescaped := strings.NewReplacer(`\s`, " ", `\n`, "\n", `\t`, "\t", `\r`, "\r", `\\`, `\`).Replace(value)
	var args []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(unescaped); i++ {
		c := unescaped[i]
		switch {
		case inQuote && c == '\\' && i+1 < len(unescaped):
			i++
			cur.WriteByte(unescaped[i])
		case c == '"':
			inQuote = !inQuote
		case !inQuote && c == ' ':
			args = append(args, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	args = append(args, cur.String())
	for i := range args {
		args[i] = strings.ReplaceAll(args[i], "%%", "%")
	}
	return args
}

func TestDesktopEntryQuotesExecPath(t *testing.T) {
	for _, program := range hostilePaths {
		t.Run(program, func(t *testing.T) {
			entry := desktopEntry(program)
			var execLine string
			for _, line := range strings.Split(entry, "\n") {
				if strings.HasPrefix(line, "Exec=") {
					execLine = strings.TrimPrefix(line, "Exec=")
				}
			}
			if execLine == "" {
				t.Fatalf("no Exec line:\n%s", entry)
			}
			if got := parseDesktopExec(t, execLine); len(got) != 1 || got[0] != program {
				t.Fatalf("Exec=%s parses to %q, want %q", execLine, got, program)
			}
			if !strings.HasPrefix(entry, "[Desktop Entry]\n") || !strings.Contains(entry, "Type=Application\n") {
				t.Fatalf("desktop entry malformed:\n%s", entry)
			}
			validateDesktopEntry(t, entry)
		})
	}
}

func TestDesktopExecArgQuotingFollowsSpecification(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/tray":          "/usr/bin/tray",
		"/opt/a b/tray":          `"/opt/a b/tray"`,
		`/opt/q"t/tray`:          `"/opt/q\"t/tray"`,
		"/opt/$x/tray":           `"/opt/\$x/tray"`,
		"/opt/`x`/tray":          "\"/opt/\\`x\\`/tray\"",
		`/opt/b\s/tray`:          `"/opt/b\\s/tray"`,
		"/opt/100%/tray":         "/opt/100%%/tray",
		"/opt/o'brien/tray":      `"/opt/o'brien/tray"`,
		"/opt/a&b;c|d<e>f~g/tr":  `"/opt/a&b;c|d<e>f~g/tr"`,
		"/opt/x(y)z*w?v#u/tray":  `"/opt/x(y)z*w?v#u/tray"`,
		"":                       `""`,
		"/opt/äöü/tray":          "/opt/äöü/tray",
		"/opt/tab\there/tray":    "\"/opt/tab\there/tray\"",
		"/opt/new\nline/tray":    "\"/opt/new\nline/tray\"",
		"/opt/plain-dir_1/tray":  "/opt/plain-dir_1/tray",
		"/opt/[brackets]/tray":   "/opt/[brackets]/tray",
		"/opt/{braces}/tray":     "/opt/{braces}/tray",
		"/opt/comma,colon:/tray": "/opt/comma,colon:/tray",
	}
	for arg, want := range cases {
		if got := desktopExecArg(arg); got != want {
			t.Errorf("desktopExecArg(%q) = %s, want %s", arg, got, want)
		}
	}
	if got := desktopStringValue(`a\b` + "\n" + "c\td\r"); got != `a\\b\nc\td\r` {
		t.Errorf("desktopStringValue = %q", got)
	}
}

// validateDesktopEntry runs desktop-file-validate when it is installed.
func validateDesktopEntry(t *testing.T, entry string) {
	t.Helper()
	validator, err := exec.LookPath("desktop-file-validate")
	if err != nil {
		return
	}
	path := filepath.Join(t.TempDir(), autostartDesktopFileName)
	if err := os.WriteFile(path, []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(validator, path).CombinedOutput(); err != nil {
		t.Fatalf("desktop-file-validate: %v\n%s", err, out)
	}
}

func TestSetAutoStartLinuxWritesXDGAutostartEntry(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only: setAutoStartLinux uses the running executable")
	}
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	configHome := filepath.Join(tmpHome, "xdg config with space")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	if err := setAutoStartLinux(true); err != nil {
		t.Fatalf("setAutoStartLinux(true) failed: %v", err)
	}

	desktopPath := filepath.Join(configHome, "autostart", autostartDesktopFileName)
	content, err := os.ReadFile(desktopPath)
	if err != nil {
		t.Fatalf("desktop entry not created under XDG_CONFIG_HOME: %v", err)
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	if string(content) != desktopEntry(exe) {
		t.Fatalf("unexpected desktop entry:\n%s", content)
	}
	if !isAutoStartEnabled() {
		t.Fatal("isAutoStartEnabled should see the entry it wrote")
	}
	if err := setAutoStartLinux(false); err != nil {
		t.Fatalf("disable failed: %v", err)
	}
	if isAutoStartEnabled() {
		t.Fatal("entry should be removed after disable")
	}
}

func TestIsAutoStartEnabled(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))

	if isAutoStartEnabled() {
		t.Fatal("expected false when no autostart file exists")
	}

	switch runtime.GOOS {
	case "darwin":
		plistDir := filepath.Join(tmpHome, "Library", "LaunchAgents")
		if err := os.MkdirAll(plistDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(plistDir, launchAgentLabel+".plist"), []byte("<plist/>"), 0o644); err != nil {
			t.Fatal(err)
		}
	case "linux":
		autoDir := filepath.Join(tmpHome, ".config", "autostart")
		if err := os.MkdirAll(autoDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(autoDir, autostartDesktopFileName), []byte("[Desktop Entry]"), 0o644); err != nil {
			t.Fatal(err)
		}
	default:
		t.Skip("unsupported OS for autostart test")
	}

	if !isAutoStartEnabled() {
		t.Fatal("expected true after creating autostart file")
	}
}

func TestSetAutoStartDarwinRemovesFormerLabel(t *testing.T) {
	home := setupFakeHome(t, fakeHomeOpts{})
	agents := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	former := filepath.Join(agents, formerLaunchAgentLabel+".plist")
	if err := os.WriteFile(former, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !launchAgentInstalled() {
		t.Fatal("a plist under the former label must count as installed")
	}

	if err := setAutoStartDarwin(false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := os.Stat(former); !os.IsNotExist(err) {
		t.Fatalf("former plist still present: %v", err)
	}
	if launchAgentInstalled() {
		t.Fatal("no agent plist should remain after disabling")
	}
}
