package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// trayLog is the package-level logger for the tray app. It writes to both
// stderr and ~/.proton-git-lfs/tray.log.
var trayLog *log.Logger

func initTrayLog() {
	writers := []io.Writer{os.Stderr}

	home, err := os.UserHomeDir()
	if err == nil {
		dir := filepath.Join(home, ".proton-git-lfs")
		_ = os.MkdirAll(dir, 0o700)
		f, err := os.OpenFile(filepath.Join(dir, "tray.log"),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			writers = append(writers, f)
		}
	}

	trayLog = log.New(io.MultiWriter(writers...), "[tray] ", log.LstdFlags)
}
