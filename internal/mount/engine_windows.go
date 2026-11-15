//go:build windows

package mount

import (
	"context"
	"fmt"
	"strconv"

	"github.com/rclone/rclone/backend/ftp"
	"github.com/rclone/rclone/backend/sftp"
	"github.com/rclone/rclone/cmd/mountlib"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/vfs/vfscommon"
	"mapftpdrive/internal/config"
)

type windowsEngine struct{}

func newPlatformEngine() Engine {
	return windowsEngine{}
}

type windowsHandle struct {
	point *mountlib.MountPoint
}

func (h windowsHandle) Wait() error    { return h.point.Wait() }
func (h windowsHandle) Unmount() error { return h.point.Unmount() }

func (windowsEngine) Mount(ctx context.Context, endpoint config.Endpoint, secret Secret) (Handle, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	password, err := obscure.Obscure(secret.Password)
	if err != nil {
		return nil, fmt.Errorf("protect backend password: %w", err)
	}
	options := configmap.Simple{
		"host": endpoint.Host,
		"port": strconv.Itoa(int(endpoint.Port)),
		"user": endpoint.Username,
		"pass": password,
	}

	var remote fs.Fs
	switch endpoint.Protocol {
	case config.FTP:
		remote, err = ftp.NewFs(ctx, endpoint.ID, endpoint.RemotePath, options)
	case config.FTPS:
		if endpoint.TLSMode == config.TLSImplicit {
			options["tls"] = "true"
		} else {
			options["explicit_tls"] = "true"
		}
		remote, err = ftp.NewFs(ctx, endpoint.ID, endpoint.RemotePath, options)
	case config.SFTP:
		if endpoint.KnownHostsFile != "" {
			options["known_hosts_file"] = endpoint.KnownHostsFile
		}
		remote, err = sftp.NewFs(ctx, endpoint.ID, endpoint.RemotePath, options)
	default:
		return nil, fmt.Errorf("unsupported protocol %q", endpoint.Protocol)
	}
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", endpoint.Host, err)
	}

	mountOptions := mountlib.Opt
	mountOptions.VolumeName = endpoint.Name
	// User mounts behave as fixed drives so Explorer's standard Rename action
	// maps to SetVolumeLabel. SYSTEM mounts need network mode to cross logon
	// session drive namespaces.
	mountOptions.NetworkMode = endpoint.Scope == config.System
	// A fresh slice keeps the shared mountlib.Opt defaults unmodified.
	mountOptions.ExtraFlags = append(append([]string(nil), mountOptions.ExtraFlags...), FileSystemNameFlag())
	vfsOptions := vfscommon.Opt
	point := mountlib.NewMountPoint(
		mount,
		endpoint.DriveLetter+":",
		remote,
		&mountOptions,
		&vfsOptions,
	)
	if _, err := point.Mount(); err != nil {
		return nil, err
	}
	return windowsHandle{point: point}, nil
}
