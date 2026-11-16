package host

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"mapftpdrive/internal/app"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
	"mapftpdrive/internal/credentials"
	"mapftpdrive/internal/mount"
	"mapftpdrive/internal/security"
)

// fakeTransport serves in memory and round-trips every request and reply
// through JSON, as the pipe does.
type fakeTransport struct {
	mu       sync.Mutex
	handler  control.Handler
	serveErr error
	otherUp  bool
	gate     chan struct{}
}

func (f *fakeTransport) ServeReady(ctx context.Context, _ config.Scope, handler control.Handler, ready func()) error {
	if f.serveErr != nil {
		return f.serveErr
	}
	f.mu.Lock()
	f.handler = handler
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil
		}
	}
	ready()
	<-ctx.Done()
	return nil
}

func (f *fakeTransport) Call(ctx context.Context, _ config.Scope, request control.Request) (control.Response, error) {
	if f.serveErr != nil {
		if f.otherUp {
			return control.Response{}, nil
		}
		return control.Response{}, errors.New("pipe not found")
	}
	f.mu.Lock()
	handler := f.handler
	f.mu.Unlock()
	if handler == nil {
		return control.Response{}, errors.New("pipe not found")
	}
	var decoded control.Request
	data, _ := json.Marshal(request)
	if err := json.Unmarshal(data, &decoded); err != nil {
		return control.Response{}, err
	}
	response, err := handler(ctx, decoded)
	if err != nil {
		response.Error = err.Error()
	}
	data, _ = json.Marshal(response)
	var reply control.Response
	if err := json.Unmarshal(data, &reply); err != nil {
		return control.Response{}, err
	}
	if reply.Error != "" {
		return reply, errors.New(reply.Error)
	}
	return reply, nil
}

type memoryCredentials struct {
	mu     sync.Mutex
	values map[string][2]string
}

func (m *memoryCredentials) Put(key, username, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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

type handle struct {
	once  sync.Once
	ended chan struct{}
}

func (h *handle) Wait() error { <-h.ended; return nil }
func (h *handle) Unmount() error {
	h.once.Do(func() { close(h.ended) })
	return nil
}

type engine struct {
	mu       sync.Mutex
	mounts   []mount.Spec
	handles  []*handle
	failures int
	probes   int
}

func (e *engine) Mount(_ context.Context, spec mount.Spec) (mount.Handle, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.mounts = append(e.mounts, spec)
	if e.failures > 0 {
		e.failures--
		return nil, errors.New("unreachable")
	}
	h := &handle{ended: make(chan struct{})}
	e.handles = append(e.handles, h)
	return h, nil
}

func (e *engine) Probe(context.Context, mount.Spec) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.probes++
	return nil
}

func (e *engine) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.mounts)
}

func (e *engine) live() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	live := 0
	for _, h := range e.handles {
		select {
		case <-h.ended:
		default:
			live++
		}
	}
	return live
}

type prober struct{ key security.HostKey }

func (p prober) SFTPHostKey(context.Context, config.Endpoint) (security.HostKey, error) {
	return p.key, nil
}

func (p prober) TLSCertificate(context.Context, config.Endpoint) (security.Certificate, error) {
	return security.Certificate{Trusted: true}, nil
}

type watcher struct{ changes chan struct{} }

func (w watcher) Changes() <-chan struct{} { return w.changes }
func (w watcher) Close() error             { return nil }

type fixture struct {
	t         *testing.T
	deps      Deps
	transport *fakeTransport
	engine    *engine
	key       security.HostKey
	cancel    context.CancelFunc
	done      chan error
}

func newFixture(t *testing.T, scope config.Scope) *fixture {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshKey, _ := ssh.NewPublicKey(public)
	key, err := security.ParseKnownHost(knownhosts.Line([]string{"example.test"}, sshKey))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, transport: &fakeTransport{}, engine: &engine{}, key: key}
	f.deps = Deps{
		Scope:       scope,
		StorePath:   filepath.Join(t.TempDir(), "endpoints.json"),
		Transport:   f.transport,
		Credentials: &memoryCredentials{values: make(map[string][2]string)},
		Engine:      f.engine,
		Prober:      prober{key: key},
		Backoff:     app.Backoff{Initial: time.Hour, Max: time.Hour, Factor: 2},
	}
	return f
}

func (f *fixture) start() {
	f.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	f.done = make(chan error, 1)
	go func() { f.done <- RunWith(ctx, f.deps) }()
	f.t.Cleanup(f.stop)
	f.eventually("host ready", func() bool {
		_, err := f.call(control.Request{Operation: control.Ping})
		return err == nil
	})
}

func (f *fixture) stop() {
	if f.cancel == nil {
		return
	}
	f.cancel()
	f.cancel = nil
	select {
	case err := <-f.done:
		if err != nil {
			f.t.Errorf("host returned %v", err)
		}
	case <-time.After(10 * time.Second):
		f.t.Error("host did not stop")
	}
}

func (f *fixture) call(request control.Request) (control.Response, error) {
	return f.transport.Call(context.Background(), f.deps.Scope, request)
}

