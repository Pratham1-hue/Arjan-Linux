//go:build linux

package main

import (
	"log"
	"net"
	"os"
	"time"
)

// browserListener creates the Unix socket the browser bridge
// (nmhost) connects to. It lives in $XDG_RUNTIME_DIR, which is
// private to the user; the file itself is chmod 0600 as well.
func browserListener(lg *log.Logger) (net.Listener, error) {
	path := browserAddress()
	// A crashed run can leave the socket behind; we already hold the
	// single-instance lock, so nothing else is listening on it.
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		lg.Printf("browser socket chmod: %v", err)
	}
	return &cleanupListener{Listener: l, path: path}, nil
}

func browserDial() (net.Conn, error) {
	return (&net.Dialer{Timeout: 2 * time.Second}).Dial("unix", browserAddress())
}
