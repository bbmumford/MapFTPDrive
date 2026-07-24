//go:build !windows

package mount

import (
	"context"

	"mapftpdrive/internal/config"
)

type unsupportedEngine struct{}

func newPlatformEngine() Engine {
	return unsupportedEngine{}
}

func (unsupportedEngine) Mount(context.Context, config.Endpoint, Secret) (Handle, error) {
	return nil, ErrNotSupported
}
