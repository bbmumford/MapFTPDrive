//go:build !windows

package config

import (
	"errors"
	"os"
	"path/filepath"
)

func DefaultPath(scope Scope) (string, error) {
	if scope != User {
		return "", errors.New("system configuration is only available on Windows")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "MapFTPDrive", "endpoints.json"), nil
}
