package control

import (
	"context"
	"errors"
	"time"

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

// RequestTimeout bounds one request end to end. The host cancels the
// handler's context after it, and callers use it as the deadline of the
// context they pass to Send, so neither side gives up while the other is
// still within its budget for a slow connect and mount.
const RequestTimeout = 2 * time.Minute

// Interim response statuses. The host sends StatusAccepted as soon as it
// has read a valid request and StatusWorking at ProgressInterval while the
// handler runs, so a client can tell a slow mount from a dead host.
const (
	StatusAccepted = "accepted"
	StatusWorking  = "working"
	StatusDone     = "done"
)

// ProgressInterval is how often the host reports StatusWorking.
const ProgressInterval = 5 * time.Second

// ErrUntrustedHost reports that the process serving the control pipe is
// not the expected MapFTPDrive host: for the system scope it is not
// running as LocalSystem, and for the user scope it belongs to another
// account or logon session. Send refuses to write a request, which may
// carry a password, to such a process.
var ErrUntrustedHost = errors.New("the MapFTPDrive control pipe is served by an untrusted process")

type Request struct {
	Operation Operation        `json:"operation"`
	Drive     string           `json:"drive,omitempty"`
	Name      string           `json:"name,omitempty"`
	Endpoint  *config.Endpoint `json:"endpoint,omitempty"`
	Password  string           `json:"password,omitempty"`
}

// Response is one message from the host. Status is StatusAccepted or
// StatusWorking for interim messages; any other status ends the exchange,
// with Error set when the request failed.
type Response struct {
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Final reports whether r ends the exchange.
func (r Response) Final() bool {
	return r.Status != StatusAccepted && r.Status != StatusWorking
}

type Handler func(context.Context, Request) error

func PipeName(scope config.Scope) (string, error) {
	return pipeName(scope)
}

// Serve accepts requests on the scope's pipe until ctx ends. Each handler
// call receives a context that ends after RequestTimeout.
func Serve(ctx context.Context, scope config.Scope, handler Handler) error {
	return serve(ctx, scope, handler)
}

// Send verifies the host serving the scope's pipe, then delivers request
// and waits for its final response or the end of ctx.
func Send(ctx context.Context, scope config.Scope, request Request) error {
	return send(ctx, scope, request)
}
