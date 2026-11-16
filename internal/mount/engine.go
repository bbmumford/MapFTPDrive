// Package mount builds rclone FTP/SFTP filesystems for endpoints and, on
// Windows, serves them as WinFsp drives through the in-process cmount
// adapter.
package mount

import (
	"context"
	"errors"

	"mapftpdrive/internal/config"
)

var ErrNotSupported = errors.New("mounting is only supported on Windows")

// Secret is the endpoint's password, or its private-key passphrase when the
// endpoint uses key authentication.
type Secret struct {
	Password string
}

// Spec is everything one mount or probe needs besides the engine's
// process-wide settings.
type Spec struct {
	Endpoint config.Endpoint
	Secret   Secret
	// KnownHostsFile holds the pinned SFTP host key for rclone builds whose
	// sftp backend has no host_keys option; it is ignored otherwise.
	KnownHostsFile string
}

type Handle interface {
	Wait() error
	Unmount() error
}

type Engine interface {
	// Mount connects and serves the endpoint on its drive letter.
	Mount(context.Context, Spec) (Handle, error)
	// Probe connects and lists the remote root without mounting.
	Probe(context.Context, Spec) error
}

// Options are process-wide engine settings, fixed by the first NewEngine.
type Options struct {
	// CacheDir is the scope's rclone cache root (config.Layout.CacheRoot);
	// each endpoint's VFS cache lives in a subdirectory named by
	// config.EndpointKey.
	CacheDir string
}

func NewEngine(options Options) Engine {
	configureRclone(options)
	return newPlatformEngine()
}
