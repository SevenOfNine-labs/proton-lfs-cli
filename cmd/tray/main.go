// Proton Git LFS system tray application.
package main

import (
	"fyne.io/systray"
)

// Version is stamped at build time via -ldflags.
var Version = "dev"

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	setupMenu()
	startStatusWatcher()
}

func onExit() {
	stopStatusWatcher()
}
