package main

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRelativeTime(t *testing.T) {
	cases := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"just now", 0, "just now"},
		{"30 seconds", 30 * time.Second, "just now"},
		{"59 seconds", 59 * time.Second, "just now"},
		{"1 minute", 1 * time.Minute, "1m ago"},
		{"5 minutes", 5 * time.Minute, "5m ago"},
		{"30 minutes", 30 * time.Minute, "30m ago"},
		{"59 minutes", 59 * time.Minute, "59m ago"},
		{"1 hour", 1 * time.Hour, "1h ago"},
		{"2 hours", 2 * time.Hour, "2h ago"},
		{"23 hours", 23 * time.Hour, "23h ago"},
		{"1 day", 24 * time.Hour, "1d ago"},
		{"2 days", 48 * time.Hour, "2d ago"},
		{"7 days", 7 * 24 * time.Hour, "7d ago"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			past := time.Now().Add(-tc.duration)
			got := relativeTime(past)
			if got != tc.expected {
				t.Fatalf("relativeTime(%v ago) = %q, expected %q", tc.duration, got, tc.expected)
			}
		})
	}
}

func TestRelativeTimeFutureTimestamp(t *testing.T) {
	future := time.Now().Add(1 * time.Hour)
	got := relativeTime(future)
	if got != "just now" {
		t.Fatalf("expected 'just now' for future time, got %q", got)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		limit    int
		expected string
	}{
		{"short", "abc", 10, "abc"},
		{"exact", "abcde", 5, "abcde"},
		{"truncated", "abcdefgh", 5, "abcd…"},
		{"limit=1", "abc", 1, "…"},
		{"empty", "", 5, ""},
		{"boundary", "abcde", 6, "abcde"},
		{"boundary-exact", "abcdef", 6, "abcdef"},
		{"long", "this is a long string that should be truncated", 10, "this is a…"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.input, tc.limit)
			if got != tc.expected {
				t.Fatalf("truncate(%q, %d) = %q, expected %q", tc.input, tc.limit, got, tc.expected)
			}
		})
	}
}

func TestTruncateMultibyteInput(t *testing.T) {
	// truncate uses len() which counts bytes, not runes.
	// "héllo" = 6 bytes (h=1, é=2, l=1, l=1, o=1).
	input := "héllo"
	got := truncate(input, 4)
	// s[:3] = "hé" (3 bytes), then + "…" (3 bytes)
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	// Document byte-slicing behavior: truncation happens at byte boundary
	t.Logf("truncate(%q, 4) = %q (len=%d)", input, got, len(got))
}

func TestTerminalCommandDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only test")
	}
	cmd := terminalCommand("echo hello")
	if cmd == nil {
		t.Fatal("expected non-nil command on darwin")
	}
	if len(cmd.Args) < 3 {
		t.Fatalf("expected at least 3 args, got %v", cmd.Args)
	}
	if cmd.Args[0] != "osascript" {
		t.Fatalf("expected osascript, got %q", cmd.Args[0])
	}
	if cmd.Args[1] != "-e" {
		t.Fatalf("expected -e flag, got %q", cmd.Args[1])
	}
	if !strings.Contains(cmd.Args[2], "echo hello") {
		t.Fatalf("expected script in AppleScript, got %q", cmd.Args[2])
	}
}
