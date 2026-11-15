//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"

	"mapftpdrive/internal/config"
	"mapftpdrive/internal/explorer"
)

var errWindowsOnly = errors.New("this application runs on Windows")

func launchUI(config.Scope) error                 { return errWindowsOnly }
func installExplorerIntegration() error           { return explorer.Install() }
func installDependencies(string) error            { return errWindowsOnly }
func uninstallExplorerIntegration() error         { return explorer.Uninstall() }
func runService() error                           { return errWindowsOnly }
func runDaemon(config.Scope) error                { return errWindowsOnly }
func runDriveAction(string, string, string) error { return errWindowsOnly }
func mountUnmountedDrive() error                  { return errWindowsOnly }

func reportError(err error, _ bool) {
	fmt.Fprintln(os.Stderr, "MapFTPDrive:", err)
}
