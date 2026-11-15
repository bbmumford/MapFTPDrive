//go:build windows

package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// userPipePrefix starts the name of every user host's pipe.
const userPipePrefix = "MapFTPDrive-user-"

// userPipes lists the user host pipes that currently exist.
func userPipes() ([]string, error) {
	pattern, err := windows.UTF16PtrFromString(`\\.\pipe\*`)
	if err != nil {
		return nil, err
	}
	var data windows.Win32finddata
	handle, err := windows.FindFirstFile(pattern, &data)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer windows.FindClose(handle)
	var names []string
	for {
		if name := windows.UTF16ToString(data.FileName[:]); strings.HasPrefix(name, userPipePrefix) {
			names = append(names, `\\.\pipe\`+name)
		}
		if err := windows.FindNextFile(handle, &data); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				return names, nil
			}
			return names, err
		}
	}
}

func stopUserHosts(ctx context.Context, timeout time.Duration) ([]uint32, error) {
	names, err := userPipes()
	if err != nil {
		return nil, err
	}
	type host struct {
		pid     uint32
		process windows.Handle
	}
	var hosts []host
	var failures []error
	for _, name := range names {
		pid, process, err := requestShutdown(ctx, name)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", name, err))
		}
		if pid != 0 {
			hosts = append(hosts, host{pid: pid, process: process})
		}
	}
	deadline := time.Now().Add(timeout)
	var running []uint32
	for _, item := range hosts {
		if item.process == 0 {
			running = append(running, item.pid)
			continue
		}
		wait := time.Until(deadline)
		if wait < 0 {
			wait = 0
		}
		event, err := windows.WaitForSingleObject(item.process, uint32(wait.Milliseconds()))
		windows.CloseHandle(item.process)
		if err != nil || event != windows.WAIT_OBJECT_0 {
			running = append(running, item.pid)
		}
	}
	return running, errors.Join(failures...)
}

// requestShutdown asks the host serving one pipe to shut down. It returns
// the serving process, opened for waiting, even when the host rejects the
// request, so the caller can still wait for or terminate it.
func requestShutdown(ctx context.Context, name string) (uint32, windows.Handle, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	connection, err := winio.DialPipeContext(dialCtx, name)
	if err != nil {
		return 0, 0, err
	}
	defer connection.Close()
	var pid uint32
	var process windows.Handle
	if pipe, ok := connection.(interface{ Fd() uintptr }); ok {
		if windows.GetNamedPipeServerProcessId(windows.Handle(pipe.Fd()), &pid) == nil {
			process, _ = windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
		}
	}
	_ = connection.SetDeadline(time.Now().Add(messageWriteTimeout))
	if err := json.NewEncoder(connection).Encode(Request{Operation: Shutdown}); err != nil {
		return pid, process, err
	}
	decoder := json.NewDecoder(io.LimitReader(connection, 64*1024))
	for {
		var response Response
		if err := decoder.Decode(&response); err != nil {
			return pid, process, err
		}
		if !response.Final() {
			continue
		}
		if response.Error != "" {
			return pid, process, fmt.Errorf("the host refused to shut down: %s", response.Error)
		}
		return pid, process, nil
	}
}
