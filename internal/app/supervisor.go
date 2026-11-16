// Package app holds the mount supervisor shared by the per-user host and the
// LocalSystem service: it keeps every enabled endpoint mounted, reconnects
// with backoff, applies user operations (map, edit, mount, unmount, rename,
// re-pin, remove) and reports per-endpoint status.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"

	"mapftpdrive/internal/config"
	"mapftpdrive/internal/credentials"
	"mapftpdrive/internal/logging"
	"mapftpdrive/internal/mount"
	"mapftpdrive/internal/security"
)

type State string

const (
	Disabled   State = "disabled"
	Connecting State = "connecting"
	Mounted    State = "mounted"
	Retrying   State = "retrying"
	// Attention: retrying cannot succeed until the user acts (changed or
	// unpinned server identity, missing credential or key file).
	Attention State = "attention"
)

// TrustProblem carries the fingerprints behind an identity failure.
type TrustProblem struct {
	Kind      string
	Pinned    string
	Presented string
}

const (
	TrustHostKeyChanged       = "host-key-changed"
	TrustHostKeyNotPinned     = "host-key-not-pinned"
	TrustCertificateChanged   = "certificate-changed"
	TrustCertificateUntrusted = "certificate-untrusted"
)

type Status struct {
	Endpoint  config.Endpoint
	State     State
	LastError string
	RetryAt   time.Time
	Since     time.Time
	Attempts  int
	Trust     *TrustProblem
}

// Report is the status of every endpoint in the scope plus store problems.
type Report struct {
	Endpoints  []Status
	Problems   []config.Problem
	StoreError string
}

// Prober reads a server's current identity before a mount.
type Prober interface {
	SFTPHostKey(context.Context, config.Endpoint) (security.HostKey, error)
	TLSCertificate(context.Context, config.Endpoint) (security.Certificate, error)
}

type networkProber struct{}

func (networkProber) SFTPHostKey(ctx context.Context, endpoint config.Endpoint) (security.HostKey, error) {
	return security.ScanSFTPHostKeyContext(ctx, endpoint)
}

func (networkProber) TLSCertificate(ctx context.Context, endpoint config.Endpoint) (security.Certificate, error) {
	return security.ScanTLSCertificate(ctx, endpoint)
}

// Options are the supervisor's dependencies. Store, Credentials and Engine
// are required; the rest have production defaults.
type Options struct {
	Store       *config.Store
	Credentials credentials.Store
	Engine      mount.Engine
	Prober      Prober
	// UsedLetters reports drive letters in use in this process's view of
	// the namespace (GetLogicalDrives on Windows) as a bit mask, bit 0 = A.
	UsedLetters func() (uint32, error)
	// OtherScopeDrives lists drives managed by the other scope (the system
	// drive index, for the per-user host).
	OtherScopeDrives func() ([]config.DriveIndexEntry, error)
	Backoff          Backoff
	// AttemptTimeout bounds one connect-and-mount attempt.
	AttemptTimeout time.Duration
	// SettleTimeout bounds how long Mount, Rename and Repin wait for the
	// first attempt before answering "still connecting".
	SettleTimeout time.Duration
	Logger        *slog.Logger
	Now           func() time.Time
}

type entry struct {
	// op serialises user operations on this endpoint so a mount and an
	// unmount can never interleave.
	op sync.Mutex

	// Fields below are guarded by Supervisor.mu.
	endpoint config.Endpoint
	status   Status
	version  uint64
	changed  chan struct{}
	cancel   context.CancelFunc
	done     chan struct{}
	wake     chan struct{}
}

type Supervisor struct {
	store       *config.Store
	credentials credentials.Store
	engine      mount.Engine
	prober      Prober
	layout      config.Layout
	options     Options
	log         *slog.Logger

	// mapMu serialises Map and Update, which check and change letter and
	// name assignments across endpoints.
	mapMu sync.Mutex

	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	entries  map[string]*entry
	problems []config.Problem
	storeErr string
}

