//go:build !windows

package control

import (
	"context"
	"time"

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
func stopUserHosts(context.Context, time.Duration) ([]uint32, error) {
	return nil, mount.ErrNotSupported
}
