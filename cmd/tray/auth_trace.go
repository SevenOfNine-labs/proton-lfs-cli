package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/SevenOfNine-labs/proton-lfs-cli/internal/bridgeproto"
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

// maxLoggedSubprocessOutput bounds what one call writes to the tray log;
// redaction runs before that cut, on at most 16 times as many runes.
const maxLoggedSubprocessOutput = 4096

func logSubprocessOutput(prefix string, out []byte) {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return
	}
	bounded := bridgeproto.Truncate(text, 16*maxLoggedSubprocessOutput)
	trayLog.Printf("%s output:\n%s", prefix, bridgeproto.Truncate(bridgeproto.RedactText(bounded), maxLoggedSubprocessOutput))
}
