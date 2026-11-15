// Package cleanup removes what MapFTPDrive leaves on a computer when it is
// uninstalled: running user hosts, stored passwords, endpoint stores, SFTP
// host keys, logs and caches. Both installers run it as LocalSystem, the
// only account that can reach every user's host pipe, its own credential
// vault and every profile.
package cleanup

// Options selects what Run removes.
type Options struct {
	// KeepData stops the hosts but leaves stores, credentials and logs for
	// a later reinstall.
	KeepData bool
	// Executable is the installed MapFTPDrive.exe, started in each signed-in
	// user's session to remove that user's credentials.
	Executable string
}

// Run stops every user host and, unless KeepData is set, removes:
//   - for each signed-in user, the MapFTPDrive/* entries in their Credential
//     Manager and their %LOCALAPPDATA%\MapFTPDrive (a copy of this program
//     runs "purge-user" in their session, because only the user's own logon
//     can open their credential vault);
//   - %LOCALAPPDATA%\MapFTPDrive in every other profile (their Credential
//     Manager entries cannot be reached and remain; see README.md);
//   - the MapFTPDrive/* entries in LocalSystem's Credential Manager and
//     %ProgramData%\MapFTPDrive.
//
// It continues past individual failures and returns them joined.
func Run(options Options) error { return run(options) }

// PurgeCurrentUser removes the calling user's MapFTPDrive/* credentials and
// %LOCALAPPDATA%\MapFTPDrive. Run starts it in each signed-in session.
func PurgeCurrentUser() error { return purgeCurrentUser() }

// IsLocalSystem reports whether the process runs as LocalSystem, which Run
// requires.
func IsLocalSystem() bool { return isLocalSystem() }
