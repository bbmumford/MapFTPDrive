//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"

	"mapftpdrive/internal/config"
)

var errWindowsOnly = errors.New("this application runs on Windows")

func launchUI(config.Scope) error                 { return errWindowsOnly }
func installMachine() error                       { return errWindowsOnly }
func installDependencies(string) error            { return errWindowsOnly }
func uninstallMachine() error                     { return errWindowsOnly }
func runService() error                           { return errWindowsOnly }
func runDaemon(config.Scope) error                { return errWindowsOnly }
func runDriveAction(string, string, string) error { return errWindowsOnly }
func mountUnmountedDrive() error                  { return errWindowsOnly }
func prepareSystemData() error                    { return errWindowsOnly }

func reportError(err error, _ bool) {
	fmt.Fprintln(os.Stderr, "MapFTPDrive:", err)
}
