package mount

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/rclone/rclone/backend/ftp"
	"github.com/rclone/rclone/backend/sftp"
	"github.com/rclone/rclone/cmd/mountlib"
	"github.com/rclone/rclone/fs"
	rcloneconfig "github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/vfs/vfscommon"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/security"
)

// Network behaviour shared by every endpoint in the process. rclone reads
// these from its global configuration on each operation (the VFS runs on
// context.Background), so they cannot be set per mount. The values bound how
// long an Explorer call blocks while the server is unreachable: a connect
// gives up after connectTimeout, a stalled transfer after ioIdleTimeout, and
// each low-level operation is attempted lowLevelRetries times.
const (
	connectTimeout  = 10 * time.Second
	ioIdleTimeout   = 60 * time.Second
	lowLevelRetries = 2
	// dirCacheTime keeps directory listings for a minute: long enough that
	// Explorer's repeated enumerations do not each reach the server, short
	// enough that changes made by others appear promptly.
	dirCacheTime = time.Minute
)

var configureOnce sync.Once

// configureRclone applies the process-wide rclone settings once, before any
// filesystem or transport exists: timeouts, the VFS cache root and the FTPS
// certificate pin check on rclone's shared TLS configuration.
func configureRclone(options Options) {
	configureOnce.Do(func() {
		global := fs.GetConfig(context.Background())
		global.ConnectTimeout = fs.Duration(connectTimeout)
		global.Timeout = fs.Duration(ioIdleTimeout)
		global.LowLevelRetries = lowLevelRetries
		if options.CacheDir != "" {
			_ = rcloneconfig.SetCacheDir(options.CacheDir)
		}
		installTLSPinning()
	})
}

// RouteLogs sends rclone's log output to handler (the scope's log file and,
// for the service, the Event Log) instead of standard error, which a GUI or
// service process does not have.
func RouteLogs(handler slog.Handler) {
	fs.SetLogger(handler)
}

// RemoteName is the rclone remote name for an endpoint. rclone uses it as
// the VFS cache subdirectory, so it is the hashed endpoint key rather than
// anything user-controlled.
func RemoteName(endpoint config.Endpoint) string {
	return config.EndpointKey(endpoint.ID)
}

// backendHasOption reports whether the linked rclone backend defines an
// option, so the engine can use native host-key pinning when it exists.
func backendHasOption(backend, option string) bool {
	info, err := fs.Find(backend)
	if err != nil {
		return false
	}
	for _, candidate := range info.Options {
		if candidate.Name == option {
			return true
		}
	}
	return false
}

// backendOptions renders an endpoint as rclone backend options. SFTP is
// refused without a pinned host key; it is pinned through host_keys when
// the backend supports it (nativeHostKeys) and through a known_hosts file
// otherwise. Remote shell commands are disabled for SFTP so no server-side
// command (hashing, disk usage) is ever run. An FTPS pin turns off rclone's
// chain verification only because the pin check installed on rclone's TLS
// configuration then enforces the exact certificate.
func backendOptions(spec Spec, nativeHostKeys bool) (configmap.Simple, error) {
	endpoint := spec.Endpoint
	options := configmap.Simple{
		"host": endpoint.Host,
		"port": strconv.Itoa(int(endpoint.Port)),
		"user": endpoint.Username,
	}
	obscured := func(secret string) (string, error) {
		value, err := obscure.Obscure(secret)
		if err != nil {
			return "", fmt.Errorf("protect backend secret: %w", err)
		}
		return value, nil
	}
	switch endpoint.AuthMethodOrDefault() {
	case config.AuthKey:
		options["key_file"] = endpoint.KeyFile
		if spec.Secret.Password != "" {
			value, err := obscured(spec.Secret.Password)
			if err != nil {
				return nil, err
			}
			options["key_file_pass"] = value
		}
	default:
		if spec.Secret.Password != "" {
			value, err := obscured(spec.Secret.Password)
			if err != nil {
				return nil, err
			}
			options["pass"] = value
		}
	}
	switch endpoint.Protocol {
	case config.FTP:
	case config.FTPS:
		if endpoint.TLSMode == config.TLSImplicit {
			options["tls"] = "true"
		} else {
			options["explicit_tls"] = "true"
		}
		if endpoint.CertificatePin != "" {
			options["no_check_certificate"] = "true"
		}
	case config.SFTP:
		if endpoint.KnownHost == "" {
			return nil, security.ErrHostKeyNotPinned
		}
		if nativeHostKeys {
			key, err := security.ParseKnownHost(endpoint.KnownHost)
			if err != nil {
				return nil, err
			}
			options["host_keys"] = key.HostKeysOption()
		} else {
			if spec.KnownHostsFile == "" {
				return nil, errors.New("pinned host key file is missing")
			}
			options["known_hosts_file"] = spec.KnownHostsFile
		}
		options["shell_type"] = "none"
		options["disable_hashcheck"] = "true"
	default:
		return nil, fmt.Errorf("unsupported protocol %q", endpoint.Protocol)
	}
	return options, nil
}

