//go:build !windows

package ui

import (
	"errors"
)

var errNoWindows = errors.New("the native interface is only available on Windows")

func showMapper(MapperOptions) (MapperResult, error) { return MapperResult{}, errNoWindows }
func showManager(ManagerOptions) error               { return errNoWindows }
func confirmRemove(string, string) bool              { return false }
func promptRename(string) (string, error)            { return "", errNoWindows }
func confirmHostKey(string, string, string) bool     { return false }
func showErrorBox(string, error)                     {}
