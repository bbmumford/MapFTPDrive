//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/host"
)

type handler struct{}

// Service-specific exit codes reported to the Service Control Manager.
const (
	exitHostFailed       = 1
	exitStopTimeout      = 2
	exitInsecureDataPath = 3
)

func (handler) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}
	if err := config.PrepareSystemDirectory(); err != nil {
		logError(eventStartRefused, fmt.Sprintf(
			"MapFTPDrive will not start because its data directory is not protected: %v. "+
				"Repair or reinstall MapFTPDrive to restore the directory's permissions.", err))
		return true, exitInsecureDataPath
	}
	if path, err := config.DefaultPath(config.System); err == nil {
		if err := config.NewStore(path).RefreshDriveIndex(); err != nil {
			logWarning(eventDriveIndexFailed, fmt.Sprintf(
				"MapFTPDrive could not update its drive index, so Explorer commands may not recognise system drives: %v", err))
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- host.Run(ctx, config.System) }()
	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			if err != nil {
				return true, exitHostFailed
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(20 * time.Second):
					return true, exitStopTimeout
				}
				return false, 0
			default:
			}
		}
	}
}

func run() error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		return errors.New("service mode must be started by the Service Control Manager")
	}
	return svc.Run(Name, handler{})
}

const (
	displayName = "MapFTPDrive system mount service"
	description = "Restores and supervises system-wide MapFTPDrive mounts."
	// stopTimeout exceeds the 20 seconds Execute allows the host to unmount.
	stopTimeout = 30 * time.Second
	// startTimeout bounds the wait for the service to report Running or to
	// stop, for example after refusing an unprotected data directory.
	startTimeout = 30 * time.Second
	// recoveryResetSeconds is the failure-count reset period: one day.
	recoveryResetSeconds = 86400
)

// recoveryActions restart the service after a crash or an error exit. Both
// installers apply them through applyServiceSettings: the EXE from install,
// the MSI from its configure-service custom action.
var recoveryActions = []mgr.RecoveryAction{
	{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
	{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
	{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
}

// serviceConfig is applied when the service is created and again on every
// install over an existing one, so an upgrade or repair from the EXE
// installer carries changed settings to installed systems. Delayed
// automatic start lets the network come up before system drives connect.
func serviceConfig(executable string) mgr.Config {
	return mgr.Config{
		ServiceType:      windows.SERVICE_WIN32_OWN_PROCESS,
		StartType:        mgr.StartAutomatic,
		ErrorControl:     mgr.ErrorNormal,
		BinaryPathName:   syscall.EscapeArg(executable) + " service",
		ServiceStartName: "LocalSystem",
		DisplayName:      displayName,
		Description:      description,
		DelayedAutoStart: true,
	}
}

// install creates the service, or stops and reconfigures an existing one,
// applies the recovery actions, starts it and waits until it runs. The EXE
// installer calls it after copying a new executable, so the restarted
// service runs the new code.
func install(executable string) error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	settings := serviceConfig(executable)
	service, err := manager.OpenService(Name)
	switch {
	case err == nil:
		defer service.Close()
		if err := stopAndWait(service); err != nil {
			return err
		}
		if err := service.UpdateConfig(settings); err != nil {
			return fmt.Errorf("update service configuration: %w", err)
		}
	case errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST):
		service, err = manager.CreateService(Name, executable, settings, "service")
		if err != nil {
			return fmt.Errorf("create service: %w", err)
		}
		defer service.Close()
	default:
		return fmt.Errorf("open service: %w", err)
	}
	if err := applyServiceSettings(service); err != nil {
		return err
	}
	return startAndWait(service)
}

// configure applies applyServiceSettings to the installed service. The MSI
// runs it as a custom action after InstallServices.
func configure() error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(Name)
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer service.Close()
	return applyServiceSettings(service)
}

// applyServiceSettings sets delayed automatic start and the recovery
// actions on an existing service. An error exit, such as a host that cannot
// start, also counts as a failure that triggers the recovery actions.
func applyServiceSettings(service *mgr.Service) error {
	current, err := service.Config()
	if err != nil {
		return fmt.Errorf("read service configuration: %w", err)
	}
	if !current.DelayedAutoStart {
		current.DelayedAutoStart = true
		if err := service.UpdateConfig(current); err != nil {
			return fmt.Errorf("set delayed automatic start: %w", err)
		}
	}
	if err := service.SetRecoveryActions(recoveryActions, recoveryResetSeconds); err != nil {
		return fmt.Errorf("set service recovery actions: %w", err)
	}
	if err := service.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return fmt.Errorf("set service recovery actions: %w", err)
	}
	return nil
}

func uninstall() error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(Name)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer service.Close()
	if err := stopAndWait(service); err != nil {
		return err
	}
	if err := service.Delete(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

// stopAndWait stops the service, if it runs, and waits until it has
// stopped, so its executable can be replaced or deleted.
func stopAndWait(service *mgr.Service) error {
	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("query service: %w", err)
	}
	if status.State == svc.Stopped {
		return nil
	}
	if status.State != svc.StopPending {
		if _, err := service.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return fmt.Errorf("stop service: %w", err)
		}
	}
	deadline := time.Now().Add(stopTimeout)
	for time.Now().Before(deadline) {
		status, err = service.Query()
		if err != nil {
			return fmt.Errorf("query service: %w", err)
		}
		if status.State == svc.Stopped {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("the %s service did not stop within %s", Name, stopTimeout)
}

// startAndWait starts the service and waits until it reports Running. A
// service that stops instead is reported with its exit code; code 3 means
// it refused an unprotected data directory.
func startAndWait(service *mgr.Service) error {
	if err := service.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("start service: %w", err)
	}
	deadline := time.Now().Add(startTimeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return fmt.Errorf("query service: %w", err)
		}
		switch status.State {
		case svc.Running:
			return nil
		case svc.Stopped:
			if status.ServiceSpecificExitCode == exitInsecureDataPath {
				return errors.New("the service stopped because its data directory is not protected; see the MapFTPDrive entry in the Application event log")
			}
			return fmt.Errorf("the service stopped while starting (exit code %d, service code %d)", status.Win32ExitCode, status.ServiceSpecificExitCode)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("the %s service did not start within %s", Name, startTimeout)
}

func ensureStarted() error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(Name)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		executable, executableErr := os.Executable()
		if executableErr != nil {
			return executableErr
		}
		return install(executable)
	}
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer service.Close()
	status, err := service.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Running || status.State == svc.StartPending {
		return nil
	}
	return startAndWait(service)
}
