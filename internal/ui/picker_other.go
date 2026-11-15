//go:build !windows

package ui

import "errors"

func pickOne(string, string, string, []string) (int, error) {
	return -1, errors.New("the native mapper is only available on Windows")
}

func showInfo(string, string) {}
