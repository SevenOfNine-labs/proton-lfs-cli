package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	bearerLogPattern = regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9._~+/=-]+`)
	// secretFieldPattern matches a secret-named key followed by ':' or '=' and
	// its value in JSON, JavaScript object, query-string and log forms
	// ("accessToken":"v", { accessToken: 'v' }, refresh_token=v).
	secretFieldPattern = regexp.MustCompile(`(?i)(\b[A-Za-z0-9_-]*(?:access|refresh|session|password|secret|token|uid)[A-Za-z0-9_-]*['"]?\s*[:=]\s*)('[^']*'|"[^"]*"|[^\s,;&}\]]+)`)
	// secretWordPattern matches "AccessToken <value>"-style log lines.
	secretWordPattern = regexp.MustCompile(`(?i)\b((?:access|refresh|session)[_-]?token\s+)[^\s,;:=]+`)
)

func newAuthTraceID() string {
	return fmt.Sprintf("tray-%d-%d", os.Getpid(), time.Now().UnixNano())
}

func withAuthTraceEnv(env []string, traceID string) []string {
	if strings.TrimSpace(traceID) == "" {
		return env
	}
	return append(env,
		"PROTON_AUTH_TRACE=1",
		"PROTON_AUTH_TRACE_ID="+traceID,
	)
}

func logSubprocessOutput(prefix string, out []byte) {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return
	}
	trayLog.Printf("%s output:\n%s", prefix, redactSubprocessOutput(text))
}

func redactSubprocessOutput(text string) string {
	text = bearerLogPattern.ReplaceAllString(text, `${1}[redacted]`)
	text = secretFieldPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := secretFieldPattern.FindStringSubmatch(match)
		value := parts[2]
		// Keep the quotes so redacted JSON stays well-formed.
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') {
			return parts[1] + string(value[0]) + "[redacted]" + string(value[0])
		}
		return parts[1] + "[redacted]"
	})
	return secretWordPattern.ReplaceAllString(text, `${1}[redacted]`)
}
