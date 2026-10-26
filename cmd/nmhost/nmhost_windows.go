//go:build windows

package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// Must match pipeName in cmd/daemon/pipe_windows.go.
const pipeName = `\\.\pipe\StudyTrackerBrowser`

func daemonDial() (net.Conn, error) {
	timeout := 2 * time.Second
	return winio.DialPipe(pipeName, &timeout)
}

// logDir is where nmhost.log goes (the daemon's data folder).
func logDir() string {
	if v := os.Getenv("STUDYTRACKER_DATA"); v != "" {
		return v
	}
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return filepath.Join(dir, "StudyTracker")
	}
	return ""
}

// detectBrowser walks up the process tree to find which browser
// started us (Chrome launches native hosts through cmd.exe), so the
// daemon can tell Chrome tabs from Edge tabs. "" if unknown.
func detectBrowser() string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(snap)

	type proc struct {
		parent uint32
		exe    string
	}
	procs := map[uint32]proc{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		procs[e.ProcessID] = proc{e.ParentProcessID, strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))}
	}

	pid := uint32(os.Getpid())
	for i := 0; i < 5; i++ {
		self, ok := procs[pid]
		if !ok {
			return ""
		}
		parent, ok := procs[self.parent]
		if !ok {
			return ""
		}
		if parent.exe == "cmd.exe" || parent.exe == "conhost.exe" {
			pid = self.parent
			continue
		}
		return parent.exe
	}
	return ""
}