// newFs connects to the endpoint. The returned release drops the FTPS pin
// registration once the filesystem is no longer used.
func newFs(ctx context.Context, spec Spec) (fs.Fs, func(), error) {
	endpoint := spec.Endpoint
	if err := endpoint.Validate(); err != nil {
		return nil, nil, err
	}
	options, err := backendOptions(spec, backendHasOption("sftp", "host_keys"))
	if err != nil {
		return nil, nil, err
	}
	release := func() {}
	if endpoint.Protocol == config.FTPS && endpoint.CertificatePin != "" {
		release = tlsPins.add(endpoint.Host, endpoint.CertificatePin)
	}
	name := RemoteName(endpoint)
	var remote fs.Fs
	switch endpoint.Protocol {
	case config.FTP, config.FTPS:
		remote, err = ftp.NewFs(ctx, name, endpoint.RemotePath, options)
	case config.SFTP:
		remote, err = sftp.NewFs(ctx, name, endpoint.RemotePath, options)
	}
	if errors.Is(err, fs.ErrorIsFile) {
		err = fmt.Errorf("remote path %q is a file, not a folder", endpoint.RemotePath)
	}
	if err != nil {
		release()
		return nil, nil, fmt.Errorf("connect to %s: %w", endpoint.Host, err)
	}
	return remote, release, nil
}

// probe connects and lists the remote root, which proves the address,
// credentials, server identity and remote path without mounting.
func probe(ctx context.Context, spec Spec) error {
	remote, release, err := newFs(ctx, spec)
	if err != nil {
		return err
	}
	defer release()
	defer shutdownFs(remote)
	if _, err := remote.List(ctx, ""); err != nil {
		return fmt.Errorf("list %s: %w", spec.Endpoint.Host, err)
	}
	return nil
}

func shutdownFs(remote fs.Fs) {
	if shutdowner, ok := remote.(fs.Shutdowner); ok {
		_ = shutdowner.Shutdown(context.Background())
	}
}

// volumePrefixFlag names system (network-mode) volumes \mapftpdrive\<id>:
// unique per endpoint, stable across renames and free of user text. The
// cmount adapter requires exactly this flag whenever NetworkMode is set.
func volumePrefixFlag(endpoint config.Endpoint) string {
	return `--VolumePrefix=\mapftpdrive\` + endpoint.ID
}

// mountSettings derives the mountlib and VFS options for an endpoint.
// VolumeName carries the raw drive name; the adapter escapes it for the
// FUSE option parser and passes it as the volume label. The VFS caches
// written files on disk so Office-style read/write opens, random writes and
// upload retries work, and caches listings for dirCacheTime.
func mountSettings(endpoint config.Endpoint) (mountlib.Options, vfscommon.Options) {
	mountOptions := mountlib.Opt
	mountOptions.VolumeName = endpoint.Name
	mountOptions.NetworkMode = endpoint.Scope == config.System
	flags := append([]string(nil), mountOptions.ExtraFlags...)
	if mountOptions.NetworkMode {
		flags = append(flags, volumePrefixFlag(endpoint))
	}
	mountOptions.ExtraFlags = flags
	mountOptions.ExtraOptions = append([]string(nil), mountOptions.ExtraOptions...)

	vfsOptions := vfscommon.Opt
	vfsOptions.CacheMode = vfscommon.CacheModeWrites
	vfsOptions.DirCacheTime = fs.Duration(dirCacheTime)
	return mountOptions, vfsOptions
}
