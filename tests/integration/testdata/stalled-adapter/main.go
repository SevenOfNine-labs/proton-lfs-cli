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

func shouldStall(event, stallOn string) bool {
	switch stallOn {
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
	_ = flag.String("local-store-dir", "", "ignored; accepted for argument compatibility with adapter config")
	stallOn := flag.String("stall-on", "both", "transfer event to stall on: upload, download, or both")
	stallMS := flag.Int("stall-ms", 15000, "stall duration in milliseconds")
	flag.Parse()

	mode := strings.ToLower(strings.TrimSpace(*stallOn))
	if mode == "" {
		mode = "both"
	}
	stallDuration := time.Duration(*stallMS) * time.Millisecond
	if stallDuration <= 0 {
		stallDuration = 15 * time.Second
	}

	dec := json.NewDecoder(os.Stdin)
	enc := json.NewEncoder(os.Stdout)

	// Stage 1 handshake: first message must be init and empty object response.
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
			if shouldStall(msg.Event, mode) {
				time.Sleep(stallDuration)
				return
			}
			_ = enc.Encode(outboundMessage{
				Event: "complete",
				OID:   msg.OID,
			})
		}
	}
}
