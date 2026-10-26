//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// notifyToast shows a desktop notification through the freedesktop
// notification service (notify-send, from libnotify-bin) and falls
// back to KDE's kdialog. Called a few times a day at most.
func notifyToast(title, message string) error {
	if p, err := exec.LookPath("notify-send"); err == nil {
		args := []string{
			"--app-name=Arjan",
			"--icon=dialog-information",
			// keep long daily reports readable instead of ellipsizing
			"--hint=string:x-canonical-private-synchronous:arjan",
			title,
			message,
		}
		return runQuiet(p, args...)
	}
	if p, err := exec.LookPath("kdialog"); err == nil {
		return runQuiet(p, "--title", title, "--passivepopup", message, "6")
	}
	return errors.New("no notification tool found — install libnotify-bin (notify-send)")
}

// runQuiet runs a command and folds its stderr into the error, like the
// Windows toast helper does.
func runQuiet(name string, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}
