//go:build windows

package config

import (
	"errors"
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// driveIndexSDDL lets every local user read the drive index, which
// unelevated Explorer verbs, the mapper and user hosts need, while only
// SYSTEM and Administrators may replace it. The DACL is protected so the
// administrators-only ACL of the containing directory is not inherited
// over it.
const driveIndexSDDL = "O:BAG:SYD:PAI(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;BU)"

// writeDriveIndexFile atomically replaces the drive index with data. The
// replacement is created with driveIndexSDDL rather than inheriting the
// directory's descriptor, which would hide it from unelevated readers.
func writeDriveIndexFile(path string, data []byte) error {
	descriptor, err := windows.SecurityDescriptorFromString(driveIndexSDDL)
	if err != nil {
		return err
	}
	temp := path + ".new"
	if err := os.Remove(temp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	name, err := windows.UTF16PtrFromString(temp)
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{SecurityDescriptor: descriptor}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	// CREATE_NEW: an existing file would keep its own descriptor.
	handle, err := windows.CreateFile(name, windows.GENERIC_WRITE, 0, &attributes, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return fmt.Errorf("create %s: %w", temp, err)
	}
	file := os.NewFile(uintptr(handle), temp)
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(temp)
		return err
	}
	from, err := windows.UTF16PtrFromString(temp)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// A reader that opened the index without FILE_SHARE_DELETE blocks the
	// replacement briefly; retry for about half a second.
	for attempt := 0; ; attempt++ {
		err = windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		retry := errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
		if err == nil || !retry || attempt == 10 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
