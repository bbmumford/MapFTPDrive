//go:build !windows

package control

import (
	"context"

	"mapftpdrive/internal/config"
	"mapftpdrive/internal/mount"
)

func pipeName(config.Scope) (string, error) { return "", mount.ErrNotSupported }
func serve(context.Context, config.Scope, Handler, func()) error {
	return mount.ErrNotSupported
}
func send(context.Context, config.Scope, Request) (Response, error) {
	return Response{}, mount.ErrNotSupported
}
