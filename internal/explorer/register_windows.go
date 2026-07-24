//go:build windows

package explorer

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	thisPCShell = `Software\Classes\CLSID\{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell`
	driveShell  = `Software\Classes\Drive\shell`
)

type verb struct {
	key      string
	label    string
	args     string
	extended bool
	shield   bool
}

var verbs = []verb{
	{thisPCShell + `\MapFTPDrive`, "Map FTP Drive", `map --scope user`, false, false},
	{thisPCShell + `\MapFTPDriveSystem`, "Map FTP Drive (system-wide)", `map --scope system`, true, true},
	{driveShell + `\MapFTPDriveUnmount`, "Unmount FTP Drive", `unmount --drive "%V"`, false, false},
	{driveShell + `\MapFTPDriveRename`, "Rename FTP Drive", `rename --drive "%V"`, false, false},
	{driveShell + `\MapFTPDriveRemove`, "Remove FTP Drive", `remove --drive "%V"`, true, false},
}

func Install() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	for _, item := range verbs {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, item.key, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("create Explorer verb %s: %w", item.key, err)
		}
		if err := key.SetStringValue("", item.label); err != nil {
			key.Close()
			return err
		}
		if err := key.SetStringValue("Icon", executable); err != nil {
			key.Close()
			return err
		}
		if item.extended {
			if err := key.SetStringValue("Extended", ""); err != nil {
				key.Close()
				return err
			}
		}
		if item.shield {
			if err := key.SetStringValue("HasLUAShield", ""); err != nil {
				key.Close()
				return err
			}
		}
		command, _, err := registry.CreateKey(key, "command", registry.SET_VALUE)
		if err != nil {
			key.Close()
			return err
		}
		err = command.SetStringValue("", fmt.Sprintf(`"%s" %s`, executable, item.args))
		command.Close()
		key.Close()
		if err != nil {
			return err
		}
	}
	run, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer run.Close()
	return run.SetStringValue("MapFTPDrive", fmt.Sprintf(`"%s" daemon --scope user`, executable))
}

func Uninstall() error {
	var first error
	for i := len(verbs) - 1; i >= 0; i-- {
		if err := registry.DeleteKey(registry.CURRENT_USER, verbs[i].key+`\command`); err != nil && err != registry.ErrNotExist && first == nil {
			first = err
		}
		if err := registry.DeleteKey(registry.CURRENT_USER, verbs[i].key); err != nil && err != registry.ErrNotExist && first == nil {
			first = err
		}
	}
	run, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err == nil {
		if deleteErr := run.DeleteValue("MapFTPDrive"); deleteErr != nil && deleteErr != registry.ErrNotExist && first == nil {
			first = deleteErr
		}
		run.Close()
	}
	return first
}
