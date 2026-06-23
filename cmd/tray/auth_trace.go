package main

import (
	"fmt"
	"os"
	"strings"
	"time"
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
	trayLog.Printf("%s output:\n%s", prefix, text)
}
