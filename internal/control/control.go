// Package control is the request/response protocol between MapFTPDrive's
// user-facing commands and the mount hosts (the per-user host and the
// LocalSystem service), carried over a per-scope named pipe.
package control

import (
	"context"
	"time"

	"mapftpdrive/internal/config"
)

type Operation string

const (
	// Map tests a new definition, mounts it and only then saves it; a
	// failure at any step leaves no definition or credential behind.
	Map Operation = "map"
	// SaveAndMount is the original name of Map and is handled identically.
	SaveAndMount Operation = "save-and-mount"
	// Update replaces an existing definition (by Endpoint.ID) with the same
	// test-then-commit rule; an empty Password keeps the stored secret.
	Update Operation = "update"
	// Test connects with a definition (Endpoint, or the stored one named by
	// ID or Drive) and lists its root without mounting.
	Test    Operation = "test"
	Mount   Operation = "mount"
	Unmount Operation = "unmount"
	Remove  Operation = "remove"
	Rename  Operation = "rename"
	// Repin stores a new SFTP host key (KnownHost) or FTPS certificate
	// digest (CertificatePin) the user confirmed, then remounts.
	Repin  Operation = "repin"
	Status Operation = "status"
	Ping   Operation = "ping"
)

// Request names its target by ID or by Drive; ID wins when both are set.
type Request struct {
	Operation Operation        `json:"operation"`
	Drive     string           `json:"drive,omitempty"`
	ID        string           `json:"id,omitempty"`
	Name      string           `json:"name,omitempty"`
	Endpoint  *config.Endpoint `json:"endpoint,omitempty"`
	// Password is the account password, or the private-key passphrase for
	// key authentication. It is only ever held in memory and Credential
	// Manager.
	Password       string `json:"password,omitempty"`
	KnownHost      string `json:"knownHost,omitempty"`
	CertificatePin string `json:"certificatePin,omitempty"`
}

type Response struct {
	Error string `json:"error,omitempty"`
	// Message is an informational result, such as a mount that is still
	// connecting when the request returns.
	Message  string           `json:"message,omitempty"`
	Endpoint *config.Endpoint `json:"endpoint,omitempty"`
	Status   *StatusReport    `json:"status,omitempty"`
}

// State is an endpoint's supervision state.
type State string

const (
	StateDisabled   State = "disabled"
	StateConnecting State = "connecting"
	StateMounted    State = "mounted"
	// StateRetrying: the last attempt failed and another is scheduled at
	// RetryAt (sooner on a network change).
	StateRetrying State = "retrying"
	// StateAttention: retrying cannot help (changed or unpinned server
	// identity, missing credential); the user must act.
	StateAttention State = "attention"
)

// TrustProblem details an identity failure so the management window can
// show the pinned and presented fingerprints before a re-pin.
type TrustProblem struct {
	Kind      string `json:"kind"`
	Pinned    string `json:"pinned,omitempty"`
	Presented string `json:"presented,omitempty"`
}

const (
	TrustHostKeyChanged       = "host-key-changed"
	TrustHostKeyNotPinned     = "host-key-not-pinned"
	TrustCertificateChanged   = "certificate-changed"
	TrustCertificateUntrusted = "certificate-untrusted"
)

type EndpointStatus struct {
	Endpoint  config.Endpoint `json:"endpoint"`
	State     State           `json:"state"`
	LastError string          `json:"lastError,omitempty"`
	RetryAt   time.Time       `json:"retryAt,omitempty"`
	Since     time.Time       `json:"since,omitempty"`
	Attempts  int             `json:"attempts,omitempty"`
	Trust     *TrustProblem   `json:"trust,omitempty"`
}

// StatusReport answers Status for one scope.
type StatusReport struct {
	Scope     config.Scope     `json:"scope"`
	Endpoints []EndpointStatus `json:"endpoints"`
	// Problems lists stored entries that were skipped as invalid.
	Problems []config.Problem `json:"problems,omitempty"`
	// StoreError is set when the store could not be read at all.
	StoreError string `json:"storeError,omitempty"`
}

// Handler answers one request. A non-nil error is sent as Response.Error.
type Handler func(context.Context, Request) (Response, error)

func PipeName(scope config.Scope) (string, error) {
	return pipeName(scope)
}

func Serve(ctx context.Context, scope config.Scope, handler Handler) error {
	return serve(ctx, scope, handler, nil)
}

// ServeReady is Serve with a callback run once this process owns the
// scope's pipe and is accepting connections. The host starts mounting only
// after it, so a second host that fails to take the pipe never mounts.
func ServeReady(ctx context.Context, scope config.Scope, handler Handler, ready func()) error {
	return serve(ctx, scope, handler, ready)
}

func Send(ctx context.Context, scope config.Scope, request Request) error {
	_, err := send(ctx, scope, request)
	return err
}

// Call sends request and returns the host's full response.
func Call(ctx context.Context, scope config.Scope, request Request) (Response, error) {
	return send(ctx, scope, request)
}
