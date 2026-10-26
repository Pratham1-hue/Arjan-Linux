//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// notifyToast shows a native Windows notification. It shells
// out to PowerShell and uses NotifyIcon.BalloonTip, which
// Windows 10/11 surfaces as a real toast notification.
// This runs only a few times a day, so the short-lived
// powershell.exe process is irrelevant to the daemon's
// steady-state footprint.
func notifyToast(title, message string) error {
	script := `Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$n = New-Object System.Windows.Forms.NotifyIcon
$n.Icon = [System.Drawing.SystemIcons]::Information
$n.BalloonTipTitle = '__TITLE__'
$n.BalloonTipText = '__MESSAGE__'
$n.Visible = $true
$n.ShowBalloonTip(5000)
Start-Sleep -Milliseconds 5100
$n.Dispose()`

	script = strings.ReplaceAll(script, "__TITLE__", escPS(title))
	script = strings.ReplaceAll(script, "__MESSAGE__", escPS(message))

	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
		"-WindowStyle", "Hidden", "-Command", script)
	// tracker.exe is a GUI program now: without CREATE_NO_WINDOW,
	// PowerShell would flash a console window on screen.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}

// escPS escapes a string for a single-quoted PowerShell literal.
func escPS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
