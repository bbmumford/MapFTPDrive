//go:build windows

// Package cmount implements a FUSE mounting system for rclone remotes.
//
// This uses the cgo based cgofuse library
package mount

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rclone/rclone/cmd/mountlib"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/atexit"
	"github.com/rclone/rclone/vfs"
	"github.com/winfsp/cgofuse/fuse"
)

// MapFTPDrive local patch: upstream's init registered a "mount" cobra
// command, the "mount/*" remote-control calls and a build tag. MapFTPDrive
// has no rclone command line or remote-control server; the engine calls
// mount directly through mountlib.NewMountPoint, so the registration is
// removed to keep that surface out of the process.

// mountOptions configures the options from the command line flags
func mountOptions(VFS *vfs.VFS, device string, mountpoint string, opt *mountlib.Options) (options []string) {
	// Options
	options = []string{
		"-o", fmt.Sprintf("attr_timeout=%g", time.Duration(opt.AttrTimeout).Seconds()),
	}
	if opt.DebugFUSE {
		options = append(options, "-o", "debug")
	}

	// MapFTPDrive local patch: only the WinFsp options remain because this
	// file builds only for Windows. The label is VolumeName, escaped by
	// volumeLabelOptions, for disk and network volumes alike; the network
	// prefix is the --VolumePrefix entry that handleVolumeName validated in
	// ExtraFlags, appended below (cmount_volume.go).
	options = append(options, "-o", "uid=-1")
	options = append(options, "-o", "gid=-1")
	options = append(options, "--FileSystemName=rclone")
	options = append(options, volumeLabelOptions(opt.VolumeName, opt.ExtraOptions)...)
	for _, option := range opt.ExtraOptions {
		options = append(options, "-o", option)
	}
	options = append(options, opt.ExtraFlags...)
	return options
}

// waitFor runs fn() until it returns true or the timeout expires
func waitFor(fn func() bool) (ok bool) {
	const totalWait = 10 * time.Second
	const individualWait = 10 * time.Millisecond
	for range int(totalWait / individualWait) {
		ok = fn()
		if ok {
			return ok
		}
		time.Sleep(individualWait)
	}
	return false
}

// mount the file system
//
// The mount point will be ready when this returns.
//
// returns an error, and an error channel for the serve process to
// report an error when fusermount is called.
func mount(VFS *vfs.VFS, mountPath string, opt *mountlib.Options) (<-chan error, func() error, string, error) {
	// Get mountpoint using OS specific logic
	f := VFS.Fs()
	mountpoint, err := getMountpoint(f, mountPath, opt)
	if err != nil {
		return nil, nil, "", err
	}
	fs.Debugf(nil, "Mounting on %q (%q)", mountpoint, opt.VolumeName)

	// Create underlying FS
	fsys := NewFS(VFS, opt)
	host := fuse.NewFileSystemHost(fsys)
	host.SetCapReaddirPlus(true) // only works on Windows
	if opt.CaseInsensitive.Valid {
		host.SetCapCaseInsensitive(opt.CaseInsensitive.Value)
	} else {
		host.SetCapCaseInsensitive(f.Features().CaseInsensitive)
	}

	// Create options
	options := mountOptions(VFS, opt.DeviceName, mountpoint, opt)
	fs.Debugf(f, "Mounting with options: %q", options)

	// Serve the mount point in the background returning error to errChan
	errChan := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("mount failed: %v", r)
				if strings.Contains(strings.ToLower(err.Error()), "cannot find winfsp") {
					err = fmt.Errorf("%w\nHint: Install WinFsp from https://winfsp.dev/rel/", err)
				}
				errChan <- err
			}
		}()
		var err error
		ok := host.Mount(mountpoint, options)
		if !ok {
			err = errors.New("mount failed")
			fs.Errorf(f, "Mount failed")
		}
		errChan <- err
	}()

	// unmount
	unmount := func() error {
		var umountOK bool
		if fsys.destroyed.Load() != 0 {
			fs.Debugf(nil, "Not calling host.Unmount as mount already Destroyed")
			umountOK = true
		} else if atexit.Signalled() {
			// If we have received a signal then FUSE will be shutting down already
			fs.Debugf(nil, "Not calling host.Unmount as signal received")
			umountOK = true
		} else {
			fs.Debugf(nil, "Calling host.Unmount")
			umountOK = host.Unmount()
		}
		if umountOK {
			fs.Debugf(nil, "Unmounted successfully")
			// MapFTPDrive local patch: the Windows-only wait runs
			// unconditionally because this file builds only for Windows.
			if !waitFor(func() bool {
				_, err := os.Stat(mountpoint)
				return err != nil
			}) {
				fs.Errorf(nil, "mountpoint %q didn't disappear after unmount - continuing anyway", mountpoint)
			}
			return nil
		}
		fs.Debugf(nil, "host.Unmount failed")
		return errors.New("host unmount failed")
	}

	// Wait for the filesystem to become ready, checking the file
	// system didn't blow up before starting
	select {
	case err := <-errChan:
		err = fmt.Errorf("mount stopped before calling Init: %w", err)
		return nil, nil, "", err
	case <-fsys.ready:
	}

	// Wait for the mount point to be available on Windows
	// On Windows the Init signal comes slightly before the mount is ready
	// MapFTPDrive local patch: unconditional because this file builds only
	// for Windows.
	if !waitFor(func() bool {
		_, err := os.Stat(mountpoint)
		return err == nil
	}) {
		fs.Errorf(nil, "mountpoint %q didn't became available on mount - continuing anyway", mountpoint)
	}

	return errChan, unmount, mountpoint, nil
}
