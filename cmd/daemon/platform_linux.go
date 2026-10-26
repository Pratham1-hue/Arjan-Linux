//go:build linux

// Linux platform layer: the focused window and idle time come from the
// X11 protocol (pure Go via github.com/jezek/xgb), with the GNOME
// idle monitor over D-Bus as a fallback for Wayland sessions.
//
// Wayland deliberately gives no portable way to read another app's
// window, so on a Wayland session only XWayland clients (and browser
// tabs, which arrive through the extension) can be identified. Log in
// with an Xorg session for complete tracking — see the README.

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/screensaver"
	"github.com/jezek/xgb/xproto"
)

// pollInterval is how often the focused window is sampled. X11 has no
// portable "focus changed" event without subscribing to every window's
// property changes, and one property read per second costs nothing.
const pollInterval = time.Second

// attachParentConsole is a no-op here: Linux apps keep stdout/stderr.
func attachParentConsole() {}

// messageBox shows an error dialog (there may be no terminal at all
// when Arjan is started from the tray or at login).
func messageBox(title, text string) {
	for _, cmd := range [][2]string{
		{"zenity", "--warning"},
		{"kdialog", "--sorry"},
	} {
		p, err := exec.LookPath(cmd[0])
		if err != nil {
			continue
		}
		var args []string
		if cmd[0] == "zenity" {
			args = []string{"--title", title, "--text", text, "--no-wrap"}
		} else {
			args = []string{"--title", title, text}
		}
		if exec.Command(p, args...).Run() == nil {
			return
		}
	}
}

// ------------------------------------------------------------------ X11 ---

type x11State struct {
	mu   sync.Mutex
	conn *xgb.Conn
	root xproto.Window
	// saver is true when the MIT-SCREEN-SAVER extension answered, so
	// the idle counter can be read straight from the X server.
	saver bool

	netActiveWindow xproto.Atom
	netClientList   xproto.Atom
	netWmName       xproto.Atom
	netWmPid        xproto.Atom
	utf8String      xproto.Atom
}

var (
	x11Once sync.Once
	x11     *x11State
)

// x11Get connects to the X server on first use; nil means there is no
// usable DISPLAY (a pure Wayland session, or a headless run).
func x11Get() *x11State {
	x11Once.Do(func() {
		if os.Getenv("DISPLAY") == "" {
			return
		}
		// xgb logs connection noise to stderr; keep the daemon log clean.
		xgb.Logger = log.New(io.Discard, "", 0)
		conn, err := xgb.NewConn()
		if err != nil {
			log.Printf("X11 unavailable: %v", err)
			return
		}
		s := &x11State{
			conn: conn,
			root: xproto.Setup(conn).DefaultScreen(conn).Root,
		}
		s.netActiveWindow = internAtom(conn, "_NET_ACTIVE_WINDOW")
		s.netClientList = internAtom(conn, "_NET_CLIENT_LIST")
		s.netWmName = internAtom(conn, "_NET_WM_NAME")
		s.netWmPid = internAtom(conn, "_NET_WM_PID")
		s.utf8String = internAtom(conn, "UTF8_STRING")
		s.saver = screensaver.Init(conn) == nil
		x11 = s
	})
	return x11
}

func internAtom(c *xgb.Conn, name string) xproto.Atom {
	reply, err := xproto.InternAtom(c, false, uint16(len(name)), name).Reply()
	if err != nil || reply == nil {
		return 0
	}
	return reply.Atom
}

// propLocked reads a whole property. Callers hold s.mu.
func (s *x11State) propLocked(w xproto.Window, property, typ xproto.Atom) []byte {
	if w == 0 || property == 0 {
		return nil
	}
	reply, err := xproto.GetProperty(s.conn, false, w, property, typ, 0, 1<<16).Reply()
	if err != nil || reply == nil {
		return nil
	}
	return reply.Value
}

// titleLocked returns _NET_WM_NAME (UTF-8), falling back to WM_NAME.
func (s *x11State) titleLocked(w xproto.Window) string {
	if v := s.propLocked(w, s.netWmName, s.utf8String); len(v) > 0 {
		return strings.TrimRight(string(v), "\x00")
	}
	if v := s.propLocked(w, xproto.AtomWmName, xproto.AtomAny); len(v) > 0 {
		return strings.TrimRight(string(v), "\x00")
	}
	return ""
}

