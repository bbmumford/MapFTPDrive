package explorer

import (
	"fmt"
	"strings"
)

const (
	// ThisPCShell is the shell key of the This PC (CLSID_MyComputer) object,
	// relative to Software\Classes.
	ThisPCShell = `CLSID\{20D04FE0-3AEA-1069-A2D8-08002B30309D}\shell`
	// DriveShell is the shell key shared by every drive root, relative to
	// Software\Classes.
	DriveShell = `Drive\shell`

	// DriveAppliesTo limits the drive verbs to volumes whose file system
	// name is the one every MapFTPDrive mount reports
	// (mount.VolumeFileSystem). Explorer evaluates it per drive, so C:, USB
	// and other network drives never show the commands. The commands still
	// validate the drive against the managed stores, because any FUSE
	// volume could choose the same name.
	DriveAppliesTo = `System.Volume.FileSystem:="FUSE-MapFTP"`
)

// Verb is one Explorer context-menu command. Both installers register the
// same table (installer/MapFTPDrive.wxs and installer/MapFTPDrive.iss) under
// HKLM\Software\Classes; verbs_test.go fails when either diverges from it.
type Verb struct {
	// Key is the verb's registry key relative to Software\Classes.
	Key string
	// Label is the menu text, stored as MUIVerb.
	Label string
	// Args follow the quoted executable path in the verb's command string.
	Args string
	// Extended verbs appear only in the Shift+right-click menu.
	Extended bool
	// Shield marks a verb that requests UAC elevation.
	Shield bool
	// AppliesTo is the Advanced Query Syntax condition Explorer evaluates
	// before showing the verb; empty means always shown.
	AppliesTo string
}

// driveArgument is the --drive value for drive verbs. Explorer expands %V
// to a root such as X:\. Quoted as "%V" the trailing backslash escapes the
// closing quote under the Windows argv rules and the program receives X:"
// instead; the dot keeps the backslash away from the quote and
// NormalizeDrive removes it again.
const driveArgument = `--drive "%V."`

// Verbs lists every Explorer command MapFTPDrive registers.
var Verbs = []Verb{
	{Key: ThisPCShell + `\MapFTPDrive`, Label: "Map FTP Drive", Args: `map --scope user`},
	{Key: ThisPCShell + `\MapFTPDriveSystem`, Label: "Map FTP Drive (system-wide)", Args: `map --scope system`, Extended: true, Shield: true},
	{Key: ThisPCShell + `\MapFTPDriveMount`, Label: "Mount FTP Drive", Args: `mount`},
	{Key: DriveShell + `\MapFTPDriveUnmount`, Label: "Unmount FTP Drive", Args: `unmount ` + driveArgument, AppliesTo: DriveAppliesTo},
	{Key: DriveShell + `\MapFTPDriveRename`, Label: "Rename FTP Drive", Args: `rename ` + driveArgument, AppliesTo: DriveAppliesTo},
	{Key: DriveShell + `\MapFTPDriveRemove`, Label: "Remove FTP Drive", Args: `remove ` + driveArgument, Extended: true, AppliesTo: DriveAppliesTo},
}

// CommandLine returns the registry command string for verb when the
// executable is installed at executable.
func (v Verb) CommandLine(executable string) string {
	return `"` + executable + `" ` + v.Args
}

// IconLocation returns the Icon value shown beside every verb.
func IconLocation(executable string) string {
	return executable
}

// NormalizeDrive reduces a --drive argument to its upper-case drive letter.
// It accepts X, X:, X:\ and X:\. as well as the X:" form produced by a
// quoted "%V" registration, so verbs registered by any installer version
// resolve to the same drive. Anything else, such as a directory path, is
// rejected before it reaches the managed-drive lookup.
func NormalizeDrive(argument string) (string, error) {
	value := strings.TrimSpace(argument)
	value = strings.TrimLeft(value, `"`)
	value = strings.TrimRight(value, `"\/. `)
	value = strings.TrimSuffix(value, ":")
	if len(value) != 1 {
		return "", fmt.Errorf("%q is not a drive letter", argument)
	}
	letter := value[0]
	if letter >= 'a' && letter <= 'z' {
		letter -= 'a' - 'A'
	}
	if letter < 'A' || letter > 'Z' {
		return "", fmt.Errorf("%q is not a drive letter", argument)
	}
	return string(letter), nil
}
