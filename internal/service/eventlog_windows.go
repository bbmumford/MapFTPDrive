//go:build windows

package service

import (
	"golang.org/x/sys/windows/svc/eventlog"
	"mapftpdrive/internal/logging"
)

// Event IDs written to the Application log under the MapFTPDrive source.
const (
	eventStartRefused = 1
)

// registerEventSource registers the Application log source the service
// and the hosts write to (logging.InstallEventSource). The MSI writes the
// same registry values itself.
func registerEventSource() error {
	return logging.InstallEventSource()
}

func removeEventSource() error {
	return logging.RemoveEventSource()
}

func logError(id uint32, message string) {
	if log, err := eventlog.Open(Name); err == nil {
		_ = log.Error(id, message)
		_ = log.Close()
	}
}
