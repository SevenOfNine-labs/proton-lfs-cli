package bridgeproto

import (
	"runtime"
	"strings"
)

// envAllowlist is the complete set of variables forwarded to the bridge
// (docs/security/threat-model.md); an entry ending in "_" is a prefix.
var envAllowlist = []string{
	"PATH",
	"HOME",
	"USER",
	"SHELL",
	"LANG",
	"LC_",
	"TERM",
	"NODE_ENV",
	"NODE_OPTIONS",
	"NODE_PATH",
	"NODE_BIN",
	"XDG_CONFIG_HOME",
	"XDG_DATA_HOME",
	"XDG_CACHE_HOME",
	"XDG_RUNTIME_DIR",
	"MOCK_BRIDGE_",
	// Variables the proton-drive-cli bridge reads (src/constants.ts,
	// src/credentials/pass-cli.ts, src/utils/auth-trace.ts).
	"PROTON_DRIVE_CLI_APP_VERSION",
	"PROTON_PASS_CLI_BIN",
	"PROTON_AUTH_TRACE",
	"PROTON_AUTH_TRACE_ID",
	// Proton Pass CLI configuration for the pass-cli process the bridge starts
	// (pass-cli docs/public/docs/get-started/configuration.md).
	"PROTON_PASS_SESSION_DIR",
	"PROTON_PASS_KEY_PROVIDER",
	"PROTON_PASS_ENCRYPTION_KEY",
	"PROTON_PASS_LINUX_KEYRING",
	// Secret Service clients (pass-cli keyring, git credential-libsecret)
	// connect to the session bus at this address (D-Bus Specification).
	"DBUS_SESSION_BUS_ADDRESS",
	// The git-credential provider runs `git credential`, which reads this
	// file (git(1), ENVIRONMENT) and Git Credential Manager's GCM_* settings
	// (git-credential-manager docs/environment.md).
	"GIT_CONFIG_GLOBAL",
	"GCM_",
	"TMPDIR",
	"TMP",
	"TEMP",
	// Windows locates the user profile, git's global configuration and
	// system DLLs through these; they carry no secrets.
	"USERPROFILE",
	"HOMEDRIVE",
	"HOMEPATH",
	"APPDATA",
	"LOCALAPPDATA",
	"SYSTEMROOT",
	"WINDIR",
	"COMSPEC",
	"PATHEXT",
}

// MatchesAllowlist reports whether the variable key reaches the bridge.
// Windows environment names are case-insensitive (Path, ComSpec, windir).
func MatchesAllowlist(key string) bool {
	return matchesAllowlist(key, runtime.GOOS == "windows")
}

func matchesAllowlist(key string, caseInsensitive bool) bool {
	if caseInsensitive {
		key = strings.ToUpper(key)
	}
	for _, allowed := range envAllowlist {
		if strings.HasSuffix(allowed, "_") {
			if strings.HasPrefix(key, allowed) {
				return true
			}
		} else if key == allowed {
			return true
		}
	}
	return false
}

// FilterEnv returns the entries of environ ("KEY=value") whose key matches
// the allowlist.
func FilterEnv(environ []string) []string {
	filtered := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if MatchesAllowlist(key) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
