//go:build windows

package service

import (
	"errors"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc/eventlog"
)

// Event IDs written to the Application log under the MapFTPDrive source.
const (
	eventStartRefused = 1
)

const eventSourceKey = `SYSTEM\CurrentControlSet\Services\EventLog\Application\` + Name

// registerEventSource registers the Application log source the service
// writes to, using EventCreate.exe as the message file so entries display
// their text. The MSI writes the same registry values itself.
func registerEventSource() error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, eventSourceKey, registry.QUERY_VALUE)
	if err == nil {
		key.Close()
		return nil
	}
	if !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return eventlog.InstallAsEventCreate(Name, eventlog.Error|eventlog.Warning|eventlog.Info)
}

func removeEventSource() error {
	err := eventlog.Remove(Name)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

func logError(id uint32, message string) {
	if log, err := eventlog.Open(Name); err == nil {
		_ = log.Error(id, message)
		_ = log.Close()
	}
}
