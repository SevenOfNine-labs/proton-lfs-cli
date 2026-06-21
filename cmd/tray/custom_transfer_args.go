package main

import (
	"regexp"
	"strings"

	"proton-lfs-cli/internal/config"
)

var shellWordRe = regexp.MustCompile(`^[A-Za-z0-9_@/.-]+$`)

func buildProtonTransferArgs(provider, driveCLIPath string) string {
	args := []string{"--backend", "sdk"}
	if provider == config.CredentialProviderGitCredential {
		args = append(args, "--credential-provider", "git-credential")
	}
	if driveCLIPath != "" {
		args = append(args, "--drive-cli-bin", driveCLIPath)
	}
	return strings.Join(shellQuoteArgs(args), " ")
}

func shellQuoteArgs(args []string) []string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuoteArg(arg))
	}
	return quoted
}

func shellQuoteArg(arg string) string {
	if arg != "" && shellWordRe.MatchString(arg) {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
}
