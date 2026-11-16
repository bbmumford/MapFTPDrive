//go:build !windows

package netwatch

import "errors"

func newPlatformWatcher() (Watcher, error) {
	return nil, errors.New("network change notifications are only implemented on Windows")
}
