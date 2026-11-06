package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// gitLFSConfig holds the global git setting the tray reads.
type gitLFSConfig struct {
	StandaloneAgent string // lfs.standalonetransferagent
}

// gitConfigCache keeps the last gitLFSConfig with a size and mtime stamp of
// the global config files, so the poll loop spawns git only after a change.
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

// globalGitConfigFiles lists the files git reads for --global (git-config(1)
// FILES): $GIT_CONFIG_GLOBAL when set, otherwise $XDG_CONFIG_HOME/git/config
// (default ~/.config/git/config) and ~/.gitconfig.
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

// queryGitLFSConfig reads the registration with `git config --global --get`,
// which exits with status 1 when the key is unset (git-config(1)).
func queryGitLFSConfig() gitLFSConfig {
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "config", "--global", "--get", gitKeyStandaloneAgent)
	cmd.WaitDelay = subprocessWaitDelay
	out, err := cmd.Output()
	if err != nil {
		return gitLFSConfig{}
	}
	return gitLFSConfig{StandaloneAgent: strings.TrimSpace(string(out))}
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
