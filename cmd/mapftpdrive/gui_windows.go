//go:build windows

package main

import (
	"context"
	"errors"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
	"mapftpdrive/internal/ui"
)

// scopeHost is a ui.Host for one scope. It makes sure the host runs before
// the first request and again after a request fails, so a host that was
// restarted meanwhile is picked up.
type scopeHost struct {
	scope config.Scope
	// autostart is false for the user scope in a UAC-elevated process: a
	// user host started from there would mount into the elevated logon
	// session, where Explorer cannot see the drives.
	autostart bool
	mu        sync.Mutex
	ready     bool
}

func (h *scopeHost) Call(ctx context.Context, request control.Request) (control.Response, error) {
	h.mu.Lock()
	ready := h.ready
	h.mu.Unlock()
	if !ready && h.autostart {
		if err := ensureHost(h.scope); err != nil {
			return control.Response{}, err
		}
	}
	response, err := control.Call(ctx, h.scope, request)
	h.mu.Lock()
	h.ready = err == nil || response.Error != ""
	h.mu.Unlock()
	return response, err
}

// uacElevated reports a process elevated through UAC (a full token linked
// to a filtered one), as opposed to an account that is always elevated.
func uacElevated() bool {
	token := windows.GetCurrentProcessToken()
	if !token.IsElevated() {
		return false
	}
	var elevationType uint32
	var returned uint32
	err := windows.GetTokenInformation(token, windows.TokenElevationType, (*byte)(unsafe.Pointer(&elevationType)), 4, &returned)
	return err == nil && elevationType == tokenElevationTypeFull
}

const tokenElevationTypeFull = 2

// letterUsage gathers what the mapper must avoid: letters this process
// sees in use, the user's own drives and the system drive index.
func letterUsage() ui.LetterUsage {
	usage := ui.LetterUsage{}
	if mask, err := windows.GetLogicalDrives(); err == nil {
		usage.Mask = mask
	}
	if path, err := config.DefaultPath(config.User); err == nil {
		if endpoints, err := config.NewStore(path).Load(); err == nil {
			usage.Managed = append(usage.Managed, config.DriveIndex(config.User, endpoints)...)
		}
	}
	if entries, err := config.ReadSystemDriveIndex(); err == nil {
		usage.Managed = append(usage.Managed, entries...)
	}
	return usage
}

// runMapper shows the map dialog for scope. The dialog stays open until
// the host has tested, mounted and saved the drive, or the user cancels.
func runMapper(scope config.Scope) error {
	if scope == config.System && !windows.GetCurrentProcessToken().IsElevated() {
		return relaunchElevated("map", "--scope", "system")
	}
	host := &scopeHost{scope: scope, autostart: scope == config.System || !uacElevated()}
	_, err := ui.ShowMapperWith(ui.MapperOptions{
		Scope: scope,
		Usage: letterUsage(),
		Submit: func(result ui.MapperResult, prompter ui.Prompter) error {
			ctx, cancel := context.WithTimeout(context.Background(), ui.SubmitTimeout)
			defer cancel()
			_, err := ui.SubmitMap(ctx, host, ui.NetworkScanner, prompter, result, nil)
			return err
		},
	})
	if errors.Is(err, ui.ErrCancelled) {
		return nil
	}
	return err
}

// runManager shows the management window: the user's drives, and the
// system-wide drives when elevated (otherwise a button relaunches it
// elevated).
func runManager() error {
	elevated := windows.GetCurrentProcessToken().IsElevated()
	hosts := map[config.Scope]ui.Host{
		config.User: &scopeHost{scope: config.User, autostart: !uacElevated()},
	}
	options := ui.ManagerOptions{Hosts: hosts, Scanner: ui.NetworkScanner, Usage: letterUsage}
	if elevated {
		hosts[config.System] = &scopeHost{scope: config.System, autostart: true}
	} else {
		options.OpenSystem = func() error { return relaunchElevated("manage") }
	}
	return ui.ShowManager(options)
}