func (f *fixture) eventually(what string, condition func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			f.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (f *fixture) endpoint(name, drive string) *config.Endpoint {
	return &config.Endpoint{
		Name:        name,
		Protocol:    config.SFTP,
		Host:        "example.test",
		Port:        22,
		Username:    "user",
		DriveLetter: drive,
		Scope:       f.deps.Scope,
		KnownHost:   f.key.Line,
	}
}

func (f *fixture) status() *control.StatusReport {
	f.t.Helper()
	response, err := f.call(control.Request{Operation: control.Status})
	if err != nil {
		f.t.Fatal(err)
	}
	return response.Report
}

func (f *fixture) state(drive string) control.State {
	for _, entry := range f.status().Endpoints {
		if entry.Endpoint.DriveLetter == drive {
			return entry.State
		}
	}
	return ""
}

func (f *fixture) seed(endpoint config.Endpoint) {
	f.t.Helper()
	endpoint.ID = "00000000-0000-4000-8000-000000000001"
	endpoint.CredentialKey = config.CredentialKeyFor(endpoint.ID)
	if err := config.NewStore(f.deps.StorePath).Upsert(endpoint); err != nil {
		f.t.Fatal(err)
	}
	if err := f.deps.Credentials.Put(endpoint.CredentialKey, endpoint.Username, "secret"); err != nil {
		f.t.Fatal(err)
	}
}

func TestSecondHostExitsWithoutMounting(t *testing.T) {
	f := newFixture(t, config.User)
	f.seed(*f.endpoint("Archive", "R"))
	f.transport.serveErr = errors.New("access denied: pipe exists")
	f.transport.otherUp = true
	if err := RunWith(context.Background(), f.deps); err != nil {
		t.Fatalf("second host = %v", err)
	}
	if f.engine.count() != 0 {
		t.Fatal("second host mounted")
	}

	f.transport.otherUp = false
	if err := RunWith(context.Background(), f.deps); err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("serve failure without another host = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RunWith(ctx, f.deps); err != nil {
		t.Fatalf("cancelled start = %v", err)
	}
}

func TestHostTakesPipeBeforeMounting(t *testing.T) {
	f := newFixture(t, config.User)
	f.seed(*f.endpoint("Archive", "R"))
	f.transport.gate = make(chan struct{})
	networkGate := make(chan struct{})
	f.deps.WaitForNetwork = func(ctx context.Context) bool {
		select {
		case <-networkGate:
			return true
		case <-ctx.Done():
			return false
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunWith(ctx, f.deps) }()
	time.Sleep(30 * time.Millisecond)
	if f.engine.count() != 0 {
		t.Fatal("mounted before owning the pipe")
	}
	close(f.transport.gate)
	f.eventually("pipe ready", func() bool {
		_, err := f.call(control.Request{Operation: control.Ping})
		return err == nil
	})
	report := f.status()
	if len(report.Endpoints) != 1 || report.Endpoints[0].State != control.StateConnecting || report.Endpoints[0].LastError != "waiting for the network" {
		t.Fatalf("status while waiting for the network = %+v", report)
	}
	if f.engine.count() != 0 {
		t.Fatal("mounted before the network was ready")
	}
	close(networkGate)
	f.eventually("mounted", func() bool { return f.state("R") == control.StateMounted })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.engine.live() != 0 {
		t.Fatal("host stopped with drives mounted")
	}
}

func TestNetworkChangeIsForwarded(t *testing.T) {
	f := newFixture(t, config.User)
	f.seed(*f.endpoint("Archive", "R"))
	f.engine.failures = 1
	changes := make(chan struct{}, 1)
	f.deps.Network = watcher{changes: changes}
	f.start()
	f.eventually("retrying", func() bool { return f.state("R") == control.StateRetrying })
	changes <- struct{}{}
	f.eventually("mounted after network change", func() bool { return f.state("R") == control.StateMounted })
}

func TestDispatchAndSystemIndex(t *testing.T) {
	f := newFixture(t, config.System)
	f.start()
	index := config.DriveIndexPath(f.deps.StorePath)
	if entries, err := config.ReadDriveIndex(index); err != nil || len(entries) != 0 {
		t.Fatalf("index at start = %+v, %v", entries, err)
	}

	if _, err := f.call(control.Request{Operation: control.Map}); err == nil {
		t.Fatal("map without an endpoint accepted")
	}
	wrongScope := f.endpoint("Archive", "R")
	wrongScope.Scope = config.User
	if _, err := f.call(control.Request{Operation: control.Map, Endpoint: wrongScope, Password: "s"}); err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("scope mismatch = %v", err)
	}
	response, err := f.call(control.Request{Operation: control.SaveAndMount, Endpoint: f.endpoint("Archive", "R"), Password: "s"})
	if err != nil {
		t.Fatal(err)
	}
	mapped := response.Endpoint
	if mapped == nil || config.ValidateID(mapped.ID) != nil {
		t.Fatalf("map response = %+v", response)
	}
	entries, err := config.ReadDriveIndex(index)
	if err != nil || len(entries) != 1 || entries[0].Drive != "R" || entries[0].Scope != config.System || entries[0].ID != mapped.ID {
		t.Fatalf("index after map = %+v, %v", entries, err)
	}
	f.eventually("mounted", func() bool { return f.state("R") == control.StateMounted })

	if response, err := f.call(control.Request{Operation: control.Test, Drive: "R"}); err != nil || response.Message == "" {
		t.Fatalf("stored test = %+v, %v", response, err)
	}
	if _, err := f.call(control.Request{Operation: control.Test, Endpoint: f.endpoint("Probe", "Q"), Password: "x"}); err != nil {
		t.Fatalf("definition test = %v", err)
	}
	if _, err := f.call(control.Request{Operation: control.Test, Endpoint: f.endpoint("Probe", "Q")}); err != nil {
		t.Fatalf("definition test without a password = %v", err)
	}

	if _, err := f.call(control.Request{Operation: control.Rename, Drive: `R:\`, Name: "Projects"}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := config.ReadDriveIndex(index); entries[0].Name != "Projects" {
		t.Fatal("index not refreshed after rename")
	}
	if _, err := f.call(control.Request{Operation: control.Unmount, Drive: "R"}); err != nil {
		t.Fatal(err)
	}
	if f.state("R") != control.StateDisabled {
		t.Fatal("unmount did not disable")
	}
	if entries, _ := config.ReadDriveIndex(index); !entries[0].Disabled {
		t.Fatal("index does not show the disabled drive")
	}
	if _, err := f.call(control.Request{Operation: control.Mount, ID: mapped.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.call(control.Request{Operation: control.Repin, Drive: "R", KnownHost: f.key.Line}); err != nil {
		t.Fatal(err)
	}

	edited := *mapped
	edited.Port = 2222
	edited.Name = "Projects"
	if _, err := f.call(control.Request{Operation: control.Update, Endpoint: &edited}); err != nil {
		t.Fatal(err)
	}
	noID := edited
	noID.ID = ""
	if _, err := f.call(control.Request{Operation: control.Update, Endpoint: &noID}); err == nil {
		t.Fatal("update without an id accepted")
	}
	if _, err := f.call(control.Request{Operation: control.Update}); err == nil {
		t.Fatal("update without an endpoint accepted")
	}
	f.eventually("mounted after update", func() bool { return f.state("R") == control.StateMounted })
	for _, entry := range f.status().Endpoints {
		if entry.Endpoint.Port != 2222 {
			t.Fatalf("update not applied: %+v", entry.Endpoint)
		}
	}

	if _, err := f.call(control.Request{Operation: "format-disk", Drive: "R"}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unknown operation = %v", err)
	}
	if _, err := f.call(control.Request{Operation: control.Mount, Drive: "Z"}); err == nil || !strings.Contains(err.Error(), "not managed") {
		t.Fatalf("unknown drive = %v", err)
	}
	if _, err := f.call(control.Request{Operation: control.Test, Drive: "Z"}); err == nil {
		t.Fatal("test of an unknown drive accepted")
	}
	if _, err := f.call(control.Request{Operation: control.Remove, Drive: "R"}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := config.ReadDriveIndex(index); len(entries) != 0 {
		t.Fatal("index still lists the removed drive")
	}
	if len(f.status().Endpoints) != 0 || f.engine.live() != 0 {
		t.Fatal("remove left state behind")
	}
}

func TestStatusReportsTrustProblems(t *testing.T) {
	f := newFixture(t, config.User)
	other := f.endpoint("Archive", "R")
	public, _, _ := ed25519.GenerateKey(rand.Reader)
	sshKey, _ := ssh.NewPublicKey(public)
	other.KnownHost = knownhosts.Line([]string{"example.test"}, sshKey)
	f.seed(*other)
	f.start()
	f.eventually("attention", func() bool { return f.state("R") == control.StateAttention })
	entry := f.status().Endpoints[0]
	if entry.Trust == nil || entry.Trust.Kind != control.TrustHostKeyChanged || entry.Trust.Presented != f.key.Fingerprint {
		t.Fatalf("trust = %+v", entry.Trust)
	}
}

func TestUnreadableStoreStillServes(t *testing.T) {
	f := newFixture(t, config.System)
	if err := config.NewStore(f.deps.StorePath).Save(nil); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(f.deps.StorePath, `{"version":42}`); err != nil {
		t.Fatal(err)
	}
	f.start()
	if report := f.status(); report.StoreError == "" {
		t.Fatalf("store error not reported: %+v", report)
	}
}

// The handler must answer before the pipe server cancels it and before the
// client, whose RequestTimeout starts earlier, stops waiting.
func TestRequestBudgetEndsBeforeTheExchange(t *testing.T) {
	if requestBudget <= 0 || control.RequestTimeout-requestBudget < 10*time.Second {
		t.Fatalf("requestBudget %s leaves too little of RequestTimeout %s for the reply", requestBudget, control.RequestTimeout)
	}
}
