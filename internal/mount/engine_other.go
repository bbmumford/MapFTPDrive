//go:build !windows

package mount

import "context"

// otherEngine cannot mount (WinFsp is Windows-only) but probes connections
// with the same rclone backends, which keeps connection logic testable.
type otherEngine struct{}

func newPlatformEngine() Engine {
	return otherEngine{}
}

func (otherEngine) Mount(context.Context, Spec) (Handle, error) {
	return nil, ErrNotSupported
}

func (otherEngine) Probe(ctx context.Context, spec Spec) error {
	return probe(ctx, spec)
}
