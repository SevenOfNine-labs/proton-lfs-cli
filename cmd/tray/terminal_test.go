//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTerminalCommandDarwinWritesSelfDeletingScript(t *testing.T) {
	setupTrayLogForTest(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	cmd, cleanup := terminalCommandDarwin("echo hello")
	if cmd == nil || cleanup == nil {
		t.Fatal("expected a command and a cleanup")
	}
	scriptPath := cmd.Args[len(cmd.Args)-1]
	if filepath.Dir(scriptPath) != tmp {
		t.Fatalf("script %q must be created under TMPDIR %q", scriptPath, tmp)
	}
	if !strings.HasPrefix(filepath.Base(scriptPath), "proton-lfs-") {
		t.Fatalf("unexpected script name %q", scriptPath)
	}
	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "echo hello\n") || !strings.Contains(string(content), `rm -f "$0"`) {
		t.Fatalf("script content:\n%s", content)
	}
	if info, _ := os.Stat(scriptPath); info.Mode().Perm() != 0o700 {
		t.Fatalf("script mode = %o, want 0700", info.Mode().Perm())
	}

	// The script path is an argv item, not part of the AppleScript source.
	if filepath.Base(cmd.Path) != "osascript" && !strings.HasSuffix(cmd.Path, "osascript") {
		t.Fatalf("expected osascript, got %q", cmd.Path)
	}
	for i, arg := range cmd.Args[:len(cmd.Args)-1] {
		if strings.Contains(arg, scriptPath) {
			t.Fatalf("arg %d interpolates the script path into AppleScript: %q", i, arg)
		}
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "on run argv") {
		t.Fatalf("AppleScript must take the path from argv: %v", cmd.Args)
	}

	cleanup()
	if _, err := os.Stat(scriptPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup must remove the script, stat err=%v", err)
	}
}

func TestLaunchTerminalRemovesScriptWhenStartFails(t *testing.T) {
	setupTrayLogForTest(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	_, cleanup := terminalCommandDarwin("echo hello")
	if cleanup == nil {
		t.Fatal("expected cleanup")
	}
	cmd := exec.Command(filepath.Join(tmp, "missing-osascript"))
	failed := make(chan struct{}, 1)
	launchTerminal(cmd, cleanup, func() { failed <- struct{}{} })

	select {
	case <-failed:
	case <-time.After(2 * time.Second):
		t.Fatal("onFailure must run when the terminal cannot be started")
	}
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "proton-lfs-") {
			t.Fatalf("script %s left behind after failed start", e.Name())
		}
	}
}

func TestLaunchTerminalReportsNonZeroExit(t *testing.T) {
	setupTrayLogForTest(t)
	failed := make(chan struct{}, 1)
	launchTerminal(exec.Command("false"), nil, func() { failed <- struct{}{} })
	select {
	case <-failed:
	case <-time.After(5 * time.Second):
		t.Fatal("onFailure must run when the terminal exits non-zero")
	}

	ok := make(chan struct{}, 1)
	launchTerminal(exec.Command("true"), nil, func() { ok <- struct{}{} })
	select {
	case <-ok:
		t.Fatal("onFailure must not run for a clean exit")
	case <-time.After(500 * time.Millisecond):
	}
}

func TestTerminalCommandLinuxArgumentConventions(t *testing.T) {
	cases := []struct {
		emulator string
		want     []string
	}{
		{"gnome-terminal", []string{"--", "bash", "-c", "tail -f log"}},
		{"xterm", []string{"-e", "bash", "-c", "tail -f log"}},
		{"x-terminal-emulator", []string{"-e", "bash", "-c", "tail -f log"}},
	}
	for _, tc := range cases {
		t.Run(tc.emulator, func(t *testing.T) {
			bin := t.TempDir()
			fake := filepath.Join(bin, tc.emulator)
			if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			cmd := terminalCommandLinux("tail -f log")
			if cmd == nil {
				t.Fatalf("expected %s to be found", tc.emulator)
			}
			if cmd.Path != fake {
				t.Fatalf("path = %q, want %q", cmd.Path, fake)
			}
			if got := cmd.Args[1:]; strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Fatalf("args = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("none", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if cmd := terminalCommandLinux("x"); cmd != nil {
			t.Fatalf("expected nil without an emulator, got %v", cmd.Args)
		}
	})
}

func TestDarwinNotificationCommandPassesMessageAsArgv(t *testing.T) {
	msg := `Logs: /tmp/it's "quoted" \ done`
	cmd := darwinNotificationCommand(msg)
	if cmd.Args[len(cmd.Args)-1] != msg {
		t.Fatalf("message must be the last argv item: %v", cmd.Args)
	}
	for _, arg := range cmd.Args[:len(cmd.Args)-1] {
		if strings.Contains(arg, "quoted") {
			t.Fatalf("message interpolated into AppleScript source: %q", arg)
		}
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "item 1 of argv") {
		t.Fatalf("AppleScript must read the message from argv: %v", cmd.Args)
	}
}
