//go:build windows

// Windows platform layer: foreground-window hook, idle detection,
// single-instance mutex and the GUI helpers. The Linux counterpart
// lives in platform_linux.go; everything else is shared.

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	eventSystemForeground  = 0x0003
	winEventOutOfContext   = 0x0000 // callback runs on our thread, no DLL injection
	winEventSkipOwnProcess = 0x0002
	wmQuit                 = 0x0012
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSetWinEventHook    = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent     = user32.NewProc("UnhookWinEvent")
	procGetMessage         = user32.NewProc("GetMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessage    = user32.NewProc("DispatchMessageW")
	procPostThreadMessage  = user32.NewProc("PostThreadMessageW")
	procGetWindowText      = user32.NewProc("GetWindowTextW")
	procGetLastInputInfo   = user32.NewProc("GetLastInputInfo")
	procGetTickCount       = kernel32.NewProc("GetTickCount")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
	procAttachConsole      = kernel32.NewProc("AttachConsole")
	procMessageBox         = user32.NewProc("MessageBoxW")
	procFindWindow         = user32.NewProc("FindWindowW")
	procShowWindow         = user32.NewProc("ShowWindow")
	procIsIconic           = user32.NewProc("IsIconic")
	procSetForeground      = user32.NewProc("SetForegroundWindow")
)

// attachParentConsole lets CLI flags (-report, --stop) print to the
// terminal even though tracker.exe is a GUI (tray) program. If
// stdout is already redirected (a pipe or file), it is left alone.
func attachParentConsole() {
	if h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		return
	}
	const attachParentProcess = ^uint32(0) // (DWORD)-1
	if r, _, _ := procAttachConsole.Call(uintptr(attachParentProcess)); r == 0 {
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = f
		os.Stderr = f
		log.SetOutput(f)
	}
}

// messageBox shows a modal error/info dialog (there is no console).
func messageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	const mbIconWarning = 0x30
	procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbIconWarning)
}

// focusWindowByTitle brings an existing top-level window with this
// exact title to the front. Returns false if there is none.
func focusWindowByTitle(title string) bool {
	p, _ := windows.UTF16PtrFromString(title)
	hwnd, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(p)))
	if hwnd == 0 {
		return false
	}
	const swRestore = 9
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		procShowWindow.Call(hwnd, swRestore)
	}
	procSetForeground.Call(hwnd)
	return true
}

type point struct{ x, y int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

var loopThreadID atomic.Uint32

// runEventLoop installs a foreground-change WinEvent hook and blocks in the
// Win32 message loop. GetMessage sleeps in the kernel until Windows delivers
// an event, so the process uses 0% CPU between window switches.
// It returns when stopEventLoop is called.
func runEventLoop(onForeground func(hwnd uintptr)) error {
	// WinEvent hooks are tied to the thread that installs them, and that
	// thread must pump messages: pin this goroutine to its OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	tid, _, _ := procGetCurrentThreadID.Call()
	loopThreadID.Store(uint32(tid))

	cb := syscall.NewCallback(func(hook, event, hwnd, idObject, idChild, thread, eventTime uintptr) uintptr {
		defer func() { _ = recover() }() // a panic in a callback would kill the process
		if uint32(event) == eventSystemForeground {
			onForeground(hwnd)
		}
		return 0
	})

	hook, _, err := procSetWinEventHook.Call(
		eventSystemForeground, eventSystemForeground,
		0, cb, 0, 0,
		winEventOutOfContext|winEventSkipOwnProcess,
	)
	if hook == 0 {
		return fmt.Errorf("SetWinEventHook failed: %w", err)
	}
	defer procUnhookWinEvent.Call(hook)

	var m msg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 = WM_QUIT, -1 = error
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// stopEventLoop makes runEventLoop return. Safe to call from any goroutine.
func stopEventLoop() {
	if tid := loopThreadID.Load(); tid != 0 {
		procPostThreadMessage.Call(uintptr(tid), wmQuit, 0, 0)
	}
}

// idleDuration returns the time since the last keyboard/mouse input anywhere
// in this login session. GetLastInputInfo is a single cheap call; the uint32
// subtraction stays correct across the 49-day tick counter wraparound.
func idleDuration() time.Duration {
	var li lastInputInfo
	li.cbSize = uint32(unsafe.Sizeof(li))
	r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li)))
	if r == 0 {
		return 0
	}
	tick, _, _ := procGetTickCount.Call()
	return time.Duration(uint32(tick)-li.dwTime) * time.Millisecond
}

func currentForeground() windowInfo {
	return describeWindow(uintptr(windows.GetForegroundWindow()))
}

func currentForegroundHWND() uintptr {
	return uintptr(windows.GetForegroundWindow())
}

// describeWindow resolves a window handle to its process image name and title.
func describeWindow(hwnd uintptr) windowInfo {
	info := windowInfo{hwnd: hwnd}
	if hwnd == 0 {
		return info
	}
	h := windows.HWND(hwnd)

	var buf [512]uint16
	if n, _, _ := procGetWindowText.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); n > 0 {
		info.title = windows.UTF16ToString(buf[:n])
	}

	info.exe = "unknown"
	var pid uint32
	if _, err := windows.GetWindowThreadProcessId(h, &pid); err != nil || pid == 0 {
		return info
	}
	proc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return info
	}
	defer windows.CloseHandle(proc)

	var path [1024]uint16
	size := uint32(len(path))
	if err := windows.QueryFullProcessImageName(proc, 0, &path[0], &size); err == nil {
		info.exe = strings.ToLower(filepath.Base(windows.UTF16ToString(path[:size])))
	}
	return info
}

// acquireSingleInstance returns false if another tracker is already running.
// The mutex handle is intentionally kept open for the life of the process.
func acquireSingleInstance() bool {
	name, _ := windows.UTF16PtrFromString(`Local\StudyTrackerDaemon`)
	_, err := windows.CreateMutex(nil, false, name)
	return !errors.Is(err, windows.ERROR_ALREADY_EXISTS)
}

// platformDataDir is where the database, logs and settings live:
// %LOCALAPPDATA%\StudyTracker.
func platformDataDir() string {
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		return filepath.Join(v, "StudyTracker")
	}
	return "."
}

// logPlatformNotes logs anything the user should know about this
// platform (nothing, on Windows).
func logPlatformNotes(lg *log.Logger) {}
