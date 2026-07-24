//go:build windows

package credentials

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
	credPrefix              = "MapFTPDrive/"
)

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	advapi32    = windows.NewLazySystemDLL("advapi32.dll")
	credWriteW  = advapi32.NewProc("CredWriteW")
	credReadW   = advapi32.NewProc("CredReadW")
	credDeleteW = advapi32.NewProc("CredDeleteW")
	credFree    = advapi32.NewProc("CredFree")
)

type windowsStore struct{}

func newPlatformStore() Store { return windowsStore{} }

func target(key string) (*uint16, error) {
	return windows.UTF16PtrFromString(credPrefix + key)
}

func (windowsStore) Put(key, username, password string) error {
	targetName, err := target(key)
	if err != nil {
		return err
	}
	userName, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return err
	}
	blob := []byte(password)
	item := credential{
		Type:               credTypeGeneric,
		TargetName:         targetName,
		CredentialBlobSize: uint32(len(blob)),
		Persist:            credPersistLocalMachine,
		UserName:           userName,
	}
	if len(blob) != 0 {
		item.CredentialBlob = &blob[0]
	}
	ok, _, callErr := credWriteW.Call(uintptr(unsafe.Pointer(&item)), 0)
	if ok == 0 {
		return callErr
	}
	return nil
}

func (windowsStore) Get(key string) (string, string, error) {
	targetName, err := target(key)
	if err != nil {
		return "", "", err
	}
	var item *credential
	ok, _, callErr := credReadW.Call(
		uintptr(unsafe.Pointer(targetName)),
		credTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&item)),
	)
	if ok == 0 {
		if errors.Is(callErr, windows.ERROR_NOT_FOUND) {
			return "", "", ErrNotFound
		}
		return "", "", callErr
	}
	defer credFree.Call(uintptr(unsafe.Pointer(item)))
	username := windows.UTF16PtrToString(item.UserName)
	var password string
	if item.CredentialBlobSize != 0 {
		password = string(unsafe.Slice(item.CredentialBlob, item.CredentialBlobSize))
	}
	return username, password, nil
}

func (windowsStore) Delete(key string) error {
	targetName, err := target(key)
	if err != nil {
		return err
	}
	ok, _, callErr := credDeleteW.Call(uintptr(unsafe.Pointer(targetName)), credTypeGeneric, 0)
	if ok == 0 && !errors.Is(callErr, windows.ERROR_NOT_FOUND) {
		return callErr
	}
	return nil
}
