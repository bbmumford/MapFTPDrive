package control

import (
	"context"

	"mapftpdrive/internal/config"
)

type Operation string

const (
	SaveAndMount Operation = "save-and-mount"
	Mount        Operation = "mount"
	Unmount      Operation = "unmount"
	Remove       Operation = "remove"
	Rename       Operation = "rename"
	Ping         Operation = "ping"
)

type Request struct {
	Operation Operation        `json:"operation"`
	Drive     string           `json:"drive,omitempty"`
	Name      string           `json:"name,omitempty"`
	Endpoint  *config.Endpoint `json:"endpoint,omitempty"`
	Password  string           `json:"password,omitempty"`
}

type Response struct {
	Error string `json:"error,omitempty"`
}

type Handler func(context.Context, Request) error

func PipeName(scope config.Scope) (string, error) {
	return pipeName(scope)
}

func Serve(ctx context.Context, scope config.Scope, handler Handler) error {
	return serve(ctx, scope, handler)
}

func Send(ctx context.Context, scope config.Scope, request Request) error {
	return send(ctx, scope, request)
}
