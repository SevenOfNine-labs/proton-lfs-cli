//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTerminateActiveKillsRunningBridge(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "grandchild.pid")
	// The bridge stand-in starts a grandchild (as proton-drive-cli starts git
	// or pass-cli) and blocks; both must die with the process group.
	script := filepath.Join(dir, "proton-drive-cli")
	body := "#!/bin/sh\nsleep 30 &\necho $! > " + shellQuote(pidFile) + "\nwait\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	bc := NewBridgeClient(BridgeClientConfig{CLIBin: script, Timeout: time.Minute})

	done := make(chan error, 1)
	go func() {
		_, err := bc.Exists(validOID)
		done <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		bc.mu.Lock()
		running := bc.active != nil && bc.active.Process != nil
		bc.mu.Unlock()
		if _, err := os.Stat(pidFile); running && err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bridge subprocess did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	bc.TerminateActive()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from the killed bridge")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("bridge command did not return after TerminateActive")
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for wait := time.Now().Add(5 * time.Second); ; {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return
		}
		if time.Now().After(wait) {
			t.Fatalf("grandchild %d survived the process-group kill", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
