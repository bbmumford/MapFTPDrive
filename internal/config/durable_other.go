//go:build !windows

package config

import "os"

func replaceFile(from, to string) error {
	return os.Rename(from, to)
}

// syncDir flushes the directory entry created by the rename.
func syncDir(dir string) error {
	handle, err := os.Open(dir) //nolint:gosec // G304: the store's own directory, opened to flush the rename.
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}
