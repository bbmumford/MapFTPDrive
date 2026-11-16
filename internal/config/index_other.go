//go:build !windows

package config

import "os"

// writeDriveIndexFile atomically replaces the drive index with data and
// makes it world-readable, mirroring the Users read grant applied on
// Windows.
func writeDriveIndexFile(path string, data []byte) error {
	return writeFileDurable(path, data, func(temp string) error {
		return os.Chmod(temp, 0o644) //nolint:gosec // G302: the index is meant to be readable by every local user.
	})
}
