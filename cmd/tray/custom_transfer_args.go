package main

import (
	"regexp"
	"strings"

	"proton-lfs-cli/internal/config"
)

var shellWordRe = regexp.MustCompile(`^[A-Za-z0-9_@/.-]+$`)

func buildProtonTransferArgs(_ string, driveCLIPath string) string {
	return buildProtonTransferArgsWithData(driveCLIPath, "", "")
}

func buildProtonTransferArgsFromPrefs(prefs config.Preferences, driveCLIPath string) string {
	return buildProtonTransferArgsWithData(
		driveCLIPath,
		prefs.DataCredentialProvider,
		prefs.DataCredentialHost,
	)
}

func buildProtonTransferArgsWithData(driveCLIPath string, dataProvider string, dataHost string) string {
	args := []string{"--backend", "sdk"}
	if driveCLIPath != "" {
		args = append(args, "--drive-cli-bin", driveCLIPath)
	}
	if strings.TrimSpace(dataProvider) != "" {
		args = append(args, "--data-credential-provider", strings.TrimSpace(dataProvider))
		host := strings.TrimSpace(dataHost)
		if host == "" {
			host = config.DefaultDataCredentialHost
		}
		args = append(args, "--data-credential-host", host)
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
