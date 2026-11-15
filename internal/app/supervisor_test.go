package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/credentials"
	"mapftpdrive/internal/mount"
	"mapftpdrive/internal/security"
)

type memoryCredentials struct {
	mu     sync.Mutex
	values map[string][2]string
	putErr error
}

func newMemoryCredentials() *memoryCredentials {
	return &memoryCredentials{values: make(map[string][2]string)}
}

func (m *memoryCredentials) Put(key, username, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putErr != nil {
		return m.putErr
	}
	m.values[key] = [2]string{username, password}
	return nil
}

func (m *memoryCredentials) Get(key string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.values[key]
	if !ok {
		return "", "", credentials.ErrNotFound
	}
	return value[0], value[1], nil
}

func (m *memoryCredentials) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, key)
	return nil
}

func (m *memoryCredentials) has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.values[key]
	return ok
}

type fakeHandle struct {
	ended chan struct{}
	once  sync.Once
	drop  error
}

func newFakeHandle() *fakeHandle {
	return &fakeHandle{ended: make(chan struct{})}
}

func (h *fakeHandle) Wait() error {
	<-h.ended
	return h.drop
}

func (h *fakeHandle) Unmount() error {
	h.once.Do(func() { close(h.ended) })
	return nil
}

func (h *fakeHandle) isUnmounted() bool {
	select {
	case <-h.ended:
		return true
	default:
		return false
	}
}

// fakeEngine records mounts; failures[id] makes that many attempts fail.
type fakeEngine struct {
	mu       sync.Mutex
	failures map[string]int
	block    chan struct{}
	mounts   []mount.Spec
	handles  []*fakeHandle
	probes   []mount.Spec
	probeErr error
	onMount  func(mount.Spec)
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{failures: make(map[string]int)}
}

func (f *fakeEngine) Mount(ctx context.Context, spec mount.Spec) (mount.Handle, error) {
	f.mu.Lock()
	block, hook := f.block, f.onMount
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	if hook != nil {
		hook(spec)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mounts = append(f.mounts, spec)
	if f.failures[spec.Endpoint.ID] > 0 {
		f.failures[spec.Endpoint.ID]--
		return nil, errors.New("server unreachable")
	}
	handle := newFakeHandle()
	f.handles = append(f.handles, handle)
	return handle, nil
}

func (f *fakeEngine) Probe(_ context.Context, spec mount.Spec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes = append(f.probes, spec)
	return f.probeErr
}

func (f *fakeEngine) setFailures(id string, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[id] = count
}

func (f *fakeEngine) live() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, handle := range f.handles {
		if !handle.isUnmounted() {
			count++
		}
	}
	return count
}

func (f *fakeEngine) lastHandle() *fakeHandle {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.handles[len(f.handles)-1]
}

func (f *fakeEngine) mountCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.mounts)
}

type fakeProber struct {
	mu          sync.Mutex
	hostKey     security.HostKey
	certificate security.Certificate
	err         error
}

func (p *fakeProber) SFTPHostKey(context.Context, config.Endpoint) (security.HostKey, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hostKey, p.err
}

func (p *fakeProber) TLSCertificate(context.Context, config.Endpoint) (security.Certificate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.certificate, p.err
}

func (p *fakeProber) setHostKey(key security.HostKey) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hostKey = key
}

func newHostKey(t *testing.T) security.HostKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := security.ParseKnownHost(knownhosts.Line([]string{"example.test"}, key))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

type harness struct {
	t           *testing.T
	store       *config.Store
	credentials *memoryCredentials
	engine      *fakeEngine
	prober      *fakeProber
	supervisor  *Supervisor
	hostKey     security.HostKey
	mu          sync.Mutex
	usedLetters uint32
	other       []config.DriveIndexEntry
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		t:           t,
		store:       config.NewStore(filepath.Join(t.TempDir(), "endpoints.json")),
		credentials: newMemoryCredentials(),
		engine:      newFakeEngine(),
		hostKey:     newHostKey(t),
	}
	h.prober = &fakeProber{hostKey: h.hostKey}
	h.supervisor = New(Options{
		Store:       h.store,
		Credentials: h.credentials,
		Engine:      h.engine,
		Prober:      h.prober,
		UsedLetters: func() (uint32, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.usedLetters, nil
		},
		OtherScopeDrives: func() ([]config.DriveIndexEntry, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.other, nil
		},
		Backoff:        Backoff{Initial: 5 * time.Millisecond, Max: 20 * time.Millisecond, Factor: 2},
		AttemptTimeout: 2 * time.Second,
		SettleTimeout:  2 * time.Second,
	})
	t.Cleanup(h.supervisor.Stop)
	return h
}

