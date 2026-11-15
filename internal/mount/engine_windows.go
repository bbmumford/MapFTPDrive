//go:build windows

package mount

import (
	"context"
	"sync"

	"github.com/rclone/rclone/cmd/mountlib"
)

type windowsEngine struct{}

func newPlatformEngine() Engine {
	return windowsEngine{}
}

type windowsHandle struct {
	point   *mountlib.MountPoint
	release func()
	once    sync.Once
}

// Wait blocks until the mount ends, whether unmounted on request or torn
// down by WinFsp, and then drops the endpoint's FTPS pin registration.
func (h *windowsHandle) Wait() error {
	err := h.point.Wait()
	h.once.Do(h.release)
	return err
}

func (h *windowsHandle) Unmount() error {
	err := h.point.Unmount()
	h.once.Do(h.release)
	return err
}

// Mount connects with a context detached from the caller's cancellation:
// rclone backends may keep the construction context for later operations,
// and a request-scoped context ends while the drive stays mounted. The
// connect is bounded by connectTimeout and lowLevelRetries instead.
func (windowsEngine) Mount(ctx context.Context, spec Spec) (Handle, error) {
	remote, release, err := newFs(context.WithoutCancel(ctx), spec)
	if err != nil {
		return nil, err
	}
	mountOptions, vfsOptions := mountSettings(spec.Endpoint)
	point := mountlib.NewMountPoint(
		mount,
		spec.Endpoint.DriveLetter+":",
		remote,
		&mountOptions,
		&vfsOptions,
	)
	if _, err := point.Mount(); err != nil {
		release()
		shutdownFs(remote)
		return nil, err
	}
	return &windowsHandle{point: point, release: release}, nil
}

func (windowsEngine) Probe(ctx context.Context, spec Spec) error {
	return probe(ctx, spec)
}
