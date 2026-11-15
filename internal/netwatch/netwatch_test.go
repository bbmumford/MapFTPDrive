package netwatch

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

var (
	loopback = Interface{Name: "lo", Up: true, Loopback: true, Addresses: []net.IP{net.ParseIP("127.0.0.1")}}
	linkOnly = Interface{Name: "eth0", Up: true, Addresses: []net.IP{net.ParseIP("169.254.10.10"), net.ParseIP("fe80::1")}}
	leased   = Interface{Name: "eth0", Up: true, Addresses: []net.IP{net.ParseIP("192.0.2.10")}}
	down     = Interface{Name: "wlan0", Up: false, Addresses: []net.IP{net.ParseIP("198.51.100.4")}}
)

func TestReady(t *testing.T) {
	cases := []struct {
		interfaces []Interface
		want       bool
	}{
		{nil, false},
		{[]Interface{loopback}, false},
		{[]Interface{loopback, linkOnly}, false},
		{[]Interface{down}, false},
		{[]Interface{loopback, leased}, true},
		{[]Interface{{Name: "v6", Up: true, Addresses: []net.IP{net.ParseIP("2001:db8::5")}}}, true},
	}
	for index, c := range cases {
		if got := Ready(c.interfaces); got != c.want {
			t.Errorf("case %d: Ready = %v, want %v", index, got, c.want)
		}
	}
}

type fakeNetwork struct {
	mu         sync.Mutex
	interfaces []Interface
}

func (f *fakeNetwork) set(interfaces ...Interface) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.interfaces = interfaces
}

func (f *fakeNetwork) snapshot() []Interface {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Interface(nil), f.interfaces...)
}

func TestWaitReady(t *testing.T) {
	network := &fakeNetwork{}
	network.set(loopback)
	changes := make(chan struct{}, 1)
	result := make(chan bool, 1)
	go func() { result <- WaitReady(context.Background(), changes, network.snapshot, time.Minute) }()
	network.set(loopback, leased)
	changes <- struct{}{}
	select {
	case ready := <-result:
		if !ready {
			t.Fatal("WaitReady returned false for a ready network")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitReady ignored the change signal")
	}

	network.set(linkOnly)
	if WaitReady(context.Background(), nil, network.snapshot, 50*time.Millisecond) {
		t.Fatal("link-local only network reported ready")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if WaitReady(ctx, nil, network.snapshot, time.Minute) {
		t.Fatal("cancelled wait reported ready")
	}
	network.set(leased)
	if !WaitReady(context.Background(), nil, network.snapshot, time.Minute) {
		t.Fatal("ready network not reported immediately")
	}
}

func TestPollerSignalsChanges(t *testing.T) {
	network := &fakeNetwork{}
	network.set(loopback)
	poller := NewPoller(5*time.Millisecond, network.snapshot)
	defer poller.Close()
	select {
	case <-poller.Changes():
		t.Fatal("signal without a change")
	case <-time.After(30 * time.Millisecond):
	}
	network.set(loopback, leased)
	select {
	case <-poller.Changes():
	case <-time.After(5 * time.Second):
		t.Fatal("address change not signalled")
	}
	if err := poller.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFingerprintIgnoresOrderAndDownInterfaces(t *testing.T) {
	a := fingerprint([]Interface{leased, loopback, down})
	b := fingerprint([]Interface{loopback, leased})
	if a != b {
		t.Fatalf("%q != %q", a, b)
	}
	if fingerprint([]Interface{loopback}) == b {
		t.Fatal("fingerprint missed an added interface")
	}
}

func TestNewAndSnapshot(t *testing.T) {
	watcher := New()
	defer watcher.Close()
	if watcher.Changes() == nil {
		t.Fatal("watcher without a change channel")
	}
	for _, entry := range Snapshot() {
		if entry.Name == "" {
			t.Fatal("unnamed interface")
		}
	}
}
