//go:build !windows

package mount

func VolumeLabel(string) (string, error) { return "", ErrNotSupported }

// UsedDriveLetters reports no letters in use: drive letters are a Windows
// concept, and the portable code paths only use the mask for clash checks.
func UsedDriveLetters() (uint32, error) { return 0, nil }
