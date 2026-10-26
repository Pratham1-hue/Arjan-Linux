//go:build linux

package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// browserNames are matched against the executable names in the parent
// process chain (substring, so "google-chrome-stable" and "chrome"
// both count as Chrome). The name returned is the executable's own
// base name, exactly what the daemon's /proc/<pid>/exe lookup yields.
var browserNames = []string{
	"chrome", "chromium", "brave", "msedge", "microsoft-edge", "vivaldi", "opera",
}

// daemonDial connects to the daemon's Unix socket (must match
// browserAddress in cmd/daemon/platform_linux.go).
func daemonDial() (net.Conn, error) {
	return (&net.Dialer{Timeout: 2 * time.Second}).Dial("unix", daemonSocket())
}

func daemonSocket() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "arjan.sock")
}

// logDir is the daemon's data folder ($XDG_DATA_HOME/arjan).
func logDir() string {
	if v := os.Getenv("STUDYTRACKER_DATA"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "arjan")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "arjan")
}

// detectBrowser walks up the process tree: the browser launches this
// bridge directly on Linux, so the first browser-like ancestor is the
// one that started us. "" if unknown.
func detectBrowser() string {
	pid := os.Getpid()
	for i := 0; i < 6; i++ {
		ppid, ok := parentPID(pid)
		if !ok || ppid <= 1 {
			return ""
		}
		exe := processExe(ppid)
		if exe != "" {
			base := strings.TrimSuffix(exe, ".exe")
			for _, name := range browserNames {
				if strings.Contains(base, name) {
					return exe
				}
			}
		}
		pid = ppid
	}
	return ""
}

// parentPID reads the parent pid from /proc/<pid>/stat.
func parentPID(pid int) (int, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	// Format: "pid (comm) state ppid ..." — comm may contain spaces and
	// parentheses, so everything after the last ')' is the fixed part.
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 || i+2 >= len(s) {
		return 0, false
	}
	fields := strings.Fields(s[i+2:])
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false
	}
	return ppid, true
}

func processExe(pid int) string {
	base := "/proc/" + strconv.Itoa(pid)
	if target, err := os.Readlink(base + "/exe"); err == nil {
		return strings.ToLower(filepath.Base(strings.TrimSuffix(target, " (deleted)")))
	}
	if b, err := os.ReadFile(base + "/comm"); err == nil {
		return strings.ToLower(strings.TrimSpace(string(b)))
	}
	return ""
}
