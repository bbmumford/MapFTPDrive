//go:build windows

package mount

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/rclone/rclone/cmd/mountlib"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/lib/file"
)

var isDriveRegex = regexp.MustCompile(`^[a-zA-Z]\:$`)
var isDriveRootPathRegex = regexp.MustCompile(`^[a-zA-Z]\:\\$`)
var isNetworkSharePathRegex = regexp.MustCompile(`^\\\\[^\\\?]+\\[^\\]`)

// isNetworkSharePath returns true if the given string is a valid network share path,
// in the basic UNC format "\\Server\Share\Path", where the first two path components
// are required ("\\Server\Share", which represents the volume).
// Extended-length UNC format "\\?\UNC\Server\Share\Path" is not considered, as it is
// not supported by cgofuse/winfsp, so returns false for any paths with prefix "\\?\".
// Note: There is a UNCPath function in lib/file, but it refers to any extended-length
// paths using prefix "\\?\", and not necessarily network resource UNC paths.
func isNetworkSharePath(l string) bool {
	return isNetworkSharePathRegex.MatchString(l)
}

// isDrive returns true if given string is a drive letter followed by the volume separator, e.g. "X:".
// This is the format supported by cgofuse/winfsp for mounting as drive.
// Extended-length format "\\?\X:" is not considered, as it is not supported by cgofuse/winfsp.
func isDrive(l string) bool {
	return isDriveRegex.MatchString(l)
}

// isDriveRootPath returns true if given string is a drive letter followed by the volume separator,
// as well as a path separator, e.g. "X:\". This is a format often used instead of the format without the
// trailing path separator to denote a drive or volume, in addition to representing the drive's root directory.
// This format is not accepted by cgofuse/winfsp for mounting as drive, but can easily be by trimming off
// the path separator. Extended-length format "\\?\X:\" is not considered.
func isDriveRootPath(l string) bool {
	return isDriveRootPathRegex.MatchString(l)
}

// MapFTPDrive local patch: isDriveOrRootPath and its regular expression are
// removed because nothing in the Windows build calls them.

// isDefaultPath returns true if given string is a special keyword used to trigger default mount.
func isDefaultPath(l string) bool {
	return l == "" || l == "*"
}

// getUnusedDrive find unused drive letter and returns string with drive letter followed by volume separator.
func getUnusedDrive() (string, error) {
	driveLetter := file.FindUnusedDriveLetter()
	if driveLetter == 0 {
		return "", errors.New("could not find unused drive letter")
	}
	mountpoint := string(driveLetter) + ":" // Drive letter with volume separator only, no trailing backslash, which is what cgofuse/winfsp expects
	fs.Logf(nil, "Assigning drive letter %q", mountpoint)
	return mountpoint, nil
}

// handleDefaultMountpath handles the case where mount path is not set, or set to a special keyword.
// This will automatically pick an unused drive letter to use as mountpoint.
func handleDefaultMountpath() (string, error) {
	return getUnusedDrive()
}

// handleNetworkShareMountpath handles the case where mount path is a network share path.
// Sets volume name option and returns a mountpoint string.
func handleNetworkShareMountpath(mountpath string, opt *mountlib.Options) (string, error) {
	// Assuming mount path is a valid network share path (UNC format, "\\Server\Share").
	// Always mount as network drive, regardless of the NetworkMode option.
	// Find an unused drive letter to use as mountpoint, the supplied path can
	// be used as volume prefix (network share path) instead of mountpoint.
	if !opt.NetworkMode {
		fs.Debugf(nil, "Forcing --network-mode because mountpoint path is network share UNC format")
		opt.NetworkMode = true
	}
	mountpoint, err := getUnusedDrive()
	if err != nil {
		return "", err
	}
	return mountpoint, nil
}

