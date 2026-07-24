//go:build !windows

package ui

import (
	"errors"

	"mapftpdrive/internal/config"
)

func showMapper(config.Scope) (MapperResult, error) {
	return MapperResult{}, errors.New("the native mapper is only available on Windows")
}
func confirmRemove(string, string) bool { return false }
func promptRename(string) (string, error) {
	return "", errors.New("the native mapper is only available on Windows")
}
func confirmHostKey(string, string, string) bool { return false }
func showError(string, error)                    {}
