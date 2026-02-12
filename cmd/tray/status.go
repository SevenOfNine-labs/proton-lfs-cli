package main

import (
	"fmt"
	"sync"
	"time"

	"fyne.io/systray"

	"proton-git-lfs/internal/config"
)

const pollInterval = 5 * time.Second

var (
	stopCh   chan struct{}
	stopOnce sync.Once
)

func startStatusWatcher() {
	stopCh = make(chan struct{})
	go watchLoop()
}

func stopStatusWatcher() {
	stopOnce.Do(func() { close(stopCh) })
}

func watchLoop() {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Initial read
	applyStatus()

	for {
		select {
		case <-ticker.C:
			applyStatus()
		case <-stopCh:
			return
		}
	}
}

func applyStatus() {
	report, err := config.ReadStatus()
	if err != nil {
		mStatus.SetTitle("Status: No adapter activity")
		systray.SetIcon(iconIdle)
		systray.SetTemplateIcon(iconIdle, iconIdle)
		return
	}

	switch report.State {
	case config.StateIdle:
		mStatus.SetTitle("Status: Ready")
		systray.SetIcon(iconIdle)
		systray.SetTemplateIcon(iconIdle, iconIdle)
	case config.StateOK:
		mStatus.SetTitle("Status: OK")
		systray.SetIcon(iconOK)
		systray.SetTemplateIcon(iconOK, iconOK)
	case config.StateError:
		msg := "Status: Error"
		if report.Error != "" {
			msg = fmt.Sprintf("Status: Error — %s", truncate(report.Error, 40))
		}
		mStatus.SetTitle(msg)
		systray.SetIcon(iconError)
		systray.SetTemplateIcon(iconError, iconError)
	case config.StateTransferring:
		mStatus.SetTitle("Status: Transferring…")
		systray.SetIcon(iconSyncing)
		systray.SetTemplateIcon(iconSyncing, iconSyncing)
	}

	if !report.Timestamp.IsZero() {
		// Only show "Last Transfer" for actual data operations, not init/terminate
		switch report.LastOp {
		case "upload", "download":
			mLastTransfer.SetTitle(fmt.Sprintf("Last Transfer: %s (%s)", relativeTime(report.Timestamp), report.LastOp))
		default:
			mLastTransfer.SetTitle(fmt.Sprintf("Last Activity: %s", relativeTime(report.Timestamp)))
		}
	}
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit-1] + "…"
}
