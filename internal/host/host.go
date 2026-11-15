package host

import (
	"context"
	"errors"
	"fmt"

	"mapftpdrive/internal/app"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
	"mapftpdrive/internal/credentials"
	"mapftpdrive/internal/mount"
)

func Run(ctx context.Context, scope config.Scope) error {
	path, err := config.DefaultPath(scope)
	if err != nil {
		return err
	}
	engine := mount.NewEngine(mount.Options{CacheDir: config.LayoutFor(path).CacheRoot()})
	supervisor := app.NewSupervisor(config.NewStore(path), credentials.NewStore(), engine)
	// A failed restored endpoint must not prevent the host from accepting
	// repair, remove, or remount commands for the remaining endpoints.
	_ = supervisor.Start(ctx)
	defer supervisor.Stop()

	handle := func(_ context.Context, request control.Request) error {
		switch request.Operation {
		case control.Ping:
			return nil
		case control.SaveAndMount:
			if request.Endpoint == nil {
				return errors.New("endpoint is required")
			}
			endpoint := request.Endpoint.Normalized()
			if endpoint.Scope != scope {
				return fmt.Errorf("endpoint scope %q does not match host scope %q", endpoint.Scope, scope)
			}
			if err := supervisor.Save(endpoint, request.Password); err != nil {
				return err
			}
			return supervisor.MountByDrive(ctx, endpoint.DriveLetter)
		case control.Mount:
			return supervisor.MountByDrive(ctx, request.Drive)
		case control.Unmount:
			return supervisor.UnmountByDrive(request.Drive, true)
		case control.Remove:
			return supervisor.UnmountByDrive(request.Drive, false)
		case control.Rename:
			return supervisor.RenameByDrive(request.Drive, request.Name)
		default:
			return fmt.Errorf("unsupported operation %q", request.Operation)
		}
	}
	return control.Serve(ctx, scope, func(ctx context.Context, request control.Request) (control.Response, error) {
		return control.Response{}, handle(ctx, request)
	})
}
