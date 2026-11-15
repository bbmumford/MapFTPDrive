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
		return relaunchElevated("map", "--scope", "system")
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
			return fmt.Errorf("unable to verify the SFTP server: %w", scanErr)
		}
		if !ui.ConfirmHostKey(result.Endpoint.Host, key.Algorithm, key.Fingerprint) {
			return nil
		}
		result.Endpoint.KnownHost = key.Line
	}
	if err := ensureHost(scope); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), control.RequestTimeout)
	defer cancel()
	err = control.Send(ctx, scope, control.Request{
		Operation: control.SaveAndMount,
		Endpoint:  &result.Endpoint,
		Password:  result.Password,
	})
	if err != nil {
		return fmt.Errorf("unable to map the drive: %w", err)
	}
	return nil
}

// installMachine is the EXE installer's post-copy step, run elevated. The
// installer itself writes the machine-wide Explorer verbs and Run entry;
// this secures the system data directory, removes the per-user
// registration of the installing account (see RemoveUserRegistration), and
// installs or updates the service.
func installMachine() error {
	if err := prepareSystemData(); err != nil {
		return err
	}
	if err := explorer.RemoveUserRegistration(); err != nil {
		return fmt.Errorf("remove per-user Explorer registration: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return service.Install(executable)
}

// prepareSystemData creates or repairs the protected system data directory
// and registers the service's event log source. Both installers run it
// elevated before the service starts, which otherwise refuses to run.
func prepareSystemData() error {
	if err := config.SecureSystemDirectory(); err != nil {
		return fmt.Errorf("secure the system data directory: %w", err)
	}
	return service.RegisterEventSource()
}

// uninstallMachine is the EXE uninstaller's pre-delete step: it removes the
// service, its event log source and any per-user registration of the
// uninstalling account. The uninstaller deletes the machine-wide verbs.
func uninstallMachine() error {
	return errors.Join(
		explorer.RemoveUserRegistration(),
		service.Uninstall(),
		service.RemoveEventSource(),
	)
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

// runDriveAction performs action on the managed drive with the given
// upper-case letter. System drives are handled by an elevated copy of this
// process, which receives the same arguments.
func runDriveAction(action, drive, name string) error {
	scope, endpoint, err := findManagedDrive(drive)
	if err != nil {
		return err
	}
	if scope == config.System && !windows.GetCurrentProcessToken().IsElevated() {
		arguments := []string{action, "--drive", drive}
		if name != "" {
			arguments = append(arguments, "--name", name)
		}
		return relaunchElevated(arguments...)
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
	ctx, cancel := context.WithTimeout(context.Background(), control.RequestTimeout)
	defer cancel()
	return control.Send(ctx, scope, control.Request{Operation: operation, Drive: drive, Name: name})
}

// mountUnmountedDrive lets the user choose one of the managed drives that
// Unmount disabled and mounts it again. It backs the This PC "Mount FTP
// Drive" verb: an unmounted drive has no drive-letter item to right-click.
func mountUnmountedDrive() error {
	var choices []config.Endpoint
	for _, endpoint := range managedDrives() {
		if endpoint.Disabled {
			choices = append(choices, endpoint)
		}
	}
	if len(choices) == 0 {
		ui.ShowInfo("Mount FTP Drive", "Every MapFTPDrive drive is already mounted.\n\nUse Map FTP Drive to add a new one.")
		return nil
	}
	labels := make([]string, len(choices))
	for index, endpoint := range choices {
		labels[index] = fmt.Sprintf("%s:  %s", endpoint.DriveLetter, endpoint.Name)
		if endpoint.Scope == config.System {
			labels[index] += "  (system-wide)"
		}
	}
	index, err := ui.PickOne("Mount FTP Drive", "Choose the drive to mount:", "Mount", labels)
	if errors.Is(err, ui.ErrCancelled) {
		return nil
	}
	if err != nil {
		return err
	}
	return runDriveAction("mount", choices[index].DriveLetter, "")
}

// managedDrives returns the endpoints of both scopes. A scope that cannot
// be read is skipped so the other stays usable.
func managedDrives() []config.Endpoint {
	var endpoints []config.Endpoint
	if path, err := config.DefaultPath(config.User); err == nil {
		if loaded, err := config.NewStore(path).Load(); err == nil {
			endpoints = append(endpoints, loaded...)
		}
	}
	return append(endpoints, systemDrives()...)
}

// systemDrives lists the system-scope drives. Only SYSTEM and
// Administrators may read the system store, so an elevated process reads
// it directly and any other process reads the drive index the service
// keeps beside it, which holds the letter, ID, name and state.
func systemDrives() []config.Endpoint {
	if windows.GetCurrentProcessToken().IsElevated() {
		if path, err := config.DefaultPath(config.System); err == nil {
			if loaded, err := config.NewStore(path).Load(); err == nil {
				return loaded
			}
		}
	}
	indexed, err := config.ReadDriveIndex()
	if err != nil {
		return nil
	}
	endpoints := make([]config.Endpoint, 0, len(indexed))
	for _, drive := range indexed {
		if drive.Scope == config.System {
			endpoints = append(endpoints, drive.Endpoint())
		}
	}
	return endpoints
}

// findManagedDrive resolves an upper-case drive letter to its managed
// endpoint, preferring the user scope when both scopes claim the letter.
func findManagedDrive(drive string) (config.Scope, config.Endpoint, error) {
	for _, endpoint := range managedDrives() {
		if endpoint.DriveLetter == drive {
			return endpoint.Scope, endpoint, nil
		}
	}
	return "", config.Endpoint{}, fmt.Errorf("drive %s: is not managed by MapFTPDrive", drive)
}

func ensureHost(scope config.Scope) error {
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	err := control.Send(ctx, scope, control.Request{Operation: control.Ping})
	cancel()
	if err == nil {
		return nil
	}
	// Another process holds the pipe name; a new host could not take it.
	if errors.Is(err, control.ErrUntrustedHost) {
		return err
	}
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
		_ = command.Process.Release()
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := control.Send(ctx, scope, control.Request{Operation: control.Ping})
		cancel()
		if err == nil || errors.Is(err, control.ErrUntrustedHost) {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("mount host did not become ready")
}

// relaunchElevated starts this executable with arguments through the UAC
// consent prompt. Each argument is escaped with the rules the child's argv
// parser reverses, so names with quotes, spaces or trailing backslashes
// arrive unchanged. Declining the prompt is not an error.
func relaunchElevated(arguments ...string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return err
	}
	args, err := windows.UTF16PtrFromString(elevatedCommandLine(arguments))
	if err != nil {
		return err
	}
	err = windows.ShellExecute(0, verb, file, args, nil, windows.SW_SHOWNORMAL)
	if errors.Is(err, windows.ERROR_CANCELLED) {
		return nil
	}
	return err
}

// elevatedCommandLine joins arguments into the parameter string that
// ShellExecute passes to the elevated process.
func elevatedCommandLine(arguments []string) string {
	escaped := make([]string, len(arguments))
	for index, argument := range arguments {
		escaped[index] = syscall.EscapeArg(argument)
	}
	return strings.Join(escaped, " ")
}

// reportError writes err to stderr and, for commands started from Explorer
// or the Start menu, also shows it in a message box because a GUI-subsystem
// process has no console there.
func reportError(err error, interactive bool) {
	fmt.Fprintln(os.Stderr, "MapFTPDrive:", err)
	if interactive {
		ui.ShowError("MapFTPDrive", err)
	}
}
