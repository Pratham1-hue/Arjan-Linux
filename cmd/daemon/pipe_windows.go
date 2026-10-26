//go:build windows

package main

import (
	"log"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// pipeName is the named pipe the browser bridge forwards tabs to.
const pipeName = `\\.\pipe\StudyTrackerBrowser`

func browserListener(_ *log.Logger) (net.Listener, error) {
	return winio.ListenPipe(pipeName, &winio.PipeConfig{
		InputBufferSize:  64 * 1024,
		OutputBufferSize: 4 * 1024,
	})
}

func browserDial() (net.Conn, error) {
	timeout := 2 * time.Second
	return winio.DialPipe(pipeName, &timeout)
}
