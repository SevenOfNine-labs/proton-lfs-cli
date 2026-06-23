package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// trayLog is the package-level logger for the tray app. It writes to both
// stderr and ~/.proton-lfs/tray.log.
var trayLog *log.Logger

func trayLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "tray.log")
	}
	return filepath.Join(home, ".proton-lfs", "tray.log")
}

func initTrayLog() {
	writers := []io.Writer{os.Stderr}

	_, err := os.UserHomeDir()
	if err == nil {
		dir := filepath.Dir(trayLogPath())
		_ = os.MkdirAll(dir, 0o700)
		f, err := os.OpenFile(trayLogPath(),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			writers = append(writers, f)
		}
	}

	trayLog = log.New(io.MultiWriter(writers...), "[tray] ", log.LstdFlags)
}
