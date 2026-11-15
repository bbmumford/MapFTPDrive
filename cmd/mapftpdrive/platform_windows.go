//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"mapftpdrive/internal/app"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
	"mapftpdrive/internal/explorer"
	"mapftpdrive/internal/host"
	"mapftpdrive/internal/security"
	"mapftpdrive/internal/service"
	"mapftpdrive/internal/ui"
)

func installDependencies(winfspMSI string) error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\WinFsp`, registry.QUERY_VALUE|registry.WOW64_32KEY)
	if err == nil {
		_ = key.Close()
		return nil
	}
	if !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("check WinFsp installation: %w", err)
	}

	// This command is launched asynchronously by the parent MSI. Windows
	// Installer may still own its global transaction mutex for a few seconds,
	// so retry ERROR_INSTALL_ALREADY_RUNNING rather than racing the parent.
	const alreadyRunning = 1618
	var lastErr error
	for attempt := 0; attempt < 60; attempt++ {
		command := exec.Command(
			filepath.Join(os.Getenv("SystemRoot"), "System32", "msiexec.exe"),
			"/i", winfspMSI, "/qn", "/norestart",
		)
		if err := command.Run(); err == nil {
			return nil
		} else {
			lastErr = err
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != alreadyRunning {
				return fmt.Errorf("install bundled WinFsp runtime: %w", err)
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("install bundled WinFsp runtime after waiting for Windows Installer: %w", lastErr)
}

func launchUI(scope config.Scope) error {
	if scope == config.System && !windows.GetCurrentProcessToken().IsElevated() {
		return relaunchElevated("map --scope system")
	}
	result, err := ui.ShowMapper(scope)
	if errors.Is(err, ui.ErrCancelled) {
		return nil
	}
	if err != nil {
		return err
	}
	id, err := app.NewID()
	if err != nil {
		return err
	}
	result.Endpoint.ID = id
	result.Endpoint.CredentialKey = "endpoint/" + id
	if result.Endpoint.Protocol == config.SFTP {
		key, scanErr := security.ScanSFTPHostKey(result.Endpoint)
		if scanErr != nil {
			ui.ShowError("Unable to verify SFTP server", scanErr)
			return scanErr
		}
		if !ui.ConfirmHostKey(result.Endpoint.Host, key.Algorithm, key.Fingerprint) {
			return nil
		}
		result.Endpoint.KnownHost = key.Line
	}
	if err := ensureHost(scope); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	err = control.Send(ctx, scope, control.Request{
		Operation: control.SaveAndMount,
		Endpoint:  &result.Endpoint,
		Password:  result.Password,
	})
	if err != nil {
		ui.ShowError("Unable to map drive", err)
	}
	return err
}

func installExplorerIntegration() error {
	if err := explorer.Install(); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return service.Install(executable)
}

func uninstallExplorerIntegration() error {
	explorerErr := explorer.Uninstall()
	serviceErr := service.Uninstall()
	return errors.Join(explorerErr, serviceErr)
}

func runService() error {
	return service.Run()
}

func runDaemon(scope config.Scope) error {
	if scope != config.User {
		return errors.New("only the Service Control Manager may start the system host")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return host.Run(ctx, scope)
}

func runDriveAction(action, drive, name string) error {
	scope, endpoint, err := findManagedDrive(drive)
	if err != nil {
		return err
	}
	if scope == config.System && !windows.GetCurrentProcessToken().IsElevated() {
		arguments := fmt.Sprintf(`%s --drive "%s"`, action, drive)
		if name != "" {
			arguments += fmt.Sprintf(` --name "%s"`, strings.ReplaceAll(name, `"`, ``))
		}
		return relaunchElevated(arguments)
	}
	if action == "remove" && !ui.ConfirmRemove(endpoint.Name, endpoint.DriveLetter) {
		return nil
	}
	if action == "rename" && strings.TrimSpace(name) == "" {
		name, err = ui.PromptRename(endpoint.Name)
		if errors.Is(err, ui.ErrCancelled) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	if err := ensureHost(scope); err != nil {
		return err
	}
	operation := control.Operation(action)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	err = control.Send(ctx, scope, control.Request{Operation: operation, Drive: drive, Name: name})
	if err != nil {
		ui.ShowError("MapFTPDrive", err)
	}
	return err
}

func findManagedDrive(drive string) (config.Scope, config.Endpoint, error) {
	for _, scope := range []config.Scope{config.User, config.System} {
		path, err := config.DefaultPath(scope)
		if err != nil {
			continue
		}
		endpoint, err := config.NewStore(path).FindByDrive(drive)
		if err == nil {
			return scope, endpoint, nil
		}
	}
	return "", config.Endpoint{}, fmt.Errorf("drive %q is not managed by MapFTPDrive", drive)
}

// ensureHost returns once the scope's host answers on its pipe, starting
// the host when nothing answers. A host takes its pipe before it mounts
// anything, so an answer means it is ready for requests, however long its
// mounts take. A host started while another already owns the pipe exits
// cleanly, so starting one more than needed is harmless; a started user
// host that exits with an error ends the wait with that error instead of
// waiting out the deadline.
func ensureHost(scope config.Scope) error {
	ping := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return control.Send(ctx, scope, control.Request{Operation: control.Ping})
	}
	if ping() == nil {
		return nil
	}
	var exited chan error
	if scope == config.System {
		if err := service.EnsureStarted(); err != nil {
			return err
		}
	} else {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		command := exec.Command(executable, "daemon", "--scope", "user")
		command.SysProcAttr = &syscall.SysProcAttr{
			HideWindow:    true,
			CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
		}
		if err := command.Start(); err != nil {
			return err
		}
		exited = make(chan error, 1)
		go func() { exited <- command.Wait() }()
	}
	const readyWithin = 60 * time.Second
	deadline := time.Now().Add(readyWithin)
	delay := 50 * time.Millisecond
	for {
		err := ping()
		if err == nil {
			return nil
		}
		select {
		case exitErr := <-exited:
			exited = nil
			if exitErr != nil {
				return fmt.Errorf("the MapFTPDrive host exited (%w); see mapftpdrive.log in %%LOCALAPPDATA%%\\MapFTPDrive\\logs", exitErr)
			}
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the MapFTPDrive host did not answer within %s: %w", readyWithin, err)
		}
		time.Sleep(delay)
		if delay < time.Second {
			delay *= 2
		}
	}
}

func relaunchElevated(arguments string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(executable)
	args, _ := windows.UTF16PtrFromString(arguments)
	return windows.ShellExecute(0, verb, file, args, nil, windows.SW_SHOWNORMAL)
}
