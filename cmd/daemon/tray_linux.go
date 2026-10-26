//go:build linux

package main

import (
	"errors"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

// autostartLabel is the tray menu entry that toggles launching at login.
const autostartLabel = "Start at login"

// trayIcon returns the icon bytes a StatusNotifierItem needs. The
// Linux tray reads PNG/JPEG, not .ico, so this is a single PNG.
func trayIcon(c color.NRGBA) []byte { return renderIcon(32, c) }

// dashboardBrowsers are tried, in order, for the chromeless "--app"
// dashboard window (the same trick the Windows build does with Edge).
var dashboardBrowsers = []string{
	"google-chrome-stable", "google-chrome", "chromium", "chromium-browser",
	"brave-browser", "microsoft-edge", "microsoft-edge-stable",
}

// openDashboardURL focuses an open dashboard window, or opens one as
// a chromeless Chromium "app" window, or falls back to the default
// browser.
func openDashboardURL(url string) {
	if focusWindowByTitle(dashboardTitle) {
		return
	}
	for _, name := range dashboardBrowsers {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if exec.Command(p, "--app="+url, "--window-size=1320,900").Start() == nil {
			return
		}
	}
	_ = exec.Command("xdg-open", url).Start()
}

// openDataFolder shows the database/log/settings folder in the file manager.
func openDataFolder(dir string) error {
	return exec.Command("xdg-open", dir).Start()
}

// -------------------------------------------------------------- autostart ---
//
// A freedesktop autostart entry (~/.config/autostart/*.desktop) is the
// portable way to launch a tray app at login on Debian, Ubuntu, XFCE,
// KDE, Cinnamon... GNOME and KDE both honour it.

func autostartPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", "arjan.desktop"), nil
}

// autostartEnabled reports whether our .desktop entry exists.
func autostartEnabled() bool {
	p, err := autostartPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func setAutostart(on bool) error {
	p, err := autostartPath()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	entry := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=Arjan\n" +
		"Comment=Study tracker\n" +
		"Exec=" + strconv.Quote(exe) + "\n" +
		"Terminal=false\n" +
		"NoDisplay=true\n" +
		"X-GNOME-Autostart-enabled=true\n"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(entry), 0o644)
}
