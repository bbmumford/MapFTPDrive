//go:build windows

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// systemDirectorySDDL protects %ProgramData%\MapFTPDrive: owned by
	// Administrators, no ACEs inherited from %ProgramData% (which lets
	// Users create files), and full control for SYSTEM and Administrators
	// inherited by every file the service creates inside it.
	systemDirectorySDDL = "O:BAG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	// driveIndexSDDL lets every local user read the drive index, which
	// unelevated Explorer verbs need, while only SYSTEM and Administrators
	// may replace it.
	driveIndexSDDL = "O:BAG:SYD:PAI(A;;FA;;;SY)(A;;FA;;;BA)(A;;FR;;;BU)"

	trustedInstallerSID = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"

	// writeAccess is every right that changes an object, its contents, its
	// security descriptor or the entries of a directory.
	writeAccess = 0x00000002 | // FILE_WRITE_DATA, FILE_ADD_FILE
		0x00000004 | // FILE_APPEND_DATA, FILE_ADD_SUBDIRECTORY
		0x00000010 | // FILE_WRITE_EA
		0x00000040 | // FILE_DELETE_CHILD
		0x00000100 | // FILE_WRITE_ATTRIBUTES
		0x00010000 | // DELETE
		0x00040000 | // WRITE_DAC
		0x00080000 | // WRITE_OWNER
		0x10000000 | // GENERIC_ALL
		0x40000000 // GENERIC_WRITE

	fileDispositionInfoEx          = 21
	fileDispositionDelete          = 0x1
	fileDispositionPOSIXSemantics  = 0x2
	fileDispositionIgnoreReadOnly  = 0x10
	securityInformationOwnerDACL   = windows.OWNER_SECURITY_INFORMATION | windows.DACL_SECURITY_INFORMATION
	accessReadSecurity             = windows.READ_CONTROL
	accessRepair                   = windows.READ_CONTROL | windows.WRITE_DAC | windows.WRITE_OWNER | windows.DELETE
	accessRemove                   = windows.READ_CONTROL | windows.DELETE
	errUnsupportedAccessEntryClass = "contains an access entry type this program does not write"
)

// PrepareSystemDirectory makes the system directory ready for the service:
// it creates the directory with its protected security descriptor when it
// is missing and otherwise requires VerifySystemDirectory to pass. The
// service refuses to start on error, because a directory that other users
// can write lets them alter the endpoints the service mounts with the
// stored passwords, or redirect its file writes.
func PrepareSystemDirectory() error {
	dir, err := SystemDirectory()
	if err != nil {
		return err
	}
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		if err := createSystemDirectory(dir); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return VerifySystemDirectory()
}

// VerifySystemDirectory reports an error unless the system directory and
// each entry directly inside it is a real file or directory (not a
// reparse point), is owned by SYSTEM, Administrators or TrustedInstaller,
// and grants write access to no other principal. Deeper entries inherit
// the directory's protection.
func VerifySystemDirectory() error {
	dir, err := SystemDirectory()
	if err != nil {
		return err
	}
	if err := verifyObject(dir); err != nil {
		return fmt.Errorf("%s %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if err := verifyObject(path); err != nil {
			return fmt.Errorf("%s %w", path, err)
		}
	}
	return nil
}

// SecureSystemDirectory creates the system directory or repairs an existing
// one; the installers run it as an administrator or SYSTEM. A pre-planted
// junction or symbolic link is removed rather than followed. The directory
// is locked to SYSTEM and Administrators first, then every entry inside it
// that another principal could have written (owned by someone else, writable
// by someone else, a reparse point, or a file with additional hard links)
// is deleted, because its contents cannot be trusted. Finally the protected
// descriptor is applied again through the inheritance-propagating API so
// the remaining entries inherit it.
func SecureSystemDirectory() error {
	dir, err := SystemDirectory()
	if err != nil {
		return err
	}
	// Backup and restore rights let an administrator open and delete entries
	// whose DACL denies Administrators; take-ownership lets it re-own the
	// directory. They are absent for ordinary users, which is harmless here.
	enablePrivileges("SeBackupPrivilege", "SeRestorePrivilege", "SeTakeOwnershipPrivilege")

	handle, err := openNoFollow(dir, accessRepair)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return createSystemDirectory(dir)
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", dir, err)
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		windows.CloseHandle(handle)
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		err := deleteOnClose(handle)
		windows.CloseHandle(handle)
		if err != nil {
			return fmt.Errorf("remove %s: %w", dir, err)
		}
		return createSystemDirectory(dir)
	}
	defer windows.CloseHandle(handle)

	descriptor, err := windows.SecurityDescriptorFromString(systemDirectorySDDL)
	if err != nil {
		return err
	}
	// SetKernelObjectSecurity changes only this directory, so the entries
	// keep the descriptors that decide below whether they are trusted.
	if err := windows.SetKernelObjectSecurity(handle, securityInformationOwnerDACL, descriptor); err != nil {
		return fmt.Errorf("protect %s: %w", dir, err)
	}
	if err := sanitizeChildren(dir); err != nil {
		return err
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		securityInformationOwnerDACL|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		owner, nil, dacl, nil); err != nil {
		return fmt.Errorf("protect %s: %w", dir, err)
	}
	return VerifySystemDirectory()
}

