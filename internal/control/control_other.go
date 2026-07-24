//go:build !windows

package control

import (
	"context"

	"mapftpdrive/internal/config"
	"mapftpdrive/internal/mount"
)

func pipeName(config.Scope) (string, error) { return "", mount.ErrNotSupported }
func serve(context.Context, config.Scope, Handler) error {
	return mount.ErrNotSupported
}
func send(context.Context, config.Scope, Request) error {
	return mount.ErrNotSupported
}
