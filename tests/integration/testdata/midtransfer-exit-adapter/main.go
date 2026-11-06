package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"strings"
	"time"
)

type inboundMessage struct {
	Event string `json:"event"`
	OID   string `json:"oid,omitempty"`
}

type outboundMessage struct {
	Event string `json:"event,omitempty"`
	OID   string `json:"oid,omitempty"`
	Path  string `json:"path,omitempty"`
}

func shouldExit(event, exitOn string) bool {
	switch exitOn {
	case "both":
		return event == "upload" || event == "download"
	case "upload":
		return event == "upload"
	case "download":
		return event == "download"
	default:
		return false
	}
}

func main() {
	exitOn := flag.String("exit-on", "both", "transfer event after which the adapter exits without completing: upload, download, or both")
	delayMS := flag.Int("delay-ms", 15000, "delay before exiting, in milliseconds")
	flag.Parse()

	mode := strings.ToLower(strings.TrimSpace(*exitOn))
	if mode == "" {
		mode = "both"
	}
	delay := time.Duration(*delayMS) * time.Millisecond
	if delay <= 0 {
		delay = 15 * time.Second
	}

	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	// Git LFS custom transfer specification, stage 1: init, answered with {}.
	var initMsg inboundMessage
	if err := dec.Decode(&initMsg); err != nil {
		return
	}
	if initMsg.Event != "init" {
		return
	}
	if err := enc.Encode(map[string]any{}); err != nil {
		return
	}

	for {
		var msg inboundMessage
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				return
			}
			return
		}

		switch msg.Event {
		case "terminate":
			return
		case "upload", "download":
			if shouldExit(msg.Event, mode) {
				// Exit without a complete message: git-lfs must detect the
				// closed stdout and fail the transfer.
				time.Sleep(delay)
				return
			}
			_ = enc.Encode(outboundMessage{
				Event: "complete",
				OID:   msg.OID,
			})
		}
	}
}
