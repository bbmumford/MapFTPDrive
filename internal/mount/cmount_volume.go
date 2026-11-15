package mount

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// The functions in this file give the WinFsp mount adapter
// (cmount_*_windows.go) its volume identity. They are MapFTPDrive code, not
// extracted from rclone, and depend only on plain values so they can be
// tested on any platform.
//
// Inputs, all taken from the mountlib.Options the engine passes to
// mountlib.NewMountPoint:
//
//   - VolumeName is the volume label, unescaped. mountlib.MountPoint.Mount
//     has already replaced ':' and '/' and cut it to 32 bytes before the
//     adapter runs.
//   - A network volume is requested with NetworkMode and exactly one
//     "--VolumePrefix=\host\share" entry in ExtraFlags.
//   - A "volname=" entry in ExtraOptions overrides the label derived from
//     VolumeName.
//
// The adapter never derives the prefix from the label or the drive letter:
// mountlib's 32-byte cut cannot carry an endpoint-unique prefix, and a
// prefix built from a display name collides when two drives share a name.

// winfspVolumePrefixFlag is WinFsp's FUSE command-line flag for a network
// volume's UNC prefix. It is passed as its own argument, outside the "-o"
// parser.
const winfspVolumePrefixFlag = "--VolumePrefix="

// volumePrefixMaxLength is the number of UTF-16 code units WinFsp keeps of
// a prefix (FSP_FSCTL_VOLUME_PREFIX_SIZE less its terminator); a longer
// prefix would be silently truncated.
const volumePrefixMaxLength = 191

// errNetworkPrefixMissing reports network mode without a volume prefix.
var errNetworkPrefixMissing = errors.New("network mode requires a " + winfspVolumePrefixFlag + `\host\share entry in ExtraFlags; VolumeName is only the volume label`)

// resolveVolumePrefix validates and normalises the network volume prefix.
//
// flags is mountlib.Options.ExtraFlags, networkMode is
// mountlib.Options.NetworkMode, and uncMountPath is the single-backslash
// prefix derived from a "\\host\share" mount path, or "" when the mount path
// is a drive letter or directory. It returns the flags with exactly one
// normalised prefix entry when the volume is a network volume, and whether it
// is one. A prefix from the mount path replaces any prefix flag.
//
// WinFsp turns the volume into a network volume whenever a prefix is
// present, so a prefix forces network mode, and network mode without a
// prefix is an error rather than a guessed name. The WinFsp driver accepts
// only prefixes that start with exactly one backslash and contain a second
// one; "\\host\share" is accepted here and shortened to that form.
func resolveVolumePrefix(flags []string, networkMode bool, uncMountPath string) ([]string, bool, error) {
	var prefix string
	var found int
	kept := make([]string, 0, len(flags)+1)
	for _, flag := range flags {
		if value, ok := strings.CutPrefix(flag, winfspVolumePrefixFlag); ok {
			prefix = value
			found++
			continue
		}
		kept = append(kept, flag)
	}
	if uncMountPath != "" {
		prefix, found = uncMountPath, 1
	}
	switch {
	case found > 1:
		return nil, false, fmt.Errorf("%d %s entries in ExtraFlags; expected one", found, winfspVolumePrefixFlag)
	case found == 0 && networkMode:
		return nil, false, errNetworkPrefixMissing
	case found == 0:
		return flags, false, nil
	}
	normalised, err := normaliseVolumePrefix(prefix)
	if err != nil {
		return nil, false, err
	}
	return append(kept, winfspVolumePrefixFlag+normalised), true, nil
}

// normaliseVolumePrefix returns prefix in WinFsp's "\host\share" form or an
// error when it cannot name a network share.
func normaliseVolumePrefix(prefix string) (string, error) {
	normalised := prefix
	if strings.HasPrefix(normalised, `\\`) {
		normalised = normalised[1:]
	}
	parts := strings.Split(normalised, `\`)
	if len(parts) != 3 || parts[0] != "" || parts[1] == "" || parts[2] == "" {
		return "", fmt.Errorf(`volume prefix %q is not of the form \host\share`, prefix)
	}
	if len(utf16.Encode([]rune(normalised))) > volumePrefixMaxLength {
		return "", fmt.Errorf("volume prefix %q is longer than %d UTF-16 code units", prefix, volumePrefixMaxLength)
	}
	return normalised, nil
}

// volumeLabelOptions returns the "-o volname=" arguments for label, or none
// when label is empty or extraOptions already carries a "volname=" entry.
//
// label is mountlib.Options.VolumeName after mountlib's 32-byte cut, which
// can split a multi-byte character; the partial character is dropped. The
// escaping happens here, after that cut, because a cut through an escape
// sequence would leave a trailing backslash that WinFsp's option parser
// reads past.
func volumeLabelOptions(label string, extraOptions []string) []string {
	for _, option := range extraOptions {
		if strings.HasPrefix(strings.TrimSpace(option), "volname=") {
			return nil
		}
	}
	label = strings.ToValidUTF8(label, "")
	if label == "" {
		return nil
	}
	return []string{"-o", "volname=" + escapeOptionValue(label)}
}

// escapeOptionValue escapes value for one entry of WinFsp's "-o" option
// list, which splits entries on ',' and treats '\' as an escape for the next
// character.
func escapeOptionValue(value string) string {
	return strings.NewReplacer(`\`, `\\`, `,`, `\,`).Replace(value)
}
