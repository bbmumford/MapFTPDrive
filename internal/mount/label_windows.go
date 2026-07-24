//go:build windows

package mount

import (
	"strings"

	"golang.org/x/sys/windows"
)

func SetVolumeLabel(drive, label string) error {
	root, err := windows.UTF16PtrFromString(strings.TrimRight(drive, `:\`) + `:\`)
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(label)
	if err != nil {
		return err
	}
	return windows.SetVolumeLabel(root, name)
}

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
