package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"mapftpdrive/internal/config"
	"mapftpdrive/internal/credentials"
	"mapftpdrive/internal/mount"
)

type Supervisor struct {
	store       *config.Store
	credentials credentials.Store
	engine      mount.Engine

	mu      sync.Mutex
	mounted map[string]mount.Handle
	cancel  context.CancelFunc
}

func NewSupervisor(store *config.Store, credentialStore credentials.Store, engine mount.Engine) *Supervisor {
	return &Supervisor{
		store:       store,
		credentials: credentialStore,
		engine:      engine,
		mounted:     make(map[string]mount.Handle),
	}
}

func NewID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(data)
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:]), nil
}

func (s *Supervisor) Save(endpoint config.Endpoint, password string) error {
	endpoint = endpoint.Normalized()
	if endpoint.ID == "" {
		id, err := NewID()
		if err != nil {
			return err
		}
		endpoint.ID = id
	}
	if endpoint.CredentialKey == "" {
		endpoint.CredentialKey = "endpoint/" + endpoint.ID
	}
	if err := endpoint.Validate(); err != nil {
		return err
	}
	previousUser, previousPassword, previousErr := s.credentials.Get(endpoint.CredentialKey)
	if err := s.credentials.Put(endpoint.CredentialKey, endpoint.Username, password); err != nil {
		return fmt.Errorf("store credential: %w", err)
	}
	if err := s.store.Upsert(endpoint); err != nil {
		if previousErr == nil {
			_ = s.credentials.Put(endpoint.CredentialKey, previousUser, previousPassword)
		} else {
			_ = s.credentials.Delete(endpoint.CredentialKey)
		}
		return err
	}
	return nil
}

func (s *Supervisor) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		cancel()
		return errors.New("supervisor is already running")
	}
	s.cancel = cancel
	s.mu.Unlock()

	endpoints, err := s.store.Load()
	if err != nil {
		s.Stop()
		return err
	}
	var failures []error
	for _, endpoint := range endpoints {
		if endpoint.Disabled {
			continue
		}
		if err := s.Mount(ctx, endpoint); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", endpoint.Name, err))
		}
	}
	go s.watchLabels(ctx)
	return errors.Join(failures...)
}

func (s *Supervisor) Mount(ctx context.Context, endpoint config.Endpoint) error {
	s.mu.Lock()
	if _, exists := s.mounted[endpoint.ID]; exists {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	username, password, err := s.credentials.Get(endpoint.CredentialKey)
	if err != nil {
		return fmt.Errorf("load credential: %w", err)
	}
	if username != "" {
		endpoint.Username = username
	}
	var knownHostsFile string
	if endpoint.Protocol == config.SFTP && endpoint.KnownHost != "" {
		layout := config.LayoutFor(s.store.Path())
		if err := os.MkdirAll(layout.KnownHostsDir(), 0o700); err != nil {
			return err
		}
		knownHostsFile = layout.KnownHostsFile(endpoint.ID)
		if err := os.WriteFile(knownHostsFile, []byte(endpoint.KnownHost+"\n"), 0o600); err != nil {
			return err
		}
	}
	handle, err := s.engine.Mount(ctx, mount.Spec{Endpoint: endpoint, Secret: mount.Secret{Password: password}, KnownHostsFile: knownHostsFile})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.mounted[endpoint.ID] = handle
	s.mu.Unlock()
	go func() {
		_ = handle.Wait()
		s.mu.Lock()
		if current := s.mounted[endpoint.ID]; current == handle {
			delete(s.mounted, endpoint.ID)
		}
		s.mu.Unlock()
	}()
	return nil
}

func (s *Supervisor) MountByDrive(ctx context.Context, drive string) error {
	endpoint, err := s.store.FindByDrive(drive)
	if err != nil {
		return err
	}
	if endpoint.Disabled {
		if _, err := s.store.SetDisabled(endpoint.ID, false); err != nil {
			return err
		}
		endpoint.Disabled = false
	}
	return s.Mount(ctx, endpoint)
}

func (s *Supervisor) UnmountByDrive(drive string, retain bool) error {
	endpoint, err := s.store.FindByDrive(drive)
	if err != nil {
		return err
	}
	s.mu.Lock()
	handle := s.mounted[endpoint.ID]
	s.mu.Unlock()
	if handle != nil {
		if err := handle.Unmount(); err != nil {
			return err
		}
		s.mu.Lock()
		delete(s.mounted, endpoint.ID)
		s.mu.Unlock()
	}
	if retain {
		_, err = s.store.SetDisabled(endpoint.ID, true)
		return err
	}
	removed, err := s.store.Remove(endpoint.ID)
	if err != nil {
		return err
	}
	if err := os.Remove(config.LayoutFor(s.store.Path()).KnownHostsFile(removed.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return s.credentials.Delete(removed.CredentialKey)
}

func (s *Supervisor) RenameByDrive(drive, name string) error {
	endpoint, err := s.store.FindByDrive(drive)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("drive name is required")
	}
	if err := mount.SetVolumeLabel(endpoint.DriveLetter, name); err == nil {
		_, err = s.store.Rename(endpoint.ID, name)
		return err
	}
	// Network-mode volumes mounted by the SYSTEM service cannot always change
	// their live label. Persist the new name and remount atomically from the
	// supervisor's point of view so the WinFsp volume prefix is rebuilt.
	updated, err := s.store.Rename(endpoint.ID, name)
	if err != nil {
		return err
	}
	s.mu.Lock()
	handle := s.mounted[endpoint.ID]
	s.mu.Unlock()
	if handle == nil {
		return nil
	}
	if err := handle.Unmount(); err != nil {
		return fmt.Errorf("remount renamed volume: %w", err)
	}
	s.mu.Lock()
	delete(s.mounted, endpoint.ID)
	s.mu.Unlock()
	return s.Mount(context.Background(), updated)
}

func (s *Supervisor) watchLabels(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			endpoints, err := s.store.Load()
			if err != nil {
				continue
			}
			for _, endpoint := range endpoints {
				s.mu.Lock()
				_, active := s.mounted[endpoint.ID]
				s.mu.Unlock()
				if !active {
					continue
				}
				label, err := mount.VolumeLabel(endpoint.DriveLetter)
				if err == nil && strings.TrimSpace(label) != "" && label != endpoint.Name {
					_, _ = s.store.Rename(endpoint.ID, label)
				}
			}
		}
	}
}

func (s *Supervisor) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	handles := make([]mount.Handle, 0, len(s.mounted))
	for _, handle := range s.mounted {
		handles = append(handles, handle)
	}
	s.mounted = make(map[string]mount.Handle)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, handle := range handles {
		_ = handle.Unmount()
	}
}

func (s *Supervisor) EndpointByDrive(drive string) (config.Endpoint, error) {
	endpoint, err := s.store.FindByDrive(drive)
	if errors.Is(err, os.ErrNotExist) {
		return config.Endpoint{}, fmt.Errorf("drive %q is not managed by MapFTPDrive", drive)
	}
	return endpoint, err
}
