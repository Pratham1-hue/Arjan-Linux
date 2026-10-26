// Command nmhost is the Native Messaging bridge between the browser
// extension and the Arjan daemon.
//
// Chrome/Edge (and other Chromium browsers) launch this executable
// (see host/com.studytracker.json) whenever the extension sends a
// message. Messages arrive on stdin in the Chrome native-messaging
// framing: a 4-byte little-endian length followed by that many bytes of
// JSON. Each message is forwarded, as a single newline-terminated JSON
// object, to the daemon's local socket (a named pipe on Windows, a Unix
// socket on Linux — see nmhost_windows.go / nmhost_linux.go).
//
// The process stays alive as long as Chrome keeps stdin open, so a
// long-lived `connectNative()` port costs only one process launch.
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

const maxMessageBytes = 1 << 20 // Chrome caps messages at 1 MB

func main() {
	// Silent by default; errors go to a log file so a failure never
	// surfaces as a browser error popup.
	lg := log.New(io.Discard, "", 0)
	if dir := logDir(); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		if f, err := os.OpenFile(filepath.Join(dir, "nmhost.log"),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			defer f.Close()
			lg = log.New(f, "nmhost ", log.LstdFlags)
		}
	}

	browser := detectBrowser()
	lg.Printf("started for browser=%q", browser)

	for {
		payload, err := readMessage(os.Stdin)
		if err != nil {
			if err != io.EOF {
				lg.Printf("read: %v", err)
			}
			return
		}
		payload = tagBrowser(payload, browser)
		if err := forward(payload); err != nil {
			lg.Printf("forward: %v", err) // daemon not running; Chrome retries on next message
			return
		}
	}
}

// tagBrowser adds "browser":"chrome" to a JSON message.
func tagBrowser(payload []byte, browser string) []byte {
	if browser == "" {
		return payload
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return payload
	}
	m["browser"] = browser
	if out, err := json.Marshal(m); err == nil {
		return out
	}
	return payload
}

func readMessage(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(hdr[:])
	if n == 0 || n > maxMessageBytes {
		return nil, fmt.Errorf("bad message length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// forward hands one message to the daemon over its local socket.
func forward(payload []byte) error {
	c, err := daemonDial()
	if err != nil {
		return fmt.Errorf("daemon not running: %w", err)
	}
	defer c.Close()
	// The daemon reads one JSON object per line.
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write(append(payload, '\n')); err != nil {
		return err
	}
	return nil
}
