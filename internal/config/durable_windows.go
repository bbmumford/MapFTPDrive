//go:build windows

package config

import "golang.org/x/sys/windows"

// replaceFile renames with MOVEFILE_WRITE_THROUGH so the rename is on disk
// before it returns; Windows offers no directory-handle flush, so this is the
// durability point that syncDir provides on other systems.
func replaceFile(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, target, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func syncDir(string) error { return nil }
