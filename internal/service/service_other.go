//go:build !windows

package service

func run() error                  { return ErrWindowsOnly }
func install(string) error        { return ErrWindowsOnly }
func uninstall() error            { return ErrWindowsOnly }
func ensureStarted() error        { return ErrWindowsOnly }
func configure() error            { return ErrWindowsOnly }
func registerEventSource() error  { return ErrWindowsOnly }
func removeEventSource() error    { return ErrWindowsOnly }
func runTask(func() error) error  { return ErrWindowsOnly }
func runAsService([]string) error { return ErrWindowsOnly }
