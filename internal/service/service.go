package service

import "errors"

var ErrWindowsOnly = errors.New("Windows service support is unavailable")

const Name = "MapFTPDrive"

func Run() error                      { return run() }
func Install(executable string) error { return install(executable) }
func Uninstall() error                { return uninstall() }
func EnsureStarted() error            { return ensureStarted() }

// RegisterEventSource registers the Application event log source the
// service reports start refusals and warnings under.
func RegisterEventSource() error { return registerEventSource() }

// RemoveEventSource removes the event log source on uninstall.
func RemoveEventSource() error { return removeEventSource() }
