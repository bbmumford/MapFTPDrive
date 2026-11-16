//go:build !windows

package logging

import (
	"errors"
	"io"
	"log/slog"
)

var errNoEventLog = errors.New("the Windows Event Log is unavailable")

func InstallEventSource() error { return errNoEventLog }
func RemoveEventSource() error  { return errNoEventLog }

func openEventLog(string) (slog.Handler, io.Closer, error) {
	return nil, nil, errNoEventLog
}
