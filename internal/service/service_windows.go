//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

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

func install(executable string) error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	existing, err := manager.OpenService(Name)
	if err == nil {
		existing.Close()
		return nil
	}
	service, err := manager.CreateService(Name, executable, mgr.Config{
		DisplayName:      "MapFTPDrive system mount service",
		Description:      "Restores and supervises system-wide MapFTPDrive mounts.",
		StartType:        mgr.StartAutomatic,
		ServiceStartName: "LocalSystem",
	}, "service")
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer service.Close()
	_ = service.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 86400)
	return service.Start()
}

func uninstall() error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(Name)
	if err != nil {
		return nil
	}
	defer service.Close()
	_, _ = service.Control(svc.Stop)
	return service.Delete()
}

func ensureStarted() error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(Name)
	if err != nil {
		executable, executableErr := os.Executable()
		if executableErr != nil {
			return executableErr
		}
		return install(executable)
	}
	defer service.Close()
	status, err := service.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Running || status.State == svc.StartPending {
		return nil
	}
	return service.Start()
}
