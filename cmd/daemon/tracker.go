package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	idleThreshold  = 3 * time.Minute  // no input for this long => pause logging
	nudgeThreshold = 15 * time.Minute // continuous distraction before a nudge
	activePoll     = 15 * time.Second // idle check + heartbeat while active
	idlePoll       = 3 * time.Second  // faster check only while idle, to catch the return
	minSessionSecs = 2                // alt-tab blips shorter than this are discarded
	suspendGapSecs = 60               // a tick gap this large means sleep/hibernate
	maxTitleRunes  = 512
	maxURLRunes    = 2048
)

// Browsers whose active tab is reported by the extension. Extend if
// needed. Linux uses the executable's own name (from /proc/<pid>/exe),
// Windows uses the image name.
var browserExes = map[string]bool{
	// Windows
	"chrome.exe": true,
	"msedge.exe": true,
	"brave.exe":  true,
	// Linux
	"chrome":         true,
	"google-chrome":  true,
	"chromium":       true,
	"brave":          true,
	"microsoft-edge": true,
	"vivaldi":        true,
}

type windowInfo struct {
	hwnd  uintptr
	exe   string // lower-case executable name, e.g. "chrome"; "" = no foreground window
	title string
}

type tabInfo struct {
	url   string
	title string
}

// session is one continuous stretch of activity.
type session struct {
	id       int64 // 0 until first written to the DB
	start    int64 // unix seconds
	end      int64
	app      string
	title    string
	url      string
	category string
}

// matches reports whether want describes the same activity as s.
// Browsers are keyed by URL (title changes don't split a session);
// everything else is keyed by window title.
func (s *session) matches(want *session) bool {
	return s.app == want.app && s.url == want.url && (want.url != "" || s.title == want.title)
}

type Tracker struct {
	mu       sync.Mutex
	store    *Store
	engine   *RuleEngine
	labels   *LabelCache // AI labels; lets the nudge see AI-detected distractions
	notifier Notifier
	log      *log.Logger
	cur      *session
	fg       windowInfo
	// Active tab per browser exe, as reported by the extension
	// (key "" = an old bridge that didn't say which browser).
	tabs     map[string]tabInfo
	idle     bool
	paused   bool
	lastTick int64
	onPause  func(paused bool)

	// Distraction streak (Phase 3): when the current streak
	// of distraction time began, whether the nudge already
	// fired, and a message waiting to be shown outside the
	// lock.
	distractSince int64
	nudged        bool
	nudgeMsg      string
}

func NewTracker(store *Store, engine *RuleEngine, notifier Notifier, lg *log.Logger) *Tracker {
	return &Tracker{store: store, engine: engine, notifier: notifier, log: lg, tabs: map[string]tabInfo{}}
}

func (t *Tracker) logf(format string, args ...any) {
	if t.log != nil {
		t.log.Printf(format, args...)
	}
}

// SetPaused stops (or resumes) recording. Paused time is not
// logged at all, exactly like idle time.
func (t *Tracker) SetPaused(p bool) {
	info := currentForeground()
	now := time.Now().Unix()

	t.mu.Lock()
	changed := t.paused != p
	t.paused = p
	if p {
		t.closeLocked(now)
		t.distractSince, t.nudged = 0, false
	} else {
		t.fg = info
		t.reconcileLocked(now)
	}
	listener := t.onPause
	t.mu.Unlock()

	if changed {
		t.logf("tracking paused=%v", p)
		if listener != nil {
			listener(p)
		}
	}
}

func (t *Tracker) Paused() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.paused
}

// OnPauseChange registers a callback (the tray) for pause changes.
func (t *Tracker) OnPauseChange(f func(paused bool)) {
	t.mu.Lock()
	t.onPause = f
	t.mu.Unlock()
}

// ---------------------------------------------------------------- inputs ---

// OnForeground is called from the WinEvent hook (event-driven, no polling).
func (t *Tracker) OnForeground(hwnd uintptr) {
	info := describeWindow(hwnd)
	now := time.Now().Unix()

	t.mu.Lock()
	t.fg = info
	t.reconcileLocked(now)
	t.mu.Unlock()
	t.fireNudge()
}

// OnBrowserTab is called when the extension reports the active tab.
// browser is the reporting browser's exe ("" if unknown).
func (t *Tracker) OnBrowserTab(url, title string, incognito bool, browser string) {
	if incognito { // never record what happens in private windows
		url, title = "", "Incognito"
	}
	now := time.Now().Unix()

	t.mu.Lock()
	t.tabs[browser] = tabInfo{url: clip(url, maxURLRunes), title: clip(title, maxTitleRunes)}
	if browserExes[t.fg.exe] {
		t.reconcileLocked(now)
	}
	t.mu.Unlock()
	t.fireNudge()
}

// Run drives the idle-detection loop until ctx is cancelled.
func (t *Tracker) Run(ctx context.Context) {
	timer := time.NewTimer(activePoll)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			timer.Reset(t.tick())
		}
	}
}

// Shutdown closes the open session and the database.
func (t *Tracker) Shutdown() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closeLocked(time.Now().Unix())
	if err := t.store.Close(); err != nil {
		t.logf("closing db: %v", err)
	}
}

