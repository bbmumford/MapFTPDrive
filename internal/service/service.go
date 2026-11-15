package service

import "errors"

var ErrWindowsOnly = errors.New("service support requires Windows")

// ErrServiceMissing reports that the MapFTPDrive service is not installed.
var ErrServiceMissing = errors.New("the MapFTPDrive service is not installed")

const Name = "MapFTPDrive"

func Run() error                      { return run() }
func Install(executable string) error { return install(executable) }
func Uninstall() error                { return uninstall() }
func EnsureStarted() error            { return ensureStarted() }

// Configure applies delayed automatic start and the recovery actions to the
// installed service; the MSI runs it after creating the service.
func Configure() error { return configure() }

// RegisterEventSource registers the Application event log source the
// service reports start refusals and warnings under.
func RegisterEventSource() error { return registerEventSource() }

// RemoveEventSource removes the event log source on uninstall.
func RemoveEventSource() error { return removeEventSource() }

// RunTask runs task in a process the Service Control Manager started from
// a RunAsService command line, and reports its result as the service's
// exit code.
func RunTask(task func() error) error { return runTask(task) }

// RunAsService runs the installed service once with arguments appended to
// its "service" command line, as LocalSystem, and waits for it to finish.
// It returns ErrServiceMissing when the service is not installed.
func RunAsService(arguments ...string) error { return runAsService(arguments) }
