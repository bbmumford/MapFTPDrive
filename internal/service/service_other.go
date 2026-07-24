//go:build !windows

package service

func run() error           { return ErrWindowsOnly }
func install(string) error { return ErrWindowsOnly }
func uninstall() error     { return ErrWindowsOnly }
func ensureStarted() error { return ErrWindowsOnly }
