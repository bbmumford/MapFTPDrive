package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"mapftpdrive/internal/config"
	"mapftpdrive/internal/mount"
)

type memoryCredentials struct {
	values map[string][2]string
}

func (m *memoryCredentials) Put(key, username, password string) error {
	m.values[key] = [2]string{username, password}
	return nil
}
func (m *memoryCredentials) Get(key string) (string, string, error) {
	value, ok := m.values[key]
	if !ok {
		return "", "", errors.New("missing credential")
	}
	return value[0], value[1], nil
}
func (m *memoryCredentials) Delete(key string) error {
	delete(m.values, key)
	return nil
}

type fakeHandle struct {
	unmounted chan struct{}
	once      sync.Once
}

func newFakeHandle() *fakeHandle {
	return &fakeHandle{unmounted: make(chan struct{})}
}
func (h *fakeHandle) Wait() error {
	<-h.unmounted
	return nil
}
func (h *fakeHandle) Unmount() error {
	h.once.Do(func() { close(h.unmounted) })
	return nil
}

type fakeEngine struct {
	mounted chan config.Endpoint
}

func (f *fakeEngine) Mount(_ context.Context, endpoint config.Endpoint, _ mount.Secret) (mount.Handle, error) {
	f.mounted <- endpoint
	return newFakeHandle(), nil
}

func TestSupervisorRestoresAndUnmounts(t *testing.T) {
	store := config.NewStore(filepath.Join(t.TempDir(), "endpoints.json"))
	credentials := &memoryCredentials{values: make(map[string][2]string)}
	engine := &fakeEngine{mounted: make(chan config.Endpoint, 1)}
	supervisor := NewSupervisor(store, credentials, engine)
	endpoint := config.Endpoint{
		Name:        "Archive",
		Protocol:    config.SFTP,
		Host:        "example.test",
		Port:        22,
		Username:    "user",
		DriveLetter: "R",
		Scope:       config.User,
	}
	if err := supervisor.Save(endpoint, "secret"); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	mounted := <-engine.mounted
	if mounted.Name != endpoint.Name {
		t.Fatalf("mounted %q, want %q", mounted.Name, endpoint.Name)
	}
	if err := supervisor.UnmountByDrive("R:", true); err != nil {
		t.Fatal(err)
	}
	stored, err := store.FindByDrive("R")
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Disabled {
		t.Fatal("unmounted endpoint was not retained as disabled")
	}
	supervisor.Stop()
}
