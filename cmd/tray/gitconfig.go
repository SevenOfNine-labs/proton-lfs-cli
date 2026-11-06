package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// gitCommandTimeout bounds the git invocations the tray makes to read and
// write its global LFS registration; they touch only local files.
const gitCommandTimeout = 30 * time.Second

const (
	gitKeyStandaloneAgent = "lfs.standalonetransferagent"
	gitKeyTransferPath    = "lfs.customtransfer.proton.path"
	gitKeyTransferArgs    = "lfs.customtransfer.proton.args"
	gitStandaloneAgent    = "proton"
)

// gitLFSConfig holds the two global git settings the tray reads.
type gitLFSConfig struct {
	StandaloneAgent string // lfs.standalonetransferagent
	TransferArgs    string // lfs.customtransfer.proton.args
}

// gitConfigCache keeps the last gitLFSConfig together with a stamp of the
// global config files (size and mtime). The poll loop runs every 5 s and
// must not spawn git on every tick; the values are re-read only when a
// file changes or after the tray itself writes the registration.
type gitConfigCache struct {
	mu    sync.Mutex
	stamp string
	value gitLFSConfig
}

var (
	gitLFSConfigCache gitConfigCache
	// queryGitLFSConfigFn is swapped by tests to count git invocations.
	queryGitLFSConfigFn = queryGitLFSConfig
)

// globalGitConfigFiles lists the files git reads for --global, per
// git-config(1) FILES: $GIT_CONFIG_GLOBAL when set, otherwise
// $XDG_CONFIG_HOME/git/config (default ~/.config/git/config) and
// ~/.gitconfig, both of which may exist.
func globalGitConfigFiles() []string {
	if p := strings.TrimSpace(os.Getenv("GIT_CONFIG_GLOBAL")); p != "" {
		return []string{p}
	}
	var files []string
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	files = append(files, filepath.Join(xdg, "git", "config"), filepath.Join(home, ".gitconfig"))
	return files
}

func gitConfigStamp(files []string) string {
	var b strings.Builder
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			fmt.Fprintf(&b, "%s|missing;", f)
			continue
		}
		fmt.Fprintf(&b, "%s|%d|%d;", f, info.Size(), info.ModTime().UnixNano())
	}
	return b.String()
}

// readGitLFSConfig returns the tray's global git LFS settings, re-reading
// them only when a global config file changed since the last call.
func readGitLFSConfig() gitLFSConfig {
	stamp := gitConfigStamp(globalGitConfigFiles())
	c := &gitLFSConfigCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if stamp != "" && c.stamp == stamp {
		return c.value
	}
	c.value = queryGitLFSConfigFn()
	c.stamp = stamp
	return c.value
}

// invalidateGitLFSConfig forces the next readGitLFSConfig to query git.
func invalidateGitLFSConfig() {
	gitLFSConfigCache.mu.Lock()
	gitLFSConfigCache.stamp = ""
	gitLFSConfigCache.mu.Unlock()
}

// queryGitLFSConfig reads both keys with a single git invocation. git exits
// with status 1 when no key matches, which is the unregistered state.
func queryGitLFSConfig() gitLFSConfig {
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "config", "--global", "--get-regexp",
		`^lfs\.(standalonetransferagent|customtransfer\.proton\.args)$`)
	cmd.WaitDelay = subprocessWaitDelay
	out, err := cmd.Output()
	if err != nil {
		return gitLFSConfig{}
	}
	var cfg gitLFSConfig
	for _, line := range strings.Split(string(out), "\n") {
		key, value, _ := strings.Cut(line, " ")
		switch key {
		case gitKeyStandaloneAgent:
			cfg.StandaloneAgent = strings.TrimSpace(value)
		case gitKeyTransferArgs:
			cfg.TransferArgs = strings.TrimSpace(value)
		}
	}
	return cfg
}

// writeGitLFSRegistration points git-lfs at the adapter in the global
// config. Both the CLI `register` command and the tray menu item call it,
// so they always write identical settings.
func writeGitLFSRegistration(adapterPath, transferArgs string) error {
	for _, kv := range [][2]string{
		{gitKeyTransferPath, adapterPath},
		{gitKeyTransferArgs, transferArgs},
		{gitKeyStandaloneAgent, gitStandaloneAgent},
	} {
		if err := runGitConfigGlobal(kv[0], kv[1]); err != nil {
			return err
		}
	}
	invalidateGitLFSConfig()
	return nil
}

func runGitConfigGlobal(key, value string) error {
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "config", "--global", key, value)
	cmd.WaitDelay = subprocessWaitDelay
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config --global %s: %w: %s", key, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// formerCredentialProviderArg matches the --credential-provider option that
// registrations written before 2026-11 put into lfs.customtransfer.proton.args;
// the adapter no longer defines it and refuses to start with it.
var formerCredentialProviderArg = regexp.MustCompile(`\s*--credential-provider(?:=|\s+)(?:'[^']*'|"[^"]*"|\S+)`)

// migrateGitLFSRegistration removes --credential-provider from an existing
// registration and keeps every other argument unchanged.
func migrateGitLFSRegistration() error {
	cfg := readGitLFSConfig()
	if !formerCredentialProviderArg.MatchString(cfg.TransferArgs) {
		return nil
	}
	args := strings.TrimSpace(formerCredentialProviderArg.ReplaceAllString(cfg.TransferArgs, ""))
	if err := runGitConfigGlobal(gitKeyTransferArgs, args); err != nil {
		return err
	}
	invalidateGitLFSConfig()
	trayLog.Printf("git config: removed --credential-provider from %s", gitKeyTransferArgs)
	return nil
}
