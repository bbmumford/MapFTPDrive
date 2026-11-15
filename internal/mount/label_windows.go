//go:build windows

package mount

import (
	"strings"

	"golang.org/x/sys/windows"
)

// VolumeLabel reads a drive's live volume label. The engine compares it
// with the drive name after mounting to detect a label the cmount adapter
// did not apply.
func VolumeLabel(drive string) (string, error) {
	root, err := windows.UTF16PtrFromString(strings.TrimRight(drive, `:\`) + `:\`)
	if err != nil {
		return "", err
	}
	buffer := make([]uint16, windows.MAX_PATH+1)
	err = windows.GetVolumeInformation(
		root,
		&buffer[0],
		uint32(len(buffer)),
		nil,
		nil,
		nil,
		nil,
		0,
	)
	if err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer), nil
}

// UsedDriveLetters returns GetLogicalDrives' mask (bit 0 = A:) for the
// calling process's view of the drive namespace: global letters plus those
// of its own logon session.
func UsedDriveLetters() (uint32, error) {
	return windows.GetLogicalDrives()
}
