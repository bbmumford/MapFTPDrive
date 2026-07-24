package ui

import (
	"errors"

	"mapftpdrive/internal/config"
)

var ErrCancelled = errors.New("mapping cancelled")

type MapperResult struct {
	Endpoint config.Endpoint
	Password string
}

func ShowMapper(scope config.Scope) (MapperResult, error) {
	return showMapper(scope)
}

func ConfirmRemove(name, drive string) bool {
	return confirmRemove(name, drive)
}

func PromptRename(current string) (string, error) {
	return promptRename(current)
}

func ConfirmHostKey(host, algorithm, fingerprint string) bool {
	return confirmHostKey(host, algorithm, fingerprint)
}

func ShowError(title string, err error) {
	showError(title, err)
}
