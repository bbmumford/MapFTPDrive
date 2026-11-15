package mount

// fileSystemOption is the WinFsp FUSE FileSystemName value given to every
// MapFTPDrive volume. WinFsp prefixes it with "FUSE-" and keeps at most 15
// characters of the result, so it must stay at 10 characters or fewer.
const fileSystemOption = "MapFTP"

// VolumeFileSystem is the file system name Windows reports for a mounted
// MapFTPDrive volume (GetVolumeInformation and Explorer's
// System.Volume.FileSystem property). The Explorer drive verbs' AppliesTo
// condition in internal/explorer and both installers matches this exact
// value, so drive commands appear only on MapFTPDrive volumes.
const VolumeFileSystem = "FUSE-" + fileSystemOption

// FileSystemNameFlag returns the WinFsp FUSE flag that makes a volume report
// VolumeFileSystem. The engine appends it after the mount adapter's defaults
// because WinFsp applies the last FileSystemName option it parses.
func FileSystemNameFlag() string {
	return "--FileSystemName=" + fileSystemOption
}
