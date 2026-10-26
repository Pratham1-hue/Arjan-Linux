//go:build windows

package main

import (
	"errors"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// autostartLabel is the tray menu entry that toggles launching at login.
const autostartLabel = "Start with Windows"

// trayIcon returns the icon bytes the Windows tray needs: a real .ico.
func trayIcon(c color.NRGBA) []byte { return faviconICO(c) }

// openDashboardURL focuses an open dashboard window, or opens one
// as a chromeless Edge "app" window (Edge ships with Windows), or
// falls back to the default browser.
func openDashboardURL(url string) {
	if focusWindowByTitle(dashboardTitle) {
		return
	}
	if edge := findEdge(); edge != "" {
		if exec.Command(edge, "--app="+url, "--window-size=1320,900").Start() == nil {
			return
		}
	}
	exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
}

func findEdge() string {
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		if base == "" {
			continue
		}
		p := filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// openDataFolder shows the database/log/settings folder in Explorer.
func openDataFolder(dir string) error {
	return exec.Command("explorer.exe", dir).Start()
}

// -------------------------------------------------------------- autostart ---

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue   = "Arjan"
)

// autostartEnabled reports whether the HKCU Run entry exists (it
// also shows up in Task Manager → Startup apps).
func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValue)
	return err == nil && v != ""
}

func setAutostart(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValue, `"`+exe+`"`)
}
