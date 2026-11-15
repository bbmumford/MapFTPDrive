//go:build !windows

package config

import "os"

// applyDriveIndexSecurity makes the index world-readable, mirroring the
// Users read grant applied on Windows.
func applyDriveIndexSecurity(path string) error {
	return os.Chmod(path, 0o644)
}
