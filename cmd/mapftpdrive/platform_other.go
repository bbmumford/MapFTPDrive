//go:build !windows

package main

import "errors"

import "mapftpdrive/internal/config"
import "mapftpdrive/internal/explorer"

var errWindowsOnly = errors.New("this application runs on Windows")

func launchUI(config.Scope) error                 { return errWindowsOnly }
func installExplorerIntegration() error           { return explorer.Install() }
func uninstallExplorerIntegration() error         { return explorer.Uninstall() }
func runService() error                           { return errWindowsOnly }
func runDaemon(config.Scope) error                { return errWindowsOnly }
func runDriveAction(string, string, string) error { return errWindowsOnly }