func New(options Options) *Supervisor {
	if options.Prober == nil {
		options.Prober = networkProber{}
	}
	if options.Backoff.Initial == 0 {
		options.Backoff = DefaultBackoff
	}
	if options.AttemptTimeout == 0 {
		options.AttemptTimeout = 60 * time.Second
	}
	if options.SettleTimeout == 0 {
		options.SettleTimeout = 20 * time.Second
	}
	if options.Logger == nil {
		options.Logger = logging.Discard().Logger
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Supervisor{
		store:       options.Store,
		credentials: options.Credentials,
		engine:      options.Engine,
		prober:      options.Prober,
		layout:      config.LayoutFor(options.Store.Path()),
		options:     options,
		log:         options.Logger,
		entries:     make(map[string]*entry),
	}
}

// NewSupervisor builds a supervisor with default options.
func NewSupervisor(store *config.Store, credentialStore credentials.Store, engine mount.Engine) *Supervisor {
	return New(Options{Store: store, Credentials: credentialStore, Engine: engine})
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

// Load reads the store and lists every endpoint without mounting; enabled
// endpoints show as connecting until Start runs. The host calls it before
// serving requests so status is complete while it waits for the network.
func (s *Supervisor) Load() ([]config.Endpoint, error) {
	snapshot, err := s.store.Snapshot()
	s.mu.Lock()
	s.problems = snapshot.Problems
	s.storeErr = ""
	if err != nil {
		s.storeErr = err.Error()
	}
	s.mu.Unlock()
	for _, problem := range snapshot.Problems {
		s.log.Warn("skipped invalid endpoint entry", "index", problem.Index, "id", problem.ID, "error", problem.Error)
	}
	if err != nil {
		s.log.Error("load endpoint store", "error", err)
		return nil, err
	}
	for _, endpoint := range snapshot.Endpoints {
		e := s.ensureEntry(endpoint)
		if !endpoint.Disabled && !s.loopRunning(e) {
			s.setStatus(e, func(status *Status) {
				status.State = Connecting
				status.LastError = "waiting for the network"
			})
		}
	}
	return snapshot.Endpoints, nil
}

// Start loads the store and supervises every enabled endpoint in the
// background. It returns at once; mounts proceed with retries, so one
// unreachable server never delays the others or the control pipe.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.ctx != nil {
		s.mu.Unlock()
		return errors.New("supervisor is already running")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()

	endpoints, err := s.Load()
	if err != nil {
		return err
	}
	for _, endpoint := range endpoints {
		if endpoint.Disabled {
			continue
		}
		e := s.ensureEntry(endpoint)
		e.op.Lock()
		// Re-read under the endpoint lock: an Unmount or Remove that ran
		// since Load must win over the snapshot.
		if current, err := s.store.Find(endpoint.ID); err == nil && !current.Disabled {
			s.setEndpoint(e, current)
			s.startLoop(e, nil)
		}
		e.op.Unlock()
	}
	return nil
}

// Stop ends supervision and unmounts everything.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	entries := make([]*entry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, e := range entries {
		s.stopLoop(e)
	}
}

// NetworkChanged wakes every endpoint waiting to retry so it reconnects now
// rather than at its next backoff time.
func (s *Supervisor) NetworkChanged() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.status.State == Retrying {
			wake(e.wake)
		}
	}
}

func wake(channel chan struct{}) {
	select {
	case channel <- struct{}{}:
	default:
	}
}

// Status reports every endpoint ordered by drive letter. Endpoints present
// in the store but not yet supervised (for example after a failed Start)
// appear as disabled or connecting from their stored state.
func (s *Supervisor) Status() Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	report := Report{Problems: append([]config.Problem(nil), s.problems...), StoreError: s.storeErr}
	for _, e := range s.entries {
		status := e.status
		status.Endpoint = e.endpoint
		report.Endpoints = append(report.Endpoints, status)
	}
	sort.Slice(report.Endpoints, func(i, j int) bool {
		return report.Endpoints[i].Endpoint.DriveLetter < report.Endpoints[j].Endpoint.DriveLetter
	})
	return report
}

// ResolveID finds the endpoint ID for a request that names a drive.
func (s *Supervisor) ResolveID(id, drive string) (string, error) {
	if id != "" {
		return id, nil
	}
	endpoint, err := s.store.FindByDrive(drive)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("drive %s: is not managed by MapFTPDrive", config.NormalizeDrive(drive))
	}
	return endpoint.ID, err
}

func (s *Supervisor) EndpointByDrive(drive string) (config.Endpoint, error) {
	endpoint, err := s.store.FindByDrive(drive)
	if errors.Is(err, os.ErrNotExist) {
		return config.Endpoint{}, fmt.Errorf("drive %q is not managed by MapFTPDrive", drive)
	}
	return endpoint, err
}

// ensureEntry returns the entry for endpoint, creating it in the state its
// stored definition implies.
func (s *Supervisor) ensureEntry(endpoint config.Endpoint) *entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[endpoint.ID]
	if e == nil {
		e = &entry{changed: make(chan struct{}), wake: make(chan struct{}, 1)}
		e.status = Status{State: Disabled, Since: s.options.Now()}
		s.entries[endpoint.ID] = e
	}
	e.endpoint = endpoint
	return e
}

func (s *Supervisor) lookup(id string) *entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[id]
}

func (s *Supervisor) forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, id)
}

// setStatus records a state change and wakes anyone waiting for it.
func (s *Supervisor) setStatus(e *entry, change func(*Status)) {
	s.mu.Lock()
	previous := e.status.State
	change(&e.status)
	if e.status.State != previous {
		e.status.Since = s.options.Now()
	}
	e.version++
	close(e.changed)
	e.changed = make(chan struct{})
	s.mu.Unlock()
}

func (s *Supervisor) setEndpoint(e *entry, endpoint config.Endpoint) {
	s.mu.Lock()
	e.endpoint = endpoint
	s.mu.Unlock()
}

func (s *Supervisor) currentEndpoint(e *entry) config.Endpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return e.endpoint
}

// waitSettled waits until the entry's state changed after version and is no
// longer Connecting, or ctx ends.
func (s *Supervisor) waitSettled(ctx context.Context, e *entry, version uint64) (Status, bool) {
	for {
		s.mu.Lock()
		status, changed := e.status, e.changed
		settled := e.version > version && status.State != Connecting
		s.mu.Unlock()
		if settled {
			return status, true
		}
		select {
		case <-ctx.Done():
			return status, false
		case <-changed:
		}
	}
}

// settings returns the tunable options under the lock, so they can be
// adjusted while loops run.
func (s *Supervisor) settings() Options {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.options
}

func (s *Supervisor) version(e *entry) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return e.version
}
