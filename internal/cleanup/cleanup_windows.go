//go:build windows

package cleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
)

const (
	// vaultTargetFilter matches every target the credentials package writes
	// ("MapFTPDrive/" followed by the endpoint's credential key).
	vaultTargetFilter = "MapFTPDrive/*"
	// hostStopTimeout is how long hosts get to unmount after Shutdown.
	hostStopTimeout = 30 * time.Second
	// userPurgeTimeout bounds each "purge-user" run in a user's session.
	userPurgeTimeout = time.Minute
	profileListKey   = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList`
	executableName   = "MapFTPDrive.exe"
)

var errNoSignedInUser = errors.New("no user is signed in to the session")

func isLocalSystem() bool {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	return err == nil && user.User.Sid.IsWellKnown(windows.WinLocalSystemSid)
}

func run(options Options) error {
	if !isLocalSystem() {
		return errors.New("uninstall cleanup must run as LocalSystem")
	}
	var failures []error
	running, err := control.StopUserHosts(context.Background(), hostStopTimeout)
	if err != nil {
		failures = append(failures, fmt.Errorf("stop user hosts: %w", err))
	}
	// Hosts that ignore Shutdown or do not exit in time would keep
	// MapFTPDrive.exe in use and its drives mounted after removal.
	if err := terminateHosts(running); err != nil {
		failures = append(failures, err)
	}
	if options.KeepData {
		return errors.Join(failures...)
	}
	if err := purgeSignedInUsers(options.Executable); err != nil {
		failures = append(failures, err)
	}
	if err := removeProfileData(); err != nil {
		failures = append(failures, err)
	}
	if err := deleteCredentials(); err != nil {
		failures = append(failures, fmt.Errorf("delete LocalSystem credentials: %w", err))
	}
	if dir, err := config.SystemDirectory(); err == nil {
		if err := os.RemoveAll(dir); err != nil {
			failures = append(failures, fmt.Errorf("remove %s: %w", dir, err))
		}
	}
	return errors.Join(failures...)
}

func purgeCurrentUser() error {
	var failures []error
	if err := deleteCredentials(); err != nil {
		failures = append(failures, fmt.Errorf("delete credentials: %w", err))
	}
	if path, err := config.DefaultPath(config.User); err == nil {
		if err := os.RemoveAll(filepath.Dir(path)); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// credential mirrors the Win32 CREDENTIALW structure.
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
	advapi32           = windows.NewLazySystemDLL("advapi32.dll")
	procCredEnumerateW = advapi32.NewProc("CredEnumerateW")
	procCredDeleteW    = advapi32.NewProc("CredDeleteW")
	procCredFree       = advapi32.NewProc("CredFree")
)

// deleteCredentials removes every MapFTPDrive credential of the calling
// account, including ones whose endpoint is no longer in any store.
func deleteCredentials() error {
	filter, err := windows.UTF16PtrFromString(vaultTargetFilter)
	if err != nil {
		return err
	}
	var count uint32
	var list **credential
	ok, _, callErr := procCredEnumerateW.Call(
		uintptr(unsafe.Pointer(filter)),
		0,
		uintptr(unsafe.Pointer(&count)),
		uintptr(unsafe.Pointer(&list)),
	)
	if ok == 0 {
		if errors.Is(callErr, windows.ERROR_NOT_FOUND) {
			return nil
		}
		return callErr
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(list)))
	var failures []error
	for _, item := range unsafe.Slice(list, count) {
		ok, _, callErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(item.TargetName)), uintptr(item.Type), 0)
		if ok == 0 && !errors.Is(callErr, windows.ERROR_NOT_FOUND) {
			failures = append(failures, fmt.Errorf("%s: %w", windows.UTF16PtrToString(item.TargetName), callErr))
		}
	}
	return errors.Join(failures...)
}

// purgeSignedInUsers runs "purge-user" as the user of every session that has
// one, in that session, and waits for each.
func purgeSignedInUsers(executable string) error {
	var sessions *windows.WTS_SESSION_INFO
	var count uint32
	if err := windows.WTSEnumerateSessions(0, 0, 1, &sessions, &count); err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(sessions)))
	var failures []error
	for _, session := range unsafe.Slice(sessions, count) {
		if session.SessionID == 0 {
			continue
		}
		err := runInSession(session.SessionID, executable, "purge-user")
		if err != nil && !errors.Is(err, errNoSignedInUser) {
			failures = append(failures, fmt.Errorf("remove the data of the user in session %d: %w", session.SessionID, err))
		}
	}
	return errors.Join(failures...)
}

// runInSession starts executable with argument as the user signed in to
// session, with that user's environment, and waits for it to succeed.
func runInSession(session uint32, executable, argument string) error {
	var token windows.Token
	if err := windows.WTSQueryUserToken(session, &token); err != nil {
		if errors.Is(err, windows.ERROR_NO_TOKEN) {
			return errNoSignedInUser
		}
		return err
	}
	defer token.Close()
	var environment *uint16
	if err := windows.CreateEnvironmentBlock(&environment, token, false); err != nil {
		return err
	}
	defer windows.DestroyEnvironmentBlock(environment)
	commandLine, err := windows.UTF16PtrFromString(syscall.EscapeArg(executable) + " " + argument)
	if err != nil {
		return err
	}
	desktop, err := windows.UTF16PtrFromString(`winsta0\default`)
	if err != nil {
		return err
	}
	startup := windows.StartupInfo{Desktop: desktop, Flags: windows.STARTF_USESHOWWINDOW, ShowWindow: windows.SW_HIDE}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	var process windows.ProcessInformation
	if err := windows.CreateProcessAsUser(token, nil, commandLine, nil, nil, false,
		windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NO_WINDOW, environment, nil, &startup, &process); err != nil {
		return err
	}
	defer windows.CloseHandle(process.Process)
	defer windows.CloseHandle(process.Thread)
	event, err := windows.WaitForSingleObject(process.Process, uint32(userPurgeTimeout/time.Millisecond))
	if err != nil {
		return err
	}
	if event != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("%s did not finish within %s", argument, userPurgeTimeout)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(process.Process, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("%s exited with code %d", argument, code)
	}
	return nil
}

// removeProfileData deletes %LOCALAPPDATA%\MapFTPDrive from every profile
// listed under ProfileList. os.RemoveAll removes a junction or symbolic
// link itself and never descends into its target, so a link planted in a
// profile cannot redirect the deletion.
func removeProfileData() error {
	list, err := registry.OpenKey(registry.LOCAL_MACHINE, profileListKey, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return fmt.Errorf("open profile list: %w", err)
	}
	defer list.Close()
	sids, err := list.ReadSubKeyNames(-1)
	if err != nil {
		return fmt.Errorf("read profile list: %w", err)
	}
	var failures []error
	for _, sid := range sids {
		profile, err := profilePath(list, sid)
		if err != nil || profile == "" {
			continue
		}
		dir := filepath.Join(profile, "AppData", "Local", "MapFTPDrive")
		if err := os.RemoveAll(dir); err != nil {
			failures = append(failures, fmt.Errorf("remove %s: %w", dir, err))
		}
	}
	return errors.Join(failures...)
}

func profilePath(list registry.Key, sid string) (string, error) {
	key, err := registry.OpenKey(list, sid, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer key.Close()
	value, _, err := key.GetStringValue("ProfileImagePath")
	if err != nil {
		return "", err
	}
	return registry.ExpandString(value)
}

// terminateHosts ends user hosts that did not exit after Shutdown, checking
// first that each process still runs MapFTPDrive.exe so a reused process ID
// is never terminated.
func terminateHosts(pids []uint32) error {
	if len(pids) == 0 {
		return nil
	}
	enablePrivilege("SeDebugPrivilege")
	var failures []error
	for _, pid := range pids {
		process, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
		if err != nil {
			continue
		}
		if isMapFTPDrive(process) {
			if err := windows.TerminateProcess(process, 1); err != nil {
				failures = append(failures, fmt.Errorf("terminate host process %d: %w", pid, err))
			} else {
				_, _ = windows.WaitForSingleObject(process, 5000)
			}
		}
		windows.CloseHandle(process)
	}
	return errors.Join(failures...)
}

func isMapFTPDrive(process windows.Handle) bool {
	buffer := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(windows.MAX_LONG_PATH)
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return false
	}
	return strings.EqualFold(filepath.Base(windows.UTF16ToString(buffer[:size])), executableName)
}

func enablePrivilege(name string) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return
	}
	defer token.Close()
	pointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return
	}
	var luid windows.LUID
	if windows.LookupPrivilegeValue(nil, pointer, &luid) != nil {
		return
	}
	privileges := windows.Tokenprivileges{PrivilegeCount: 1}
	privileges.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	_ = windows.AdjustTokenPrivileges(token, false, &privileges, 0, nil, nil)
}
