package main

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"net"
	"strings"
	"time"
)

// The browser launches the small native-messaging bridge (nmhost) for
// the extension. nmhost forwards each message to this long-running
// daemon over a local socket, one JSON object per line. The transport
// differs per OS (named pipe on Windows, Unix socket on Linux) and
// lives in pipe_windows.go / pipe_linux.go; the protocol is shared.

// inbound is the wire format between nmhost and the daemon. It is also
// the format the extension sends.
//
//	{"type":"tab","url":"https://...","title":"...","incognito":false,"browser":"chrome"}
//	{"type":"shutdown"}                     (used by `arjan --stop`)
//
// "browser" is added by nmhost (it detects which browser launched it).
type inbound struct {
	Type      string `json:"type"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Incognito bool   `json:"incognito"`
	Browser   string `json:"browser"`
}

func serveBrowserPipe(ctx context.Context, t *Tracker, onShutdown func(), lg *log.Logger) error {
	l, err := browserListener(lg)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		l.Close()
	}()

	for {
		c, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			lg.Printf("browser socket accept: %v", err)
			time.Sleep(time.Second) // avoid a hot loop on a persistent error
			continue
		}
		go handlePipeConn(c, t, onShutdown, lg)
	}
}

func handlePipeConn(c net.Conn, t *Tracker, onShutdown func(), lg *log.Logger) {
	defer c.Close()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 0, 4*1024), 1<<20)
	for sc.Scan() {
		var m inbound
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		switch m.Type {
		case "tab":
			t.OnBrowserTab(m.URL, m.Title, m.Incognito, strings.ToLower(m.Browser))
		case "shutdown":
			lg.Printf("shutdown requested")
			onShutdown()
			return
		}
	}
}

// sendStop asks a running daemon to exit cleanly. Returns a process exit code.
func sendStop() int {
	c, err := browserDial()
	if err != nil {
		return 1
	}
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write([]byte("{\"type\":\"shutdown\"}\n")); err != nil {
		return 1
	}
	return 0
}
