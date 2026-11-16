//go:build !windows

package config

import (
	"errors"
	"os"
	"path/filepath"
)

var errSystemScope = errors.New("system configuration is only available on Windows")

func DefaultPath(scope Scope) (string, error) {
	if scope != User {
		return "", errSystemScope
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "MapFTPDrive", "endpoints.json"), nil
}

func SystemDirectory() (string, error) { return "", errSystemScope }
func PrepareSystemDirectory() error    { return errSystemScope }
func SecureSystemDirectory() error     { return errSystemScope }
func VerifySystemDirectory() error     { return errSystemScope }

func isSystemStore(string) bool { return false }

func ensureStoreDirectory(dir string) error { return os.MkdirAll(dir, 0o700) }