func (h *harness) endpoint(name, drive string) config.Endpoint {
	return config.Endpoint{
		Name:        name,
		Protocol:    config.SFTP,
		Host:        "example.test",
		Port:        22,
		Username:    "user",
		DriveLetter: drive,
		Scope:       config.User,
		KnownHost:   h.hostKey.Line,
	}
}

func (h *harness) mapDrive(name, drive string) config.Endpoint {
	h.t.Helper()
	mapped, err := h.supervisor.Map(context.Background(), h.endpoint(name, drive), "secret")
	if err != nil {
		h.t.Fatal(err)
	}
	h.waitState(mapped.ID, Mounted)
	return mapped
}

func (h *harness) status(id string) Status {
	for _, status := range h.supervisor.Status().Endpoints {
		if status.Endpoint.ID == id {
			return status
		}
	}
	return Status{}
}

func (h *harness) waitState(id string, state State) Status {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if status := h.status(id); status.State == state {
			return status
		}
		time.Sleep(2 * time.Millisecond)
	}
	h.t.Fatalf("endpoint %s never reached %s; last %+v", id, state, h.status(id))
	return Status{}
}

func (h *harness) setOptions(change func(*Options)) {
	h.supervisor.mu.Lock()
	defer h.supervisor.mu.Unlock()
	change(&h.supervisor.options)
}

func TestBackoffSchedule(t *testing.T) {
	backoff := DefaultBackoff
	backoff.Rand = func() float64 { return 0.5 }
	want := []time.Duration{5, 10, 20, 40, 80, 160, 300, 300}
	for index, expected := range want {
		if got := backoff.Delay(index + 1); got != expected*time.Second {
			t.Errorf("Delay(%d) = %v, want %v", index+1, got, expected*time.Second)
		}
	}
	if backoff.Delay(0) != 5*time.Second || backoff.Delay(1000) != 300*time.Second {
		t.Error("out-of-range failure counts not clamped")
	}
	backoff.Rand = func() float64 { return 0 }
	if got := backoff.Delay(1); got != 4*time.Second {
		t.Errorf("low jitter = %v", got)
	}
	backoff.Rand = func() float64 { return 0.999999 }
	if got := backoff.Delay(1); got < 5999*time.Millisecond || got > 6*time.Second {
		t.Errorf("high jitter = %v", got)
	}
	tiny := Backoff{Initial: time.Nanosecond, Max: time.Nanosecond, Factor: 2}
	if tiny.Delay(1) != time.Millisecond {
		t.Error("delay below the floor")
	}
	for range 100 {
		if delay := DefaultBackoff.Delay(3); delay < 16*time.Second || delay > 24*time.Second {
			t.Fatalf("random jitter out of bounds: %v", delay)
		}
	}
}

