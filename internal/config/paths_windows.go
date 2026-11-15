//go:build windows

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func DefaultPath(scope Scope) (string, error) {
	var base string
	switch scope {
	case User:
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("Windows configuration directory is unavailable")
		}
		base = filepath.Join(base, "MapFTPDrive")
	case System:
		var err error
		base, err = SystemDirectory()
		if err != nil {
			return "", err
		}
	default:
		return "", errors.New("invalid configuration scope")
	}
	return filepath.Join(base, "endpoints.json"), nil
}

// SystemDirectory returns %ProgramData%\MapFTPDrive, which holds the system
// endpoint store, the SFTP host keys the service pins and the readable drive
// index. Only SYSTEM and Administrators may write it; see
// PrepareSystemDirectory and SecureSystemDirectory.
func SystemDirectory() (string, error) {
	base := os.Getenv("ProgramData")
	if base == "" {
		return "", errors.New("Windows configuration directory is unavailable")
	}
	return filepath.Join(base, "MapFTPDrive"), nil
}

// isSystemStore reports whether path is the system endpoint store, whose
// saves also refresh the drive index.
func isSystemStore(path string) bool {
	system, err := DefaultPath(System)
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(path), filepath.Clean(system))
}