// ensureStoreDirectory creates the directory that holds a store. The system
// store's directory is created with its protected descriptor so a plain
// MkdirAll never gives it the ProgramData ACL, which lets Users add files.
func ensureStoreDirectory(dir string) error {
	system, err := SystemDirectory()
	if err == nil && strings.EqualFold(filepath.Clean(dir), filepath.Clean(system)) {
		if _, statErr := os.Lstat(dir); errors.Is(statErr, os.ErrNotExist) {
			if err := createSystemDirectory(dir); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
				return err
			}
		}
		return nil
	}
	return os.MkdirAll(dir, 0o700)
}

func createSystemDirectory(dir string) error {
	descriptor, err := windows.SecurityDescriptorFromString(systemDirectorySDDL)
	if err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	attributes := windows.SecurityAttributes{SecurityDescriptor: descriptor}
	attributes.Length = uint32(unsafe.Sizeof(attributes))
	return windows.CreateDirectory(name, &attributes)
}

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

func openNoFollow(path string, access uint32) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(
		name,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
}

func verifyObject(path string) error {
	handle, err := openNoFollow(path, accessReadSecurity)
	if err != nil {
		return fmt.Errorf("cannot be inspected: %w", err)
	}
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return fmt.Errorf("cannot be inspected: %w", err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("is a junction or symbolic link")
	}
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, securityInformationOwnerDACL)
	if err != nil {
		return fmt.Errorf("cannot be inspected: %w", err)
	}
	return checkSecurity(descriptor)
}

func trustedPrincipal(sid *windows.SID) bool {
	if sid == nil {
		return false
	}
	return sid.IsWellKnown(windows.WinLocalSystemSid) ||
		sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) ||
		sid.IsWellKnown(windows.WinCreatorOwnerSid) ||
		sid.String() == trustedInstallerSID
}

// checkSecurity reports why descriptor would let a principal other than
// SYSTEM, Administrators or TrustedInstaller change the object. CREATOR
// OWNER is accepted because only those principals can create entries.
func checkSecurity(descriptor *windows.SECURITY_DESCRIPTOR) error {
	owner, _, err := descriptor.Owner()
	if err != nil {
		return fmt.Errorf("cannot be inspected: %w", err)
	}
	if !trustedPrincipal(owner) {
		return fmt.Errorf("is owned by %s", owner)
	}
	dacl, _, err := descriptor.DACL()
	if errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) || (err == nil && dacl == nil) {
		return errors.New("has no access control list, which grants everyone full access")
	}
	if err != nil {
		return fmt.Errorf("cannot be inspected: %w", err)
	}
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return fmt.Errorf("cannot be inspected: %w", err)
		}
		switch ace.Header.AceType {
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if uint32(ace.Mask)&writeAccess != 0 && !trustedPrincipal(sid) {
				return fmt.Errorf("grants write access to %s", sid)
			}
		case windows.ACCESS_DENIED_ACE_TYPE:
		default:
			return errors.New(errUnsupportedAccessEntryClass)
		}
	}
	return nil
}

func sanitizeChildren(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := sanitizeEntry(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeEntry(path string) error {
	handle, err := openNoFollow(path, accessReadSecurity)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	var info windows.ByHandleFileInformation
	infoErr := windows.GetFileInformationByHandle(handle, &info)
	descriptor, securityErr := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, securityInformationOwnerDACL)
	windows.CloseHandle(handle)
	if infoErr != nil {
		return fmt.Errorf("inspect %s: %w", path, infoErr)
	}
	directory := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	trusted := securityErr == nil &&
		info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 &&
		(directory || info.NumberOfLinks == 1) &&
		checkSecurity(descriptor) == nil
	if !trusted {
		return removeTree(path)
	}
	if directory {
		return sanitizeChildren(path)
	}
	return nil
}

// removeTree deletes path and, for a real directory, everything below it,
// opening each entry without following reparse points so a link is removed
// itself and its target is never touched.
func removeTree(path string) error {
	handle, err := openNoFollow(path, accessRemove)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 && info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		for _, entry := range entries {
			if err := removeTree(filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
	}
	if err := deleteOnClose(handle); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// deleteOnClose marks the object behind handle for deletion when the handle
// closes. The extended form also removes read-only files; Windows versions
// without it fall back to the plain disposition.
func deleteOnClose(handle windows.Handle) error {
	flags := uint32(fileDispositionDelete | fileDispositionPOSIXSemantics | fileDispositionIgnoreReadOnly)
	err := windows.SetFileInformationByHandle(handle, fileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	if err == nil {
		return nil
	}
	disposition := byte(1)
	return windows.SetFileInformationByHandle(handle, windows.FileDispositionInfo, &disposition, 1)
}

func enablePrivileges(names ...string) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return
	}
	defer token.Close()
	for _, name := range names {
		pointer, err := windows.UTF16PtrFromString(name)
		if err != nil {
			continue
		}
		var luid windows.LUID
		if windows.LookupPrivilegeValue(nil, pointer, &luid) != nil {
			continue
		}
		privileges := windows.Tokenprivileges{PrivilegeCount: 1}
		privileges.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
		_ = windows.AdjustTokenPrivileges(token, false, &privileges, 0, nil, nil)
	}
}
