//go:build linux

package main

import (
	"bufio"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The Linux tray takes a PNG (StatusNotifierItem IconPixmap), not the
// .ico the Windows Shell uses.
func TestTrayIconIsValidPNG(t *testing.T) {
	img, err := png.Decode(strings.NewReader(string(trayIcon(colorActive))))
	if err != nil {
		t.Fatalf("trayIcon is not a PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() < 16 || b.Dx() != b.Dy() {
		t.Fatalf("tray icon is %dx%d, want a square >= 16px", b.Dx(), b.Dy())
	}
}

// "Start at login" is a freedesktop autostart entry.
func TestAutostartDesktopEntry(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	if autostartEnabled() {
		t.Fatal("autostart should be off in a fresh config home")
	}
	if err := setAutostart(true); err != nil {
		t.Fatalf("setAutostart: %v", err)
	}
	path := filepath.Join(cfgHome, "autostart", "arjan.desktop")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("desktop entry not written: %v", err)
	}
	for _, want := range []string{"[Desktop Entry]", "Type=Application", "Exec=", "Name=Arjan"} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("desktop entry missing %q:\n%s", want, b)
		}
	}
	if !autostartEnabled() {
		t.Fatal("autostartEnabled() should be true after setAutostart(true)")
	}
	if err := setAutostart(false); err != nil {
		t.Fatalf("setAutostart(false): %v", err)
	}
	if autostartEnabled() {
		t.Fatal("autostartEnabled() should be false after setAutostart(false)")
	}
}

// Data lives under $XDG_DATA_HOME, so a test never touches the real folder.
func TestPlatformDataDir(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "share"))
	if got, want := platformDataDir(), filepath.Join(os.Getenv("XDG_DATA_HOME"), "arjan"); got != want {
		t.Fatalf("platformDataDir() = %q, want %q", got, want)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	t.Setenv("XDG_DATA_HOME", "")
	if got, want := platformDataDir(), filepath.Join(home, ".local", "share", "arjan"); got != want {
		t.Fatalf("platformDataDir() with no XDG_DATA_HOME = %q, want %q", got, want)
	}
}

// A second Arjan must not start: the flock is what the tray relies on
// when you double-click the launcher twice.
func TestSingleInstanceLock(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if !acquireSingleInstance() {
		t.Fatal("first acquireSingleInstance() failed")
	}
	defer func() {
		if singleInstanceFile != nil {
			singleInstanceFile.Close()
			singleInstanceFile = nil
		}
	}()
	if acquireSingleInstance() {
		t.Fatal("second acquireSingleInstance() should fail while the first holds the lock")
	}
}

// The browser bridge talks over a private Unix socket.
func TestBrowserSocket(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)

	l, err := browserListener(log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("browserListener: %v", err)
	}
	defer l.Close()

	if info, err := os.Stat(browserAddress()); err != nil {
		t.Fatalf("socket missing: %v", err)
	} else if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("socket mode = %v, want owner-only", info.Mode().Perm())
	}

	done := make(chan string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			close(done)
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadString('\n')
		done <- line
	}()

	c, err := browserDial()
	if err != nil {
		t.Fatalf("browserDial: %v", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("{\"type\":\"tab\",\"url\":\"https://leetcode.com/\"}\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case line := <-done:
		if !strings.Contains(line, "leetcode.com") {
			t.Fatalf("daemon read %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the message")
	}
}
