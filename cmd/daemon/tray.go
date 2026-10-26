package main

import (
	"fmt"
	"log"
	"time"

	"fyne.io/systray"
)

// dashboardTitle must match the page <title>; it's how an already
// open dashboard window is found and brought to the front.
const dashboardTitle = "Arjan Dashboard"

// Tray is the system-tray UI: the app's only visible surface
// besides the dashboard window. The platform-specific pieces
// (autostart, opening the dashboard and the data folder, the icon
// format) live in tray_windows.go and tray_linux.go.
type Tray struct {
	tracker  *Tracker
	analyzer *Analyzer
	server   *Server
	notifier Notifier
	dataDir  string
	lg       *log.Logger
	quit     func()
}

func (t *Tray) onReady() {
	systray.SetIcon(trayIcon(colorActive))
	systray.SetTooltip("Arjan — tracking")

	mStatus := systray.AddMenuItem("Arjan", "")
	mStatus.Disable()
	systray.AddSeparator()
	mOpen := systray.AddMenuItem("Open Dashboard", "Open the Arjan dashboard")
	systray.AddSeparator()
	mPause := systray.AddMenuItemCheckbox("Pause tracking", "Stop recording until resumed", t.tracker.Paused())
	mAuto := systray.AddMenuItemCheckbox(autostartLabel, "Launch Arjan when you sign in", autostartEnabled())
	systray.AddSeparator()
	mReport := systray.AddMenuItem("Show today's report", "Show today's summary as a notification")
	mData := systray.AddMenuItem("Open data folder", "Database, logs and settings")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit Arjan", "Stop tracking and exit")

	// Left click opens the dashboard; right click shows the menu.
	systray.SetOnTapped(t.openDashboard)

	applyPaused := func(p bool) {
		if p {
			mPause.Check()
			systray.SetIcon(trayIcon(colorPaused))
		} else {
			mPause.Uncheck()
			systray.SetIcon(trayIcon(colorActive))
		}
		t.refreshStatus(mStatus)
	}
	t.tracker.OnPauseChange(applyPaused)

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		t.refreshStatus(mStatus)
		for {
			select {
			case <-ticker.C:
				t.refreshStatus(mStatus)
			case <-mOpen.ClickedCh:
				t.openDashboard()
			case <-mPause.ClickedCh:
				t.tracker.SetPaused(!t.tracker.Paused())
			case <-mAuto.ClickedCh:
				on := !autostartEnabled()
				if err := setAutostart(on); err != nil {
					t.lg.Printf("autostart: %v", err)
				}
				if autostartEnabled() {
					mAuto.Check()
				} else {
					mAuto.Uncheck()
				}
			case <-mReport.ClickedCh:
				go func() {
					msg, err := dailyReport(t.analyzer)
					if err != nil {
						msg = err.Error()
					}
					t.notifier("Arjan — today so far", msg)
				}()
			case <-mData.ClickedCh:
				if err := openDataFolder(t.dataDir); err != nil {
					t.lg.Printf("open data folder: %v", err)
				}
			case <-mQuit.ClickedCh:
				t.quit()
				return
			}
		}
	}()
}

// refreshStatus updates the first menu line and the tooltip with
// today's totals.
func (t *Tray) refreshStatus(item *systray.MenuItem) {
	state := "tracking"
	if t.tracker.Paused() {
		state = "paused"
	}
	rep, err := t.analyzer.Day(time.Now())
	if err != nil || rep.Total == 0 {
		item.SetTitle("Arjan — " + state)
		systray.SetTooltip("Arjan — " + state)
		return
	}
	line := fmt.Sprintf("Today %s", fmtDur(rep.Total))
	if rep.FocusScore != nil {
		line += fmt.Sprintf(" · Focus %d", *rep.FocusScore)
	}
	item.SetTitle(line)
	systray.SetTooltip(fmt.Sprintf("Arjan — %s\n%s", state, line))
}

func (t *Tray) openDashboard() { openDashboardURL(t.server.URL()) }
