//go:build !windows

package mount

func SetVolumeLabel(string, string) error { return ErrNotSupported }
func VolumeLabel(string) (string, error)  { return "", ErrNotSupported }
