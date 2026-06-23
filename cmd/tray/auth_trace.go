package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

var (
	bearerLogPattern        = regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9._~+/=-]+`)
	jsonSecretPattern       = regexp.MustCompile(`(?i)("?(?:access|refresh|session|password|secret|token|uid)[A-Za-z0-9_-]*"?\s*:\s*")[^"]+(")`)
	assignmentSecretPattern = regexp.MustCompile(`(?i)\b((?:access|refresh|session|password|secret|token)[A-Za-z0-9_-]*=)[^&\s]+`)
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
	text = jsonSecretPattern.ReplaceAllString(text, `${1}[redacted]${2}`)
	return assignmentSecretPattern.ReplaceAllString(text, `${1}[redacted]`)
}
