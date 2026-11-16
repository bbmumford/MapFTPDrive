//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"mapftpdrive/internal/cleanup"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
	"mapftpdrive/internal/explorer"
	"mapftpdrive/internal/host"
	"mapftpdrive/internal/service"
	"mapftpdrive/internal/ui"
)

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

// uninstallMachine is the EXE uninstaller's pre-delete step, run elevated:
// it stops the user hosts and, unless keepData is set, removes stored
// passwords and data (uninstallCleanup), then removes the service, its
// event log source and any per-user registration of the uninstalling
// account. The uninstaller deletes the machine-wide verbs and files.
func uninstallMachine(keepData bool) error {
	return errors.Join(
		uninstallCleanup(keepData),
		explorer.RemoveUserRegistration(),
		service.Uninstall(),
		service.RemoveEventSource(),
	)
}

// uninstallCleanup runs cleanup.Run as LocalSystem: directly when this
// process is LocalSystem (the MSI's custom action), otherwise through the
// installed service (the EXE uninstaller runs as an administrator, who
// cannot open other users' host pipes or LocalSystem's credential vault).
// Without the service, an administrator can still remove the system data
// directory; the result reports what was left.
func uninstallCleanup(keepData bool) error {
	if cleanup.IsLocalSystem() {
		return runUninstallCleanup(keepData)
	}
	arguments := []string{"--cleanup"}
	if keepData {
		arguments = append(arguments, "--keep-data")
	}
	err := service.RunAsService(arguments...)
	if !errors.Is(err, service.ErrServiceMissing) {
		return err
	}
	if keepData {
		return errors.New("the MapFTPDrive service is missing, so running user hosts were not stopped")
	}
	dir, dirErr := config.SystemDirectory()
	if dirErr == nil {
		dirErr = os.RemoveAll(dir)
	}
	return errors.Join(
		errors.New("the MapFTPDrive service is missing, so user hosts, user data and LocalSystem's stored passwords were not removed"),
		dirErr,
	)
}

// runUninstallCleanup is uninstallCleanup's LocalSystem part.
func runUninstallCleanup(keepData bool) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return cleanup.Run(cleanup.Options{KeepData: keepData, Executable: executable})
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
	indexed, err := config.ReadSystemDriveIndex()
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

// ensureHost returns once the scope's host answers on its pipe, starting
// the host when nothing answers. A host takes its pipe before it mounts
// anything, so an answer means it is ready for requests, however long its
// mounts take. A host started while another already owns the pipe exits
// cleanly, so starting one more than needed is harmless; a started user
// host that exits with an error ends the wait with that error instead of
// waiting out the deadline. A pipe served by a process that fails the
// identity check (control.ErrUntrustedHost) ends the wait at once: that
// process holds the pipe name, so a new host could not take it.
func ensureHost(scope config.Scope) error {
	ping := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return control.Send(ctx, scope, control.Request{Operation: control.Ping})
	}
	if err := ping(); err == nil || errors.Is(err, control.ErrUntrustedHost) {
		return err
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
		// executable is this program's own path from os.Executable and the
		// arguments are constants, so nothing caller-supplied reaches it.
		command := exec.Command(executable, "daemon", "--scope", "user") //nolint:gosec // G204: own executable, constant arguments.
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
		if err == nil || errors.Is(err, control.ErrUntrustedHost) {
			return err
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
// process has no console there. An error the command already showed itself
// (ui.WasShown) is not shown a second time.
func reportError(err error, interactive bool) {
	fmt.Fprintln(os.Stderr, "MapFTPDrive:", err)
	if interactive && !ui.WasShown(err) {
		ui.ShowError("MapFTPDrive", err)
	}
}
