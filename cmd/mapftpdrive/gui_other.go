//go:build !windows

package main

import (
	"errors"

	"mapftpdrive/internal/config"
)

func runMapper(config.Scope) error {
	return errors.New("mapping is only available on Windows")
}

func runManager() error {
	return errors.New("drive management is only available on Windows")
}
