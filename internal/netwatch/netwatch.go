// Package netwatch tells the mount supervisor when the machine's network
// configuration changes, so endpoints waiting to reconnect retry at once
// instead of at their next backoff time, and lets the hosts wait for a usable
// network at boot or sign-in before the first mount attempt.
package netwatch

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"
)

// Watcher signals network changes on Changes; bursts are coalesced into one
// pending signal.
type Watcher interface {
	Changes() <-chan struct{}
	Close() error
}

// New returns the platform watcher: IP interface and unicast address change
// notifications on Windows, interface polling elsewhere or when the
// notification API is unavailable.
func New() Watcher {
	if watcher, err := newPlatformWatcher(); err == nil {
		return watcher
	}
	return NewPoller(5*time.Second, Snapshot)
}

// Interface is the subset of an interface Ready needs.
type Interface struct {
	Name      string
	Up        bool
	Loopback  bool
	Addresses []net.IP
}

// Snapshot lists the system's interfaces.
func Snapshot() []Interface {
	system, err := net.Interfaces()
	if err != nil {
		return nil
	}
	result := make([]Interface, 0, len(system))
	for _, candidate := range system {
		entry := Interface{
			Name:     candidate.Name,
			Up:       candidate.Flags&net.FlagUp != 0,
			Loopback: candidate.Flags&net.FlagLoopback != 0,
		}
		addresses, _ := candidate.Addrs()
		for _, address := range addresses {
			if network, ok := address.(*net.IPNet); ok {
				entry.Addresses = append(entry.Addresses, network.IP)
			}
		}
		result = append(result, entry)
	}
	return result
}

// Ready reports whether some non-loopback interface is up with a routable
// unicast address; link-local addresses alone (no DHCP lease yet) do not
// count.
func Ready(interfaces []Interface) bool {
	for _, candidate := range interfaces {
		if !candidate.Up || candidate.Loopback {
			continue
		}
		for _, address := range candidate.Addresses {
			if address.IsGlobalUnicast() {
				return true
			}
		}
	}
	return false
}

// WaitReady returns true once snapshot reports a ready network, false when
// timeout passes or ctx ends first. It re-checks on every change signal and
// once a second.
func WaitReady(ctx context.Context, changes <-chan struct{}, snapshot func() []Interface, timeout time.Duration) bool {
	if Ready(snapshot()) {
		return true
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return Ready(snapshot())
		case <-changes:
		case <-ticker.C:
		}
		if Ready(snapshot()) {
			return true
		}
	}
}

// fingerprint renders the up interfaces and their addresses canonically so
// two snapshots compare equal exactly when nothing relevant changed.
func fingerprint(interfaces []Interface) string {
	var parts []string
	for _, candidate := range interfaces {
		if !candidate.Up {
			continue
		}
		addresses := make([]string, 0, len(candidate.Addresses))
		for _, address := range candidate.Addresses {
			addresses = append(addresses, address.String())
		}
		sort.Strings(addresses)
		parts = append(parts, candidate.Name+"="+strings.Join(addresses, ","))
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// Poller signals when the interface fingerprint changes between polls.
type Poller struct {
	changes chan struct{}
	stop    chan struct{}
	done    chan struct{}
}

func NewPoller(interval time.Duration, snapshot func() []Interface) *Poller {
	poller := &Poller{changes: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(poller.done)
		last := fingerprint(snapshot())
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-poller.stop:
				return
			case <-ticker.C:
				if current := fingerprint(snapshot()); current != last {
					last = current
					signal(poller.changes)
				}
			}
		}
	}()
	return poller
}

func (p *Poller) Changes() <-chan struct{} { return p.changes }

func (p *Poller) Close() error {
	select {
	case <-p.stop:
	default:
		close(p.stop)
	}
	<-p.done
	return nil
}

// signal records a pending change without blocking.
func signal(changes chan struct{}) {
	select {
	case changes <- struct{}{}:
	default:
	}
}