// ------------------------------------------------------------ idle loop ---

// tick does idle detection, samples the foreground title (cheap substitute
// for a NAMECHANGE hook, which would wake the CPU constantly and can push
// Chrome into accessibility mode), and writes a heartbeat. It returns the
// delay until the next tick.
func (t *Tracker) tick() time.Duration {
	now := time.Now().Unix() // wall clock on purpose: monotonic time can skip sleep
	idleFor := idleDuration()
	info := currentForeground()

	// Pick up edits to rules.json without restarting the daemon.
	_ = t.engine.Reload()

	t.mu.Lock()

	// Laptop slept / hibernated: close at the last moment we know it was alive.
	if t.lastTick != 0 && now-t.lastTick > suspendGapSecs {
		t.closeLocked(t.lastTick)
	}
	t.lastTick = now

	if idleFor >= idleThreshold {
		if !t.idle {
			t.idle = true
			t.closeLocked(now - int64(idleFor/time.Second)) // end at the last real input
			t.distractSince = 0                             // idle breaks a distraction streak
			t.nudged = false
			t.logf("idle for %s: logging paused", idleFor.Round(time.Second))
		}
		t.mu.Unlock()
		return idlePoll
	}

	at := now
	if t.idle { // input is back: the new session starts at that input
		t.idle = false
		at = now - int64(idleFor/time.Second)
		t.logf("activity resumed")
	}

	t.fg = info
	t.reconcileLocked(at)
	t.heartbeatLocked(now)
	t.mu.Unlock()
	t.fireNudge()
	return activePoll
}

// ----------------------------------------------------- session machinery ---
// All *Locked methods require t.mu to be held.

func (t *Tracker) desiredLocked() *session {
	if browserExes[t.fg.exe] {
		// Only trust tab info from the browser that is actually in
		// front; a browser without the extension falls back to its
		// window title instead of inheriting another browser's tab.
		tab, ok := t.tabs[t.fg.exe]
		if !ok {
			tab, ok = t.tabs[""]
		}
		if ok {
			return &session{app: t.fg.exe, title: tab.title, url: tab.url}
		}
	}
	return &session{app: t.fg.exe, title: clip(t.fg.title, maxTitleRunes)}
}

// reconcileLocked makes the open session match what is on screen right now.
func (t *Tracker) reconcileLocked(at int64) {
	if t.idle || t.paused || t.fg.exe == "" {
		t.closeLocked(at)
		return
	}
	want := t.desiredLocked()
	want.category = t.engine.Categorize(want.app, want.title, want.url)
	if t.cur != nil {
		if t.cur.matches(want) {
			t.cur.title = want.title // same activity, newer title
			t.trackDistractionLocked(want, at)
			return
		}
		t.closeLocked(at)
	}
	want.start, want.end = at, at
	t.trackDistractionLocked(want, at)
	t.cur = want
}

// trackDistractionLocked maintains the continuous-distraction
// streak and arms the 15-minute nudge.
func (t *Tracker) trackDistractionLocked(s *session, at int64) {
	if t.kindOfLocked(s) != kindDistraction {
		t.distractSince = 0
		t.nudged = false
		return
	}
	if t.distractSince == 0 {
		t.distractSince = at
		return
	}
	if !t.nudged && at-t.distractSince >= int64(nudgeThreshold/time.Second) {
		t.nudged = true
		t.nudgeMsg = fmt.Sprintf(
			"You've spent 15 minutes in \"%s\". Time for a break?", s.category)
	}
}

// kindOfLocked: rules.json decides when it matches; otherwise the
// AI label (if the activity has been classified already).
func (t *Tracker) kindOfLocked(s *session) string {
	if s.category != uncategorized {
		return t.engine.KindOf(s.category)
	}
	if l, ok := t.labels.Get(activityKey(s.app, s.url, s.title)); ok {
		return l.Kind
	}
	return kindNeutral
}

// fireNudge shows a pending nudge. Called with t.mu NOT held:
// a toast spawns a process, which must not happen under the lock.
func (t *Tracker) fireNudge() {
	t.mu.Lock()
	msg := t.nudgeMsg
	t.nudgeMsg = ""
	t.mu.Unlock()
	if msg != "" && t.notifier != nil {
		t.notifier("Arjan nudge", msg)
	}
}

func (t *Tracker) closeLocked(end int64) {
	s := t.cur
	if s == nil {
		return
	}
	t.cur = nil
	if end < s.start {
		end = s.start
	}
	s.end = end
	if s.end-s.start < minSessionSecs {
		if s.id != 0 {
			if err := t.store.Delete(s.id); err != nil {
				t.logf("db delete failed: %v", err)
			}
		}
		return
	}
	t.persist(s)
}

// heartbeatLocked persists the running session so a crash or power loss
// costs at most one poll interval of data.
func (t *Tracker) heartbeatLocked(now int64) {
	s := t.cur
	if s == nil || now-s.start < minSessionSecs {
		return
	}
	s.end = now
	t.persist(s)
}

func (t *Tracker) persist(s *session) {
	var err error
	if s.id == 0 {
		s.id, err = t.store.Insert(s)
	} else {
		err = t.store.Update(s)
	}
	if err != nil {
		t.logf("db write failed: %v", err)
	}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
