package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTryInstanceLockRejectsSecondHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), trayLockFileName)

	first, locked, err := tryInstanceLock(path)
	if err != nil || !locked {
		t.Fatalf("first lock: locked=%v err=%v", locked, err)
	}

	if _, locked, err := tryInstanceLock(path); err != nil || locked {
		t.Fatalf("second holder must be rejected while the first holds the lock: locked=%v err=%v", locked, err)
	}

	if err := first.Unlock(); err != nil {
		t.Fatal(err)
	}
	second, locked, err := tryInstanceLock(path)
	if err != nil || !locked {
		t.Fatalf("lock must be available again after release: locked=%v err=%v", locked, err)
	}
	_ = second.Unlock()
}

func TestAcquireInstanceLockLivesInAppDir(t *testing.T) {
	home := setupFakeHome(t, fakeHomeOpts{})

	locked, err := acquireInstanceLock()
	if err != nil || !locked {
		t.Fatalf("acquire: locked=%v err=%v", locked, err)
	}
	t.Cleanup(releaseInstanceLock)

	want := filepath.Join(home, ".proton-lfs", trayLockFileName)
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("lock file must live under the shared app dir %s: %v", want, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".proton-lfs-cli")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no separate ~/.proton-lfs-cli directory may be created, stat err=%v", err)
	}

	if locked, err := acquireInstanceLock(); err != nil || locked {
		t.Fatalf("second instance must not acquire the lock: locked=%v err=%v", locked, err)
	}
	releaseInstanceLock()
	if locked, err := acquireInstanceLock(); err != nil || !locked {
		t.Fatalf("lock must be free after release: locked=%v err=%v", locked, err)
	}
}

func writeFakeShell(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake login shell needs a POSIX sh")
	}
	path := filepath.Join(t.TempDir(), "fakeshell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoginShellPathKeepsLastLine(t *testing.T) {
	shell := writeFakeShell(t, "echo 'Welcome banner'\necho 'second banner'\nprintf '%s' '/opt/homebrew/bin:/usr/bin:/bin'\n")

	got, err := loginShellPath(context.Background(), shell)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/opt/homebrew/bin:/usr/bin:/bin" {
		t.Fatalf("loginShellPath = %q, want only the last line", got)
	}
}

func TestLoginShellPathHonoursTimeout(t *testing.T) {
	shell := writeFakeShell(t, "sleep 5\nprintf '%s' /late\n")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := loginShellPath(ctx, shell); err == nil {
		t.Fatal("expected an error when the login shell exceeds the timeout")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("loginShellPath took %s; the timeout must bound it", elapsed)
	}
}

func TestLoginShellPathRunsRealShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	got, err := loginShellPath(context.Background(), "sh")
	if err != nil {
		t.Skipf("sh -l unavailable: %v", err)
	}
	if !strings.Contains(got, string(os.PathListSeparator)) && got == "" {
		t.Fatalf("expected a PATH value, got %q", got)
	}
}