// pidLocked returns the process that owns the window (_NET_WM_PID).
func (s *x11State) pidLocked(w xproto.Window) int {
	v := s.propLocked(w, s.netWmPid, xproto.AtomCardinal)
	if len(v) < 4 {
		return 0
	}
	return int(binary.LittleEndian.Uint32(v[:4]))
}

// windowLocked reads a single WINDOW-typed property (32-bit ids).
func (s *x11State) windowLocked(w xproto.Window, property xproto.Atom) xproto.Window {
	v := s.propLocked(w, property, xproto.AtomWindow)
	if len(v) < 4 {
		return 0
	}
	return xproto.Window(binary.LittleEndian.Uint32(v[:4]))
}

func (s *x11State) activeWindow() xproto.Window {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.windowLocked(s.root, s.netActiveWindow)
}

func (s *x11State) describe(w xproto.Window) (title string, pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.titleLocked(w), s.pidLocked(w)
}

// clientWindows lists the managed toplevel windows, in stacking order.
func (s *x11State) clientWindows() []xproto.Window {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.propLocked(s.root, s.netClientList, xproto.AtomWindow)
	out := make([]xproto.Window, 0, len(v)/4)
	for i := 0; i+4 <= len(v); i += 4 {
		out = append(out, xproto.Window(binary.LittleEndian.Uint32(v[i:i+4])))
	}
	return out
}

// activate asks the window manager to raise and focus w.
func (s *x11State) activate(w xproto.Window) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := xproto.ClientMessageEvent{
		Format: 32,
		Window: w,
		Type:   s.netActiveWindow,
		Data: xproto.ClientMessageDataUnion{
			// source indication 2 = "user action" (pager/taskbar)
			Data32: []uint32{2, xproto.TimeCurrentTime, 0, 0, 0},
		},
	}
	return xproto.SendEvent(s.conn, false, s.root,
		xproto.EventMaskSubstructureRedirect|xproto.EventMaskSubstructureNotify,
		string(ev.Bytes())).Check()
}

// x11Idle reports the idle time from the X server's screen-saver
// counter (works on Xorg and on XWayland for X input).
func x11Idle() (time.Duration, bool) {
	s := x11Get()
	if s == nil || !s.saver {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	info, err := screensaver.QueryInfo(s.conn, xproto.Drawable(s.root)).Reply()
	if err != nil || info == nil {
		return 0, false
	}
	return time.Duration(info.MsSinceUserInput) * time.Millisecond, true
}

// ------------------------------------------------------- GNOME idle (D-Bus) ---

var (
	mutterOnce sync.Once
	mutterBus  *dbus.Conn
)

// mutterIdle asks GNOME Shell how long the user has been idle. This is
// the only reliable idle source on a Wayland session.
func mutterIdle() (time.Duration, bool) {
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		return 0, false
	}
	mutterOnce.Do(func() {
		if c, err := dbus.SessionBus(); err == nil {
			mutterBus = c
		}
	})
	if mutterBus == nil {
		return 0, false
	}
	var ms uint64
	call := mutterBus.Object("org.gnome.Mutter.IdleMonitor",
		"/org/gnome/Mutter/IdleMonitor/Core").
		Call("org.gnome.Mutter.IdleMonitor.GetIdletime", 0)
	done := make(chan error, 1)
	go func() { done <- call.Store(&ms) }()
	select {
	case err := <-done:
		if err != nil {
			return 0, false
		}
		return time.Duration(ms) * time.Millisecond, true
	case <-time.After(2 * time.Second):
		return 0, false
	}
}

// ----------------------------------------------------------------- inputs ---

// idleDuration returns the time since the last keyboard/mouse input.
// Mutter (GNOME) is preferred on Wayland; otherwise the X server knows.
func idleDuration() time.Duration {
	wayland := os.Getenv("XDG_SESSION_TYPE") == "wayland" || os.Getenv("WAYLAND_DISPLAY") != ""
	if wayland {
		if d, ok := mutterIdle(); ok {
			return d
		}
	}
	if d, ok := x11Idle(); ok {
		return d
	}
	if d, ok := mutterIdle(); ok {
		return d
	}
	return 0
}

func currentForeground() windowInfo {
	s := x11Get()
	if s == nil {
		return windowInfo{}
	}
	return describeWindow(uintptr(s.activeWindow()))
}

// processExe resolves a pid to its executable's base name ("code",
// "firefox"), the Linux equivalent of a Windows image name.
func processExe(pid int) string {
	if pid <= 0 {
		return ""
	}
	link := fmt.Sprintf("/proc/%d/exe", pid)
	if target, err := os.Readlink(link); err == nil {
		target = strings.TrimSuffix(target, " (deleted)")
		if base := filepath.Base(target); base != "" {
			return strings.ToLower(base)
		}
	}
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
		if name := strings.TrimSpace(string(b)); name != "" {
			return strings.ToLower(name)
		}
	}
	return ""
}

