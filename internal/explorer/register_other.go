//go:build !windows

package explorer

import "errors"

var errWindowsOnly = errors.New("Explorer integration is only supported on Windows")

func RemoveUserRegistration() error { return errWindowsOnly }
