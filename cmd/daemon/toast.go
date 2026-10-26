package main

// Notifier displays a desktop notification. The implementation is
// platform-specific: a PowerShell balloon toast on Windows
// (toast_windows.go), notify-send on Linux (toast_linux.go).
type Notifier func(title, message string)
