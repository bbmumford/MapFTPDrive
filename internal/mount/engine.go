package mount

import (
	"context"
	"errors"

	"mapftpdrive/internal/config"
)

var ErrNotSupported = errors.New("mounting is only supported on Windows")

type Secret struct {
	Password string
}

type Handle interface {
	Wait() error
	Unmount() error
}

type Engine interface {
	Mount(context.Context, config.Endpoint, Secret) (Handle, error)
}

func NewEngine() Engine {
	return newPlatformEngine()
}