// handleLocalMountpath handles the case where mount path is a local file system path.
func handleLocalMountpath(f fs.Fs, mountpath string, opt *mountlib.Options) (string, error) {
	// Assuming path is drive letter or directory path, not network share (UNC) path.
	// If drive letter: Must be given as a single character followed by ":" and nothing else.
	// Else, assume directory path: Directory must not exist, but its parent must.
	if _, err := os.Stat(mountpath); err == nil {
		return "", errors.New("mountpoint path already exists: " + mountpath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to retrieve mountpoint path information: %w", err)
	}
	if isDriveRootPath(mountpath) { // Assume intention with "X:\" was "X:"
		mountpath = mountpath[:len(mountpath)-1] // WinFsp needs drive mountpoints without trailing path separator
	}
	if !isDrive(mountpath) {
		// Assuming directory path, since it is not a pure drive letter string such as "X:".
		// Drive letter string can be used as is, since we have already checked it does not exist,
		// but directory path needs more checks.
		if opt.NetworkMode {
			fs.Errorf(nil, "Ignoring --network-mode as it is not supported with directory mountpoint")
			opt.NetworkMode = false
		}
		var err error
		if mountpath, err = filepath.Abs(mountpath); err != nil { // Ensures parent is found but also more informative log messages
			return "", fmt.Errorf("mountpoint path is not valid: %s: %w", mountpath, err)
		}
		parent := filepath.Join(mountpath, "..")
		if _, err = os.Stat(parent); err != nil {
			if os.IsNotExist(err) {
				return "", errors.New("parent of mountpoint directory does not exist: " + parent)
			}
			return "", fmt.Errorf("failed to retrieve mountpoint directory parent information: %w", err)
		}
		if err = mountlib.CheckOverlap(f, mountpath); err != nil {
			return "", err
		}
	}
	return mountpath, nil
}

// handleVolumeName handles the volume name option.
//
// MapFTPDrive local patch: upstream reused VolumeName as the network volume
// prefix and, for a plain name, invented "\server\<VolumeName>". Here
// VolumeName is only ever the volume label (see volumeLabelOptions) and the
// prefix comes from an explicit --VolumePrefix entry in ExtraFlags or from a
// UNC mount path (volumeName). resolveVolumePrefix in cmount_volume.go
// documents the rules; a network mount without a valid prefix fails.
func handleVolumeName(opt *mountlib.Options, volumeName string) error {
	flags, network, err := resolveVolumePrefix(opt.ExtraFlags, opt.NetworkMode, volumeName)
	if err != nil {
		return err
	}
	if network && !opt.NetworkMode {
		fs.Debugf(nil, "Forcing network mode because a volume prefix is set")
	}
	opt.ExtraFlags, opt.NetworkMode = flags, network
	return nil
}

// getMountpoint handles mounting details on Windows,
// where disk and network based file systems are treated different.
func getMountpoint(f fs.Fs, mountpath string, opt *mountlib.Options) (mountpoint string, err error) {
	// Inform about some options not relevant in this mode
	if opt.AllowNonEmpty {
		fs.Logf(nil, "--allow-non-empty flag does nothing on Windows")
	}
	if opt.AllowRoot {
		fs.Logf(nil, "--allow-root flag does nothing on Windows")
	}
	if opt.AllowOther {
		fs.Logf(nil, "--allow-other flag does nothing on Windows")
	}

	// Handle mountpath
	var volumeName string
	if isDefaultPath(mountpath) {
		// Mount path indicates defaults, which will automatically pick an unused drive letter.
		mountpoint, err = handleDefaultMountpath()
	} else if isNetworkSharePath(mountpath) {
		// Mount path is a valid network share path (UNC format, "\\Server\Share" prefix).
		mountpoint, err = handleNetworkShareMountpath(mountpath, opt)
		// In this case the volume name is taken from the mount path, will replace any existing volume name option.
		volumeName = mountpath[1:] // WinFsp requires volume prefix as UNC-like path but with only a single backslash
	} else {
		// Mount path is drive letter or directory path.
		mountpoint, err = handleLocalMountpath(f, mountpath, opt)
	}

	// Handle volume name
	// MapFTPDrive local patch: an invalid volume prefix is returned like a
	// mount point error, so mount stops before calling WinFsp.
	if err == nil {
		err = handleVolumeName(opt, volumeName)
	}

	// Done, return mountpoint to be used, together with updated mount options.
	if opt.NetworkMode {
		fs.Debugf(nil, "Network mode mounting is enabled")
	} else {
		fs.Debugf(nil, "Network mode mounting is disabled")
	}
	return
}
