//go:build windows

package explorer

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const (
	classesRoot = `Software\Classes\`
	runKey      = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// RemoveUserRegistration deletes any copy of the Explorer verbs and the
// startup entry from the current user's hive. Both belong in HKLM, where
// the installers write them: a per-user copy shadows the machine-wide verbs
// for that user (HKCU wins in the merged HKCR view) with whatever command
// strings it holds, and starts a second host at sign-in. The EXE installer
// runs it during install and uninstall for the installing account.
func RemoveUserRegistration() error {
	var first error
	keep := func(err error) {
		if err != nil && !errors.Is(err, registry.ErrNotExist) && first == nil {
			first = err
		}
	}
	for i := len(Verbs) - 1; i >= 0; i-- {
		keep(registry.DeleteKey(registry.CURRENT_USER, classesRoot+Verbs[i].Key+`\command`))
		keep(registry.DeleteKey(registry.CURRENT_USER, classesRoot+Verbs[i].Key))
	}
	run, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err == nil {
		keep(run.DeleteValue("MapFTPDrive"))
		run.Close()
	}
	return first
}
