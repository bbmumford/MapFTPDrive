// Package control is the request/response protocol between MapFTPDrive's
// user-facing commands and the mount hosts (the per-user host and the
// LocalSystem service), carried over a per-scope named pipe.
//
// One exchange is one request followed by host messages: StatusAccepted as
// soon as the host has read the request, StatusWorking every
// ProgressInterval while the handler runs, and a final message that carries
// the handler's Response. Both sides bound the whole exchange by
// RequestTimeout.
package control

import (
	"context"
	"errors"
	"time"

	"mapftpdrive/internal/config"
)

type Operation string

const (
	// Map tests a new definition, mounts it and only then saves it; a
	// failure at any step leaves no definition or credential behind.
	Map Operation = "map"
	// SaveAndMount is an alias of Map and is handled identically.
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
	// Shutdown makes Serve return, so the host stops its drives and exits.
	// Serve answers it itself; the handler never sees it. The uninstaller
	// sends it to every user host before removing the program.
	Shutdown Operation = "shutdown"
)

// RequestTimeout bounds one request end to end. The host cancels the
// handler's context after it, and callers use it as the deadline of the
// context they pass to Send or Call, so neither side gives up while the
// other is still within its budget for a slow connect and mount. Handlers
// that wait on a mount stop somewhat earlier (see internal/host) so their
// own answer, rather than a timeout, reaches the client.
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

// Response is one message from the host. Status is StatusAccepted or
// StatusWorking for interim messages, which carry nothing else; any other
// status ends the exchange, with Error set when the request failed and the
// remaining fields filled by the handler.
type Response struct {
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
	// Message is an informational result, such as a mount that is still
	// connecting when the request returns.
	Message  string           `json:"message,omitempty"`
	Endpoint *config.Endpoint `json:"endpoint,omitempty"`
	// Report answers the Status operation.
	Report *StatusReport `json:"report,omitempty"`
}

// Final reports whether r ends the exchange.
func (r Response) Final() bool {
	return r.Status != StatusAccepted && r.Status != StatusWorking
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

// Handler answers one request. A non-nil error is sent as Response.Error
// alongside the returned Response. The context ends after RequestTimeout.
type Handler func(context.Context, Request) (Response, error)

func PipeName(scope config.Scope) (string, error) {
	return pipeName(scope)
}

// Serve accepts requests on the scope's pipe until ctx ends or a Shutdown
// request arrives, then returns nil. Each handler call receives a context
// that ends after RequestTimeout.
func Serve(ctx context.Context, scope config.Scope, handler Handler) error {
	return serve(ctx, scope, handler, nil)
}

// ServeReady is Serve with a callback run once this process owns the
// scope's pipe and is accepting connections. The host starts mounting only
// after it, so a second host that fails to take the pipe never mounts.
func ServeReady(ctx context.Context, scope config.Scope, handler Handler, ready func()) error {
	return serve(ctx, scope, handler, ready)
}

// StopUserHosts sends Shutdown to every user host on this computer and
// waits up to timeout for each to exit. It returns the process IDs of hosts
// that are still running, which the caller may terminate. Only SYSTEM (and
// each host's own user) can open user pipes, so the uninstaller runs it as
// LocalSystem. No credential is sent, so the pipe server is not verified.
func StopUserHosts(ctx context.Context, timeout time.Duration) ([]uint32, error) {
	return stopUserHosts(ctx, timeout)
}

// Send verifies the host serving the scope's pipe, then delivers request
// and waits for its final response or the end of ctx.
func Send(ctx context.Context, scope config.Scope, request Request) error {
	_, err := send(ctx, scope, request)
	return err
}

// Call is Send returning the host's final response. When the host reports
// a failure, the response is returned together with the error.
func Call(ctx context.Context, scope config.Scope, request Request) (Response, error) {
	return send(ctx, scope, request)
}
