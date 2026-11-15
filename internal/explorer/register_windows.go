//go:build windows

package explorer

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const classesRoot = `Software\Classes\`

func Install() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	for _, item := range Verbs {
		if err := writeVerb(registry.CURRENT_USER, item, executable); err != nil {
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

// writeVerb replaces the verb's key so values dropped from the table, such
// as a former Extended flag, do not survive an upgrade.
func writeVerb(root registry.Key, item Verb, executable string) error {
	if err := deleteVerb(root, item); err != nil {
		return err
	}
	key, _, err := registry.CreateKey(root, classesRoot+item.Key, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create Explorer verb %s: %w", item.Key, err)
	}
	defer key.Close()
	values := map[string]string{
		"MUIVerb": item.Label,
		"Icon":    IconLocation(executable),
	}
	if item.Extended {
		values["Extended"] = ""
	}
	if item.Shield {
		values["HasLUAShield"] = ""
	}
	if item.AppliesTo != "" {
		values["AppliesTo"] = item.AppliesTo
	}
	for name, value := range values {
		if err := key.SetStringValue(name, value); err != nil {
			return fmt.Errorf("write Explorer verb %s: %w", item.Key, err)
		}
	}
	command, _, err := registry.CreateKey(key, "command", registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer command.Close()
	return command.SetStringValue("", item.CommandLine(executable))
}

func deleteVerb(root registry.Key, item Verb) error {
	var first error
	for _, path := range []string{classesRoot + item.Key + `\command`, classesRoot + item.Key} {
		if err := registry.DeleteKey(root, path); err != nil && !errors.Is(err, registry.ErrNotExist) && first == nil {
			first = err
		}
	}
	return first
}

func Uninstall() error {
	var first error
	for i := len(Verbs) - 1; i >= 0; i-- {
		if err := deleteVerb(registry.CURRENT_USER, Verbs[i]); err != nil && first == nil {
			first = err
		}
	}
	run, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err == nil {
		if deleteErr := run.DeleteValue("MapFTPDrive"); deleteErr != nil && !errors.Is(deleteErr, registry.ErrNotExist) && first == nil {
			first = deleteErr
		}
		run.Close()
	}
	return first
}
