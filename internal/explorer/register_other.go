//go:build !windows

package explorer

import "errors"

var errWindowsOnly = errors.New("Explorer integration is only supported on Windows")

func Install() error   { return errWindowsOnly }
func Uninstall() error { return errWindowsOnly }
