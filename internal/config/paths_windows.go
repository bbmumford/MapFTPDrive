//go:build windows

package config

import (
	"errors"
	"os"
	"path/filepath"
)

func DefaultPath(scope Scope) (string, error) {
	var base string
	switch scope {
	case User:
		base = os.Getenv("LOCALAPPDATA")
	case System:
		base = os.Getenv("ProgramData")
	default:
		return "", errors.New("invalid configuration scope")
	}
	if base == "" {
		return "", errors.New("Windows configuration directory is unavailable")
	}
	return filepath.Join(base, "MapFTPDrive", "endpoints.json"), nil
}
