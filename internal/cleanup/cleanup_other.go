//go:build !windows

package cleanup

import "errors"

var errWindowsOnly = errors.New("uninstall cleanup is only available on Windows")

func run(Options) error       { return errWindowsOnly }
func purgeCurrentUser() error { return errWindowsOnly }
func isLocalSystem() bool     { return false }
