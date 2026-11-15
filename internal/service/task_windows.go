//go:build windows

package service

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	// exitTaskFailed reports a one-off task that returned an error.
	exitTaskFailed = 4
	// taskTimeout bounds a one-off task, which stops user hosts and waits
	// for each signed-in user's cleanup.
	taskTimeout = 5 * time.Minute
	// eventTaskFailed is the Application log event for a failed task.
	eventTaskFailed = 3
)

// taskHandler runs one task inside the service process and then stops.
type taskHandler struct {
	task func() error
}

func (h taskHandler) Execute(_ []string, _ <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending, WaitHint: uint32(taskTimeout.Milliseconds())}
	if err := h.task(); err != nil {
		logError(eventTaskFailed, fmt.Sprintf("MapFTPDrive could not finish an installer task: %v", err))
		return true, exitTaskFailed
	}
	return false, 0
}

func runTask(task func() error) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		return errors.New("service mode must be started by the Service Control Manager")
	}
	return svc.Run(Name, taskHandler{task: task})
}

// runAsService runs this executable once as the installed LocalSystem
// service, with arguments after "service", and waits for it to stop. The
// service's command line is restored afterwards. An elevated administrator
// uses it for work only LocalSystem can do, such as reaching every user's
// host pipe and LocalSystem's own credential vault.
func runAsService(arguments []string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(Name)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return ErrServiceMissing
	}
	if err != nil {
		return fmt.Errorf("open service: %w", err)
	}
	defer service.Close()
	if err := stopAndWait(service); err != nil {
		return err
	}
	settings, err := service.Config()
	if err != nil {
		return fmt.Errorf("read service configuration: %w", err)
	}
	original := settings.BinaryPathName
	commandLine := syscall.EscapeArg(executable) + " service"
	for _, argument := range arguments {
		commandLine += " " + syscall.EscapeArg(argument)
	}
	settings.BinaryPathName = commandLine
	if err := service.UpdateConfig(settings); err != nil {
		return fmt.Errorf("prepare service task: %w", err)
	}
	defer func() {
		settings.BinaryPathName = original
		_ = service.UpdateConfig(settings)
	}()
	if err := service.Start(); err != nil {
		return fmt.Errorf("start service task: %w", err)
	}
	deadline := time.Now().Add(taskTimeout + time.Minute)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return fmt.Errorf("query service: %w", err)
		}
		if status.State == svc.Stopped {
			if status.ServiceSpecificExitCode != 0 || status.Win32ExitCode != 0 && status.Win32ExitCode != uint32(windows.ERROR_SERVICE_SPECIFIC_ERROR) {
				return fmt.Errorf("the service task failed (exit code %d, service code %d); see the MapFTPDrive entry in the Application event log",
					status.Win32ExitCode, status.ServiceSpecificExitCode)
			}
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("the service task did not finish within %s", taskTimeout)
}
