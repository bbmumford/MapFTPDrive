//go:build windows

package netwatch

import (
	"errors"
	"sync"

	"golang.org/x/sys/windows"
)

// Windows notification callbacks are created with windows.NewCallback,
// which allocates from a small process-wide pool that is never freed, so a
// single callback is created once and fans out to every live watcher.
var (
	callbackOnce sync.Once
	callback     uintptr
	watchersMu   sync.Mutex
	watchers     = make(map[*windowsWatcher]struct{})
)

func notify(_ uintptr, _ uintptr, _ uint32) uintptr {
	watchersMu.Lock()
	defer watchersMu.Unlock()
	for watcher := range watchers {
		signal(watcher.changes)
	}
	return 0
}

type windowsWatcher struct {
	changes    chan struct{}
	interfaces windows.Handle
	address    windows.Handle
	closeOnce  sync.Once
}

// newPlatformWatcher registers for interface state changes (adapter up or
// down) and unicast address changes (DHCP lease, roaming), for IPv4 and IPv6.
func newPlatformWatcher() (Watcher, error) {
	callbackOnce.Do(func() { callback = windows.NewCallback(notify) })
	watcher := &windowsWatcher{changes: make(chan struct{}, 1)}
	watchersMu.Lock()
	watchers[watcher] = struct{}{}
	watchersMu.Unlock()
	if err := windows.NotifyIpInterfaceChange(windows.AF_UNSPEC, callback, nil, false, &watcher.interfaces); err != nil {
		watcher.Close()
		return nil, err
	}
	if err := windows.NotifyUnicastIpAddressChange(windows.AF_UNSPEC, callback, nil, false, &watcher.address); err != nil {
		watcher.Close()
		return nil, err
	}
	return watcher, nil
}

func (w *windowsWatcher) Changes() <-chan struct{} { return w.changes }

func (w *windowsWatcher) Close() error {
	var errs []error
	w.closeOnce.Do(func() {
		for _, handle := range []windows.Handle{w.interfaces, w.address} {
			if handle != 0 {
				errs = append(errs, windows.CancelMibChangeNotify2(handle))
			}
		}
		watchersMu.Lock()
		delete(watchers, w)
		watchersMu.Unlock()
	})
	return errors.Join(errs...)
}