func TestMapTestsMountsThenSaves(t *testing.T) {
	h := newHarness(t)
	var violations []string
	h.engine.onMount = func(spec mount.Spec) {
		if _, err := h.store.Find(spec.Endpoint.ID); err == nil {
			violations = append(violations, "saved-before-mount")
		}
		if h.credentials.has(spec.Endpoint.CredentialKey) {
			violations = append(violations, "credential-before-mount")
		}
		if spec.Secret.Password != "secret" || spec.KnownHostsFile == "" {
			violations = append(violations, "bad-spec")
		}
	}
	mapped := h.mapDrive("Archive", "r")
	if len(violations) != 0 {
		t.Fatalf("map order violations: %v", violations)
	}
	if config.ValidateID(mapped.ID) != nil || mapped.DriveLetter != "R" || mapped.CredentialKey != config.CredentialKeyFor(mapped.ID) {
		t.Fatalf("mapped = %+v", mapped)
	}
	stored, err := h.store.Find(mapped.ID)
	if err != nil || stored.Name != "Archive" {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if _, password, _ := h.credentials.Get(mapped.CredentialKey); password != "secret" {
		t.Fatal("credential not stored")
	}
	knownHosts := config.LayoutFor(h.store.Path()).KnownHostsFile(mapped.ID)
	if data, err := os.ReadFile(knownHosts); err != nil || strings.TrimSpace(string(data)) != h.hostKey.Line {
		t.Fatalf("known_hosts = %q, %v", data, err)
	}
	if h.engine.mountCount() != 1 {
		t.Fatalf("the test mount must be adopted, not repeated: %d mounts", h.engine.mountCount())
	}
}

func TestMapRollsBackOnMountFailure(t *testing.T) {
	h := newHarness(t)
	endpoint := h.endpoint("Archive", "R")
	endpoint.ID = "00000000-0000-4000-8000-000000000042"
	h.engine.setFailures(endpoint.ID, 1)
	if _, err := h.supervisor.Map(context.Background(), endpoint, "secret"); err == nil {
		t.Fatal("expected mount failure")
	}
	if endpoints, _ := h.store.Load(); len(endpoints) != 0 {
		t.Fatalf("failed map left %+v", endpoints)
	}
	if h.credentials.has(config.CredentialKeyFor(endpoint.ID)) {
		t.Fatal("failed map left a credential")
	}
	if _, err := os.Stat(config.LayoutFor(h.store.Path()).KnownHostsFile(endpoint.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed map left a known_hosts file")
	}
	h.mapDrive("Archive", "R")
}

func TestMapRollsBackWhenSavingFails(t *testing.T) {
	h := newHarness(t)
	endpoint := h.endpoint("Archive", "R")
	endpoint.ID = "00000000-0000-4000-8000-000000000043"
	key := config.CredentialKeyFor(endpoint.ID)
	if err := h.credentials.Put(key, "old-user", "old-secret"); err != nil {
		t.Fatal(err)
	}
	// A directory where the store file belongs makes the save fail on every
	// OS once the mount has succeeded (directory permissions would not stop
	// a write on Windows).
	h.engine.onMount = func(mount.Spec) {
		if err := os.Mkdir(h.store.Path(), 0o700); err != nil {
			t.Error(err)
		}
	}
	if _, err := h.supervisor.Map(context.Background(), endpoint, "secret"); err == nil {
		t.Fatal("expected save failure")
	}
	if err := os.Remove(h.store.Path()); err != nil {
		t.Fatal(err)
	}
	if h.engine.live() != 0 {
		t.Fatal("mount not undone after a failed save")
	}
	if user, password, err := h.credentials.Get(key); err != nil || user != "old-user" || password != "old-secret" {
		t.Fatalf("previous credential not restored: %q %q %v", user, password, err)
	}

	h.engine.onMount = nil
	h.credentials.mu.Lock()
	h.credentials.putErr = errors.New("vault locked")
	h.credentials.mu.Unlock()
	if _, err := h.supervisor.Map(context.Background(), h.endpoint("Other", "S"), "secret"); err == nil || !strings.Contains(err.Error(), "vault locked") {
		t.Fatalf("credential failure = %v", err)
	}
	if h.engine.live() != 0 {
		t.Fatal("mount not undone after a credential failure")
	}
}

func TestMapRefusesClashes(t *testing.T) {
	h := newHarness(t)
	h.mapDrive("Archive", "R")
	cases := []struct {
		name    string
		setup   func() config.Endpoint
		message string
	}{
		{"mapped letter", func() config.Endpoint { return h.endpoint("Other", "R") }, "already mapped"},
		{"same name", func() config.Endpoint { return h.endpoint("archive", "S") }, "already named"},
		{"letter in use", func() config.Endpoint {
			h.mu.Lock()
			h.usedLetters = 1 << ('T' - 'A')
			h.mu.Unlock()
			return h.endpoint("Other", "T")
		}, "in use"},
		{"system drive", func() config.Endpoint {
			h.mu.Lock()
			h.other = []config.DriveIndexEntry{{Drive: "U", Scope: config.System, ID: "00000000-0000-4000-8000-000000000099", Name: "Shared"}}
			h.mu.Unlock()
			return h.endpoint("Other", "U")
		}, "system drive"},
		{"invalid name", func() config.Endpoint { return h.endpoint("a,b", "V") }, "cannot contain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := h.supervisor.Map(context.Background(), c.setup(), "x"); err == nil || !strings.Contains(err.Error(), c.message) {
				t.Fatalf("error = %v, want %q", err, c.message)
			}
		})
	}
}

func TestMapReusesOrReplacesDisabledLetter(t *testing.T) {
	h := newHarness(t)
	first := h.mapDrive("Archive", "R")
	if err := h.supervisor.Disable(first.ID); err != nil {
		t.Fatal(err)
	}
	again, err := h.supervisor.Map(context.Background(), h.endpoint("Archive again", "R"), "new-secret")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID {
		t.Fatal("same account on the same letter should re-use the disabled definition")
	}
	if _, password, _ := h.credentials.Get(first.CredentialKey); password != "new-secret" {
		t.Fatal("credential not updated on re-use")
	}
	h.waitState(first.ID, Mounted)
	if _, err := h.supervisor.Map(context.Background(), h.endpoint("Archive", "R"), "x"); err == nil {
		t.Fatal("map over an enabled drive accepted")
	}
	if err := h.supervisor.Disable(first.ID); err != nil {
		t.Fatal(err)
	}
	if h.status(first.ID).State != Disabled {
		t.Fatal("disable did not update status")
	}

	different := h.endpoint("Elsewhere", "R")
	different.Host = "other.test"
	replaced, err := h.supervisor.Map(context.Background(), different, "s")
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID == first.ID {
		t.Fatal("a different server must replace, not re-use")
	}
	if _, err := h.store.Find(first.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("replaced definition still stored")
	}
	if h.credentials.has(first.CredentialKey) {
		t.Fatal("replaced definition's credential kept")
	}
	if h.status(first.ID).State != "" {
		t.Fatal("replaced definition still reported")
	}
}