// describeWindow resolves an X window id to its process and title.
func describeWindow(hwnd uintptr) windowInfo {
	info := windowInfo{hwnd: hwnd}
	if hwnd == 0 {
		return info
	}
	s := x11Get()
	if s == nil {
		return info
	}
	title, pid := s.describe(xproto.Window(hwnd))
	info.title = title
	if pid == 0 {
		info.exe = "unknown"
		return info
	}
	info.exe = processExe(pid)
	if info.exe == "" {
		info.exe = "unknown"
	}
	return info
}

// runEventLoop samples the focused window and calls onForeground when
// it changes. It returns when stopEventLoop is called.
func runEventLoop(onForeground func(hwnd uintptr)) error {
	s := x11Get()
	if s == nil {
		// No window information on this session; keep the app alive so
		// idle handling and browser tabs still work.
		<-eventLoopStop
		return nil
	}
	var last xproto.Window
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-eventLoopStop:
			return nil
		case <-ticker.C:
			w := s.activeWindow()
			if w == last {
				continue
			}
			last = w
			onForeground(uintptr(w))
		}
	}
}

var (
	eventLoopStop     = make(chan struct{})
	eventLoopStopOnce sync.Once
)

// stopEventLoop makes runEventLoop return. Safe to call more than once.
func stopEventLoop() { eventLoopStopOnce.Do(func() { close(eventLoopStop) }) }

// focusWindowByTitle raises an existing window with this exact title.
// Returns false when there is none.
func focusWindowByTitle(title string) bool {
	s := x11Get()
	if s == nil {
		return false
	}
	for _, w := range s.clientWindows() {
		t, _ := s.describe(w)
		if t != title {
			continue
		}
		if err := s.activate(w); err == nil {
			return true
		}
		return false
	}
	return false
}

// ------------------------------------------------------- single instance ---
//
// flock() on a per-user file: the lock disappears with the process, so
// a crash never leaves a stale "already running" state behind.

var singleInstanceFile *os.File

func singleInstancePath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, fmt.Sprintf("arjan-%d.lock", os.Getuid()))
}

// acquireSingleInstance returns false if another Arjan is already running.
func acquireSingleInstance() bool {
	f, err := os.OpenFile(singleInstancePath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return true // can't lock: better to run than to refuse to start
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return false
	}
	singleInstanceFile = f // held for the life of the process
	return true
}

// ------------------------------------------------------------- data dirs ---

// platformDataDir is where the database, logs and settings live on
// Linux: $XDG_DATA_HOME/arjan, usually ~/.local/share/arjan.
func platformDataDir() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "arjan")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local", "share", "arjan")
}

// logPlatformNotes warns about anything that limits tracking here.
func logPlatformNotes(lg *log.Logger) {
	if x11Get() == nil {
		lg.Printf("note: no X11 display — window titles cannot be read; only browser tabs will be tracked")
		notifyToast("Arjan", "No X11 display: window tracking is off. Log in with an Xorg session for full tracking.")
		return
	}
	if os.Getenv("XDG_SESSION_TYPE") == "wayland" || os.Getenv("WAYLAND_DISPLAY") != "" {
		lg.Printf("note: Wayland session — only X11 (XWayland) windows can be identified; " +
			"choose \"Ubuntu on Xorg\" at login for complete tracking")
		notifyToast("Arjan", "Wayland session: only X11 apps are tracked. "+
			"Log in with \"Ubuntu on Xorg\" for complete tracking.")
	}
	if _, err := exec.LookPath("notify-send"); err != nil {
		lg.Printf("note: notify-send not found — install libnotify-bin for desktop notifications")
	}
}

// browserAddress is the Unix socket the browser bridge (nmhost)
// forwards tabs to. XDG_RUNTIME_DIR is per-user and private.
func browserAddress() string {
	return filepath.Join(runtimeDir(), "arjan.sock")
}

func runtimeDir() string {
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		return v
	}
	return os.TempDir()
}

// cleanupListener removes the socket file on close, so a restart never
// trips over a leftover inode.
type cleanupListener struct {
	net.Listener
	path string
}

func (l *cleanupListener) Close() error {
	err := l.Listener.Close()
	_ = os.Remove(l.path)
	return err
}
