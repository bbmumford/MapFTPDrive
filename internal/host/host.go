// Package host runs a scope's mount host: the per-user host process
// (daemon --scope user) and the LocalSystem service body (scope system).
// The host takes the scope's control pipe before it mounts anything, so the
// pipe doubles as a singleton lock: a second host for the same scope exits
// without touching any drive.
package host

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"mapftpdrive/internal/app"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
	"mapftpdrive/internal/credentials"
	"mapftpdrive/internal/logging"
	"mapftpdrive/internal/mount"
	"mapftpdrive/internal/netwatch"
)

// networkWait bounds how long a freshly started host waits for a usable
// network before its first mount attempts; endpoints then retry with
// backoff and on every network change.
const networkWait = 90 * time.Second

// Transport is the control channel; the pipe implementation is in
// internal/control and tests substitute an in-memory one.
type Transport interface {
	ServeReady(ctx context.Context, scope config.Scope, handler control.Handler, ready func()) error
	Call(ctx context.Context, scope config.Scope, request control.Request) (control.Response, error)
}

type pipeTransport struct{}

func (pipeTransport) ServeReady(ctx context.Context, scope config.Scope, handler control.Handler, ready func()) error {
	return control.ServeReady(ctx, scope, handler, ready)
}

func (pipeTransport) Call(ctx context.Context, scope config.Scope, request control.Request) (control.Response, error) {
	return control.Call(ctx, scope, request)
}

// Deps are a host's collaborators; Run builds the production set.
type Deps struct {
	Scope            config.Scope
	StorePath        string
	Transport        Transport
	Credentials      credentials.Store
	Engine           mount.Engine
	Prober           app.Prober
	UsedLetters      func() (uint32, error)
	OtherScopeDrives func() ([]config.DriveIndexEntry, error)
	// Network delivers change notifications; nil disables them.
	Network netwatch.Watcher
	// WaitForNetwork blocks until the network is usable or gives up; nil
	// mounts immediately.
	WaitForNetwork func(context.Context) bool
	Logger         *slog.Logger
	Backoff        app.Backoff
}

// Run is the production host for scope.
func Run(ctx context.Context, scope config.Scope) error {
	path, err := config.DefaultPath(scope)
	if err != nil {
		return err
	}
	layout := config.LayoutFor(path)
	logger, logErr := logging.Open(logging.Options{Dir: layout.LogDir(), EventLog: scope == config.System})
	defer logger.Close()
	slog.SetDefault(logger.Logger)
	mount.RouteLogs(logger.Handler())
	if logErr != nil {
		logger.Warn("logging is incomplete", "error", logErr)
	}

	watcher := netwatch.New()
	defer watcher.Close()
	deps := Deps{
		Scope:       scope,
		StorePath:   path,
		Transport:   pipeTransport{},
		Credentials: credentials.NewStore(),
		Engine:      mount.NewEngine(mount.Options{CacheDir: layout.CacheRoot()}),
		UsedLetters: mount.UsedDriveLetters,
		Network:     watcher,
		WaitForNetwork: func(ctx context.Context) bool {
			return netwatch.WaitReady(ctx, watcher.Changes(), netwatch.Snapshot, networkWait)
		},
		Logger: logger.Logger,
	}
	if scope == config.User {
		deps.OtherScopeDrives = systemDrives
	}
	err = RunWith(ctx, deps)
	if err != nil {
		logger.Error("host stopped", "scope", scope, "error", err)
	}
	return err
}

// systemDrives reads the system scope's user-readable drive index.
func systemDrives() ([]config.DriveIndexEntry, error) {
	path, err := config.DefaultPath(config.System)
	if err != nil {
		return nil, err
	}
	return config.ReadDriveIndex(config.DriveIndexPath(path))
}

// RunWith serves the scope's control pipe until ctx ends. It returns nil
// without mounting when another live host already owns the pipe.
func RunWith(ctx context.Context, deps Deps) error {
	logger := deps.Logger
	if logger == nil {
		logger = logging.Discard().Logger
	}
	store := config.NewStore(deps.StorePath)
	if deps.Scope == config.System {
		index := config.DriveIndexPath(deps.StorePath)
		store.SetSaveHook(func(endpoints []config.Endpoint) {
			if err := config.WriteDriveIndex(index, config.System, endpoints); err != nil {
				logger.Error("write drive index", "error", err)
			}
		})
	}
	supervisor := app.New(app.Options{
		Store:            store,
		Credentials:      deps.Credentials,
		Engine:           deps.Engine,
		Prober:           deps.Prober,
		UsedLetters:      deps.UsedLetters,
		OtherScopeDrives: deps.OtherScopeDrives,
		Backoff:          deps.Backoff,
		Logger:           logger,
	})

	// Loading first lets the very first status request list every drive;
	// it only reads, so a second host that will exit may do it too.
	endpoints, loadErr := supervisor.Load()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ready := make(chan struct{})
	served := make(chan error, 1)
	go func() {
		served <- deps.Transport.ServeReady(ctx, deps.Scope, NewHandler(deps.Scope, supervisor), func() { close(ready) })
	}()
	select {
	case err := <-served:
		if ctx.Err() != nil {
			return nil
		}
		if anotherHost(ctx, deps) {
			logger.Info("another host already serves this scope; exiting", "scope", deps.Scope)
			return nil
		}
		if err == nil {
			err = errors.New("control pipe closed before it was ready")
		}
		return err
	case <-ready:
	}
	logger.Info("host started", "scope", deps.Scope)

	if loadErr != nil {
		logger.Error("endpoint store is unreadable; serving repair requests only", "error", loadErr)
	}
	if deps.Scope == config.System && loadErr == nil {
		if err := config.WriteDriveIndex(config.DriveIndexPath(deps.StorePath), config.System, endpoints); err != nil {
			logger.Error("write drive index", "error", err)
		}
	}
	started := make(chan struct{})
	go func() {
		defer close(started)
		if deps.WaitForNetwork != nil && !deps.WaitForNetwork(ctx) && ctx.Err() == nil {
			logger.Warn("no usable network yet; mounting anyway and retrying on network changes")
		}
		if ctx.Err() != nil {
			return
		}
		if err := supervisor.Start(ctx); err != nil {
			logger.Error("start supervision", "error", err)
		}
		if deps.Network == nil {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-deps.Network.Changes():
				logger.Info("network changed; retrying waiting drives")
				supervisor.NetworkChanged()
			}
		}
	}()

	err := <-served
	cancel()
	<-started
	supervisor.Stop()
	logger.Info("host stopped", "scope", deps.Scope)
	if ctx.Err() != nil && err == nil {
		return nil
	}
	return err
}

// anotherHost reports whether a different live host answers on the pipe.
func anotherHost(ctx context.Context, deps Deps) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := deps.Transport.Call(ctx, deps.Scope, control.Request{Operation: control.Ping})
	return err == nil
}