func seed(t *testing.T, h *harness, id, name, drive string) config.Endpoint {
	t.Helper()
	endpoint := h.endpoint(name, drive)
	endpoint.ID = id
	endpoint.CredentialKey = config.CredentialKeyFor(id)
	if err := h.store.Upsert(endpoint); err != nil {
		t.Fatal(err)
	}
	if err := h.credentials.Put(endpoint.CredentialKey, "user", "secret"); err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func TestRetriesWithBackoffAndRestartsDroppedMount(t *testing.T) {
	h := newHarness(t)
	endpoint := seed(t, h, "00000000-0000-4000-8000-000000000007", "Archive", "R")
	h.engine.setFailures(endpoint.ID, 2)
	if err := h.supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.supervisor.Start(context.Background()); err == nil {
		t.Fatal("second Start accepted")
	}
	h.waitState(endpoint.ID, Mounted)
	if h.engine.mountCount() != 3 {
		t.Fatalf("mount attempts = %d, want 3", h.engine.mountCount())
	}

	dropped := h.engine.lastHandle()
	dropped.drop = errors.New("connection reset")
	_ = dropped.Unmount()
	deadline := time.Now().Add(5 * time.Second)
	for h.engine.mountCount() < 4 {
		if time.Now().After(deadline) {
			t.Fatal("dropped mount not restarted")
		}
		time.Sleep(2 * time.Millisecond)
	}
	h.waitState(endpoint.ID, Mounted)
	if _, err := h.store.Find(endpoint.ID); err != nil {
		t.Fatal("dropped mount deleted its definition")
	}
}

func TestNetworkChangeWakesRetryingEndpoint(t *testing.T) {
	h := newHarness(t)
	h.setOptions(func(options *Options) { options.Backoff = Backoff{Initial: time.Hour, Max: time.Hour, Factor: 2} })
	endpoint := seed(t, h, "00000000-0000-4000-8000-000000000008", "Archive", "R")
	h.engine.setFailures(endpoint.ID, 1)
	if err := h.supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := h.waitState(endpoint.ID, Retrying)
	if status.LastError == "" || status.RetryAt.Before(time.Now().Add(30*time.Minute)) || status.Attempts != 1 {
		t.Fatalf("retrying status = %+v", status)
	}
	h.supervisor.NetworkChanged()
	h.waitState(endpoint.ID, Mounted)
}

func TestLoadShowsEndpointsBeforeStart(t *testing.T) {
	h := newHarness(t)
	enabled := seed(t, h, "00000000-0000-4000-8000-000000000011", "Archive", "R")
	disabled := seed(t, h, "00000000-0000-4000-8000-000000000012", "Old", "S")
	if _, err := h.store.SetDisabled(disabled.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.supervisor.Load(); err != nil {
		t.Fatal(err)
	}
	if h.status(enabled.ID).State != Connecting || h.status(disabled.ID).State != Disabled {
		t.Fatalf("status before start = %+v", h.supervisor.Status())
	}
	if h.engine.mountCount() != 0 {
		t.Fatal("Load mounted")
	}
}

func TestStoreProblemsAreReported(t *testing.T) {
	h := newHarness(t)
	if err := os.WriteFile(h.store.Path(), []byte(`{"version":1,"endpoints":[{"id":"../x"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := h.supervisor.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if report := h.supervisor.Status(); len(report.Problems) != 1 || report.StoreError != "" {
		t.Fatalf("report = %+v", report)
	}

	broken := newHarness(t)
	if err := os.WriteFile(broken.store.Path(), []byte(`{"version":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := broken.supervisor.Start(context.Background()); err == nil {
		t.Fatal("newer store accepted")
	}
	if broken.supervisor.Status().StoreError == "" {
		t.Fatal("store error not reported")
	}
}

func TestHostKeyChangeNeedsAttentionAndRepin(t *testing.T) {
	h := newHarness(t)
	mapped := h.mapDrive("Archive", "R")
	if err := h.supervisor.Disable(mapped.ID); err != nil {
		t.Fatal(err)
	}
	changed := newHostKey(t)
	h.prober.setHostKey(changed)
	if _, err := h.supervisor.Enable(context.Background(), mapped.ID); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("enable with a changed key = %v", err)
	}
	status := h.waitState(mapped.ID, Attention)
	if status.Trust == nil || status.Trust.Kind != TrustHostKeyChanged || status.Trust.Pinned != h.hostKey.Fingerprint || status.Trust.Presented != changed.Fingerprint {
		t.Fatalf("trust problem = %+v", status.Trust)
	}
	h.supervisor.NetworkChanged()
	time.Sleep(20 * time.Millisecond)
	if h.status(mapped.ID).State != Attention {
		t.Fatal("a network change must not retry an identity problem")
	}
	if _, err := h.supervisor.Repin(context.Background(), mapped.ID, "", strings.Repeat("a", 64)); err == nil {
		t.Fatal("certificate pin accepted for SFTP")
	}
	if _, err := h.supervisor.Repin(context.Background(), mapped.ID, "", ""); err == nil {
		t.Fatal("empty re-pin accepted")
	}
	if _, err := h.supervisor.Repin(context.Background(), mapped.ID, changed.Line, ""); err != nil {
		t.Fatal(err)
	}
	h.waitState(mapped.ID, Mounted)
	stored, _ := h.store.Find(mapped.ID)
	if stored.KnownHost != changed.Line {
		t.Fatal("new host key not stored")
	}
}

func TestUnpinnedSFTPIsNeverMounted(t *testing.T) {
	h := newHarness(t)
	endpoint := h.endpoint("Archive", "R")
	endpoint.KnownHost = ""
	if _, err := h.supervisor.Map(context.Background(), endpoint, "s"); !errors.Is(err, security.ErrHostKeyNotPinned) {
		t.Fatalf("unpinned map = %v", err)
	}
	if h.engine.mountCount() != 0 {
		t.Fatal("unpinned endpoint reached the engine")
	}
}

func TestFTPSCertificateTrust(t *testing.T) {
	h := newHarness(t)
	h.prober.certificate = security.Certificate{SHA256: strings.Repeat("ab", 32), TrustError: "self-signed"}
	endpoint := h.endpoint("Files", "F")
	endpoint.Protocol = config.FTPS
	endpoint.TLSMode = config.TLSExplicit
	endpoint.Port = 21
	endpoint.KnownHost = ""
	if _, err := h.supervisor.Map(context.Background(), endpoint, "s"); !errors.Is(err, security.ErrCertificateUntrusted) {
		t.Fatalf("untrusted = %v", err)
	}
	endpoint.CertificatePin = strings.Repeat("ab", 32)
	mapped, err := h.supervisor.Map(context.Background(), endpoint, "s")
	if err != nil {
		t.Fatal(err)
	}
	h.waitState(mapped.ID, Mounted)
	_ = h.supervisor.Disable(mapped.ID)
	h.prober.mu.Lock()
	h.prober.certificate.SHA256 = strings.Repeat("cd", 32)
	h.prober.mu.Unlock()
	_, _ = h.supervisor.Enable(context.Background(), mapped.ID)
	status := h.waitState(mapped.ID, Attention)
	if status.Trust == nil || status.Trust.Kind != TrustCertificateChanged || status.Trust.Presented != strings.Repeat("cd", 32) {
		t.Fatalf("certificate change = %+v", status.Trust)
	}
	if _, err := h.supervisor.Repin(context.Background(), mapped.ID, "x", ""); err == nil {
		t.Fatal("host key accepted for FTPS")
	}
	if _, err := h.supervisor.Repin(context.Background(), mapped.ID, "", strings.Repeat("cd", 32)); err != nil {
		t.Fatal(err)
	}
	h.waitState(mapped.ID, Mounted)

	trusted := h.endpoint("Public", "P")
	trusted.Protocol = config.FTPS
	trusted.Port = 990
	trusted.TLSMode = config.TLSImplicit
	trusted.KnownHost = ""
	h.prober.mu.Lock()
	h.prober.certificate = security.Certificate{SHA256: strings.Repeat("ef", 32), Trusted: true}
	h.prober.mu.Unlock()
	if _, err := h.supervisor.Map(context.Background(), trusted, "s"); err != nil {
		t.Fatalf("trusted certificate = %v", err)
	}

	plain := h.endpoint("Plain", "Q")
	plain.Protocol = config.FTP
	plain.Port = 21
	plain.KnownHost = ""
	mappedPlain, err := h.supervisor.Map(context.Background(), plain, "s")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.supervisor.Repin(context.Background(), mappedPlain.ID, "", strings.Repeat("ab", 32)); err == nil {
		t.Fatal("pin accepted for plain FTP")
	}
}

func TestMissingCredentialNeedsAttention(t *testing.T) {
	h := newHarness(t)
	mapped := h.mapDrive("Archive", "R")
	_ = h.supervisor.Disable(mapped.ID)
	_ = h.credentials.Delete(mapped.CredentialKey)
	if _, err := h.supervisor.Enable(context.Background(), mapped.ID); err == nil {
		t.Fatal("enable without a credential succeeded")
	}
	status := h.waitState(mapped.ID, Attention)
	if !strings.Contains(status.LastError, "credential") || status.Trust != nil {
		t.Fatalf("status = %+v", status)
	}
}

func TestEnableReportsRetryingAndStillConnecting(t *testing.T) {
	h := newHarness(t)
	mapped := h.mapDrive("Archive", "R")
	_ = h.supervisor.Disable(mapped.ID)
	h.setOptions(func(options *Options) { options.Backoff = Backoff{Initial: time.Hour, Max: time.Hour, Factor: 2} })
	h.engine.setFailures(mapped.ID, 1)
	if _, err := h.supervisor.Enable(context.Background(), mapped.ID); err == nil || !strings.Contains(err.Error(), "keeps retrying") {
		t.Fatalf("failed enable = %v", err)
	}
	h.supervisor.NetworkChanged()
	h.waitState(mapped.ID, Mounted)
	if message, err := h.supervisor.Enable(context.Background(), mapped.ID); err != nil || message != "" {
		t.Fatalf("enable of a mounted drive = %q, %v", message, err)
	}

	_ = h.supervisor.Disable(mapped.ID)
	h.engine.mu.Lock()
	h.engine.block = make(chan struct{})
	h.engine.mu.Unlock()
	h.setOptions(func(options *Options) { options.SettleTimeout = 20 * time.Millisecond })
	message, err := h.supervisor.Enable(context.Background(), mapped.ID)
	if err != nil || !strings.Contains(message, "still connecting") {
		t.Fatalf("slow enable = %q, %v", message, err)
	}
	h.engine.mu.Lock()
	close(h.engine.block)
	h.engine.block = nil
	h.engine.mu.Unlock()
	h.waitState(mapped.ID, Mounted)
}

func TestAttemptTimeoutUnmountsLateMount(t *testing.T) {
	h := newHarness(t)
	h.setOptions(func(options *Options) { options.AttemptTimeout = 30 * time.Millisecond })
	h.engine.block = make(chan struct{})
	if _, err := h.supervisor.Map(context.Background(), h.endpoint("Archive", "R"), "s"); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow map = %v", err)
	}
	h.engine.mu.Lock()
	close(h.engine.block)
	h.engine.block = nil
	h.engine.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for h.engine.mountCount() == 0 || h.engine.live() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("late mount was not unmounted")
		}
		time.Sleep(2 * time.Millisecond)
	}
	if endpoints, _ := h.store.Load(); len(endpoints) != 0 {
		t.Fatal("timed-out map saved a definition")
	}
}

func TestRenameRemountsMountedDrive(t *testing.T) {
	h := newHarness(t)
	mapped := h.mapDrive("Archive", "R")
	if _, err := h.supervisor.Rename(context.Background(), mapped.ID, `bad\name`); err == nil {
		t.Fatal("invalid name accepted")
	}
	other := h.mapDrive("Other", "S")
	if _, err := h.supervisor.Rename(context.Background(), mapped.ID, "OTHER"); err == nil {
		t.Fatal("duplicate name accepted")
	}
	before := h.engine.mountCount()
	if _, err := h.supervisor.Rename(context.Background(), mapped.ID, "Projects"); err != nil {
		t.Fatal(err)
	}
	h.waitState(mapped.ID, Mounted)
	if h.engine.mountCount() != before+1 {
		t.Fatalf("rename did not remount once: %d -> %d", before, h.engine.mountCount())
	}
	h.engine.mu.Lock()
	last := h.engine.mounts[len(h.engine.mounts)-1]
	h.engine.mu.Unlock()
	if last.Endpoint.Name != "Projects" {
		t.Fatalf("remounted with %q", last.Endpoint.Name)
	}
	if h.engine.live() != 2 {
		t.Fatalf("live mounts = %d", h.engine.live())
	}

	_ = h.supervisor.Disable(other.ID)
	count := h.engine.mountCount()
	if _, err := h.supervisor.Rename(context.Background(), other.ID, "Quiet"); err != nil {
		t.Fatal(err)
	}
	if h.engine.mountCount() != count || h.status(other.ID).State != Disabled {
		t.Fatal("renaming a disabled drive mounted it")
	}

	h.engine.setFailures(mapped.ID, 5)
	h.setOptions(func(options *Options) { options.Backoff = Backoff{Initial: time.Hour, Max: time.Hour, Factor: 2} })
	if _, err := h.supervisor.Rename(context.Background(), mapped.ID, "Broken"); err == nil || !strings.Contains(err.Error(), "renamed") {
		t.Fatalf("failed remount = %v", err)
	}
}

func TestUpdateSwapsOrRestores(t *testing.T) {
	h := newHarness(t)
	mapped := h.mapDrive("Archive", "R")
	edited := mapped
	edited.Port = 2222
	edited.DriveLetter = "S"
	updated, err := h.supervisor.Update(context.Background(), edited, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Port != 2222 || updated.DriveLetter != "S" {
		t.Fatalf("updated = %+v", updated)
	}
	h.waitState(mapped.ID, Mounted)
	if h.engine.live() != 1 {
		t.Fatalf("live mounts after update = %d", h.engine.live())
	}

	failing := updated
	failing.RemotePath = "/missing"
	h.engine.setFailures(mapped.ID, 1)
	newSecret := "other"
	if _, err := h.supervisor.Update(context.Background(), failing, &newSecret); err == nil {
		t.Fatal("failed update succeeded")
	}
	stored, _ := h.store.Find(mapped.ID)
	if stored.RemotePath != "" || stored.DriveLetter != "S" {
		t.Fatalf("failed update changed the stored definition: %+v", stored)
	}
	if _, password, _ := h.credentials.Get(mapped.CredentialKey); password != "secret" {
		t.Fatal("failed update changed the credential")
	}
	h.waitState(mapped.ID, Mounted)
	if h.engine.live() != 1 {
		t.Fatalf("old mount not restored: %d live", h.engine.live())
	}
	missing := edited
	missing.ID = "00000000-0000-4000-8000-000000000077"
	if _, err := h.supervisor.Update(context.Background(), missing, nil); err == nil {
		t.Fatal("update of an unknown drive succeeded")
	}
}

func TestRemoveCleansEverything(t *testing.T) {
	h := newHarness(t)
	keyFile := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyFile, []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	endpoint := h.endpoint("Archive", "R")
	endpoint.Auth = config.AuthKey
	endpoint.KeyFile = keyFile
	mapped, err := h.supervisor.Map(context.Background(), endpoint, "passphrase")
	if err != nil {
		t.Fatal(err)
	}
	layout := config.LayoutFor(h.store.Path())
	if mapped.KeyFile != layout.KeyFile(mapped.ID) {
		t.Fatalf("key not copied into the scope directory: %q", mapped.KeyFile)
	}
	if data, err := os.ReadFile(mapped.KeyFile); err != nil || !strings.Contains(string(data), "PRIVATE KEY") {
		t.Fatalf("copied key = %q, %v", data, err)
	}
	h.engine.mu.Lock()
	usedKey := h.engine.mounts[0].Endpoint.KeyFile
	h.engine.mu.Unlock()
	if !strings.HasSuffix(usedKey, ".new") {
		t.Fatalf("mount should use the staged key, used %q", usedKey)
	}
	for _, dir := range layout.CacheDirs(mapped.ID) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.supervisor.Remove(mapped.ID); err != nil {
		t.Fatal(err)
	}
	if h.engine.live() != 0 {
		t.Fatal("remove left the drive mounted")
	}
	for _, path := range append([]string{layout.KnownHostsFile(mapped.ID), layout.KeyFile(mapped.ID)}, layout.CacheDirs(mapped.ID)...) {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived remove", path)
		}
	}
	if h.credentials.has(mapped.CredentialKey) {
		t.Fatal("credential survived remove")
	}
	if err := h.supervisor.Remove(mapped.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("second remove = %v", err)
	}

	endpoint.KeyFile = filepath.Join(t.TempDir(), "missing")
	if _, err := h.supervisor.Map(context.Background(), endpoint, ""); err == nil {
		t.Fatal("missing key file accepted")
	}
}

func TestTestProbesWithoutSaving(t *testing.T) {
	h := newHarness(t)
	endpoint := h.endpoint("Archive", "R")
	secret := "probe-secret"
	if err := h.supervisor.Test(context.Background(), endpoint, &secret); err != nil {
		t.Fatal(err)
	}
	if endpoints, _ := h.store.Load(); len(endpoints) != 0 || h.engine.mountCount() != 0 {
		t.Fatal("test saved or mounted")
	}
	h.engine.mu.Lock()
	probed := h.engine.probes[0]
	h.engine.mu.Unlock()
	if probed.Secret.Password != secret {
		t.Fatal("probe did not use the supplied secret")
	}
	mapped := h.mapDrive("Archive", "R")
	if err := h.supervisor.TestStored(context.Background(), mapped.ID); err != nil {
		t.Fatal(err)
	}
	h.engine.mu.Lock()
	stored := h.engine.probes[1]
	h.engine.probeErr = errors.New("permission denied")
	h.engine.mu.Unlock()
	if stored.Secret.Password != "secret" {
		t.Fatal("stored test did not use the stored secret")
	}
	if err := h.supervisor.TestStored(context.Background(), mapped.ID); err == nil {
		t.Fatal("probe failure hidden")
	}
	if err := h.supervisor.TestStored(context.Background(), "00000000-0000-4000-8000-000000000055"); err == nil {
		t.Fatal("unknown drive tested")
	}
	if err := h.supervisor.Test(context.Background(), h.endpoint("", "R"), &secret); err == nil {
		t.Fatal("invalid definition tested")
	}
}

func TestResolveID(t *testing.T) {
	h := newHarness(t)
	mapped := h.mapDrive("Archive", "R")
	if id, err := h.supervisor.ResolveID("", `r:\`); err != nil || id != mapped.ID {
		t.Fatalf("resolve = %q, %v", id, err)
	}
	if id, _ := h.supervisor.ResolveID("given", "R"); id != "given" {
		t.Fatal("explicit id ignored")
	}
	if _, err := h.supervisor.ResolveID("", "Z"); err == nil || !strings.Contains(err.Error(), "not managed") {
		t.Fatalf("unknown drive = %v", err)
	}
	if endpoint, err := h.supervisor.EndpointByDrive("R"); err != nil || endpoint.ID != mapped.ID {
		t.Fatal("EndpointByDrive failed")
	}
	if _, err := h.supervisor.EndpointByDrive("Z"); err == nil {
		t.Fatal("unknown drive found")
	}
}

// TestConcurrentMountUnmountKeepsOneConsistentState hammers one endpoint
// with interleaved mount and unmount requests; the per-endpoint lock must
// leave no mount alive when the final request was an unmount.
func TestConcurrentMountUnmountKeepsOneConsistentState(t *testing.T) {
	h := newHarness(t)
	mapped := h.mapDrive("Archive", "R")
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := range 10 {
				if (worker+round)%2 == 0 {
					_, _ = h.supervisor.Enable(context.Background(), mapped.ID)
				} else {
					_ = h.supervisor.Disable(mapped.ID)
				}
			}
		}()
	}
	wg.Wait()
	if err := h.supervisor.Disable(mapped.ID); err != nil {
		t.Fatal(err)
	}
	if live := h.engine.live(); live != 0 {
		t.Fatalf("%d mounts alive after the final unmount", live)
	}
	stored, _ := h.store.Find(mapped.ID)
	if !stored.Disabled || h.status(mapped.ID).State != Disabled {
		t.Fatalf("final state: stored disabled=%v status=%s", stored.Disabled, h.status(mapped.ID).State)
	}
	if _, err := h.supervisor.Enable(context.Background(), mapped.ID); err != nil {
		t.Fatal(err)
	}
	if live := h.engine.live(); live != 1 {
		t.Fatalf("%d mounts alive after the final mount", live)
	}
}

func TestStopUnmountsEverything(t *testing.T) {
	h := newHarness(t)
	for index, letter := range []string{"R", "S", "T"} {
		h.mapDrive(fmt.Sprintf("Drive %d", index), letter)
	}
	h.supervisor.Stop()
	if live := h.engine.live(); live != 0 {
		t.Fatalf("%d mounts alive after Stop", live)
	}
	for _, status := range h.supervisor.Status().Endpoints {
		if status.State != Disabled {
			t.Fatalf("status after stop = %+v", status)
		}
	}
}

func TestNewSupervisorDefaults(t *testing.T) {
	supervisor := NewSupervisor(config.NewStore(filepath.Join(t.TempDir(), "e.json")), newMemoryCredentials(), newFakeEngine())
	if supervisor.options.Backoff.Initial != DefaultBackoff.Initial || supervisor.prober == nil || supervisor.log == nil {
		t.Fatal("defaults not applied")
	}
	id, err := NewID()
	if err != nil || config.ValidateID(id) != nil {
		t.Fatalf("NewID = %q, %v", id, err)
	}
}
