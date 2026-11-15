package ui

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/control"
	"mapftpdrive/internal/security"
)

func hostKey(t *testing.T) security.HostKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ssh.NewPublicKey(public)
	parsed, err := security.ParseKnownHost(knownhosts.Line([]string{"example.test"}, key))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

type scanner struct {
	key         security.HostKey
	certificate security.Certificate
	err         error
}

func (s scanner) SFTPHostKey(context.Context, config.Endpoint) (security.HostKey, error) {
	return s.key, s.err
}

func (s scanner) TLSCertificate(context.Context, config.Endpoint) (security.Certificate, error) {
	return s.certificate, s.err
}

// prompter answers every prompt with answer and records which prompts ran.
type prompter struct {
	answer bool
	asked  []string
}

func (p *prompter) ConfirmPlainFTP() bool {
	p.asked = append(p.asked, "ftp")
	return p.answer
}

func (p *prompter) ConfirmHostKey(string, security.HostKey) bool {
	p.asked = append(p.asked, "hostkey")
	return p.answer
}

func (p *prompter) ConfirmHostKeyChange(string, security.HostKey, security.HostKey) bool {
	p.asked = append(p.asked, "hostkey-change")
	return p.answer
}

func (p *prompter) ConfirmCertificate(string, security.Certificate) bool {
	p.asked = append(p.asked, "certificate")
	return p.answer
}

func (p *prompter) ConfirmCertificateChange(string, string, security.Certificate) bool {
	p.asked = append(p.asked, "certificate-change")
	return p.answer
}

type host struct {
	requests []control.Request
	response control.Response
	err      error
}

func (h *host) Call(_ context.Context, request control.Request) (control.Response, error) {
	h.requests = append(h.requests, request)
	return h.response, h.err
}

func sftpEndpoint() config.Endpoint {
	return config.Endpoint{Name: "Archive", Protocol: config.SFTP, Host: "example.test", Port: 22, Username: "u", DriveLetter: "R", Scope: config.User}
}

func TestPinIdentitySFTP(t *testing.T) {
	key := hostKey(t)
	ctx := context.Background()

	ask := &prompter{answer: true}
	pinned, err := PinIdentity(ctx, sftpEndpoint(), scanner{key: key}, ask)
	if err != nil || pinned.KnownHost != key.Line || strings.Join(ask.asked, ",") != "hostkey" {
		t.Fatalf("first use = %+v, %v, %v", pinned, err, ask.asked)
	}

	unchanged := &prompter{}
	if again, err := PinIdentity(ctx, pinned, scanner{key: key}, unchanged); err != nil || again.KnownHost != key.Line || len(unchanged.asked) != 0 {
		t.Fatalf("unchanged key asked %v, %v", unchanged.asked, err)
	}

	other := hostKey(t)
	refuse := &prompter{answer: false}
	if _, err := PinIdentity(ctx, pinned, scanner{key: other}, refuse); !errors.Is(err, ErrCancelled) || refuse.asked[0] != "hostkey-change" {
		t.Fatalf("changed key refused = %v, %v", err, refuse.asked)
	}
	accept := &prompter{answer: true}
	if changed, err := PinIdentity(ctx, pinned, scanner{key: other}, accept); err != nil || changed.KnownHost != other.Line {
		t.Fatalf("changed key accepted = %+v, %v", changed, err)
	}

	if _, err := PinIdentity(ctx, sftpEndpoint(), scanner{key: key}, &prompter{}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("declined first use = %v", err)
	}
	if _, err := PinIdentity(ctx, sftpEndpoint(), scanner{err: errors.New("timeout")}, &prompter{answer: true}); err == nil || !strings.Contains(err.Error(), "host key") {
		t.Fatalf("scan failure = %v", err)
	}
	garbage := sftpEndpoint()
	garbage.KnownHost = "garbage"
	repaired := &prompter{answer: true}
	if fixed, err := PinIdentity(ctx, garbage, scanner{key: key}, repaired); err != nil || fixed.KnownHost != key.Line || repaired.asked[0] != "hostkey" {
		t.Fatalf("unparseable pin = %+v, %v, %v", fixed, err, repaired.asked)
	}
}

func TestPinIdentityFTPS(t *testing.T) {
	ctx := context.Background()
	endpoint := sftpEndpoint()
	endpoint.Protocol = config.FTPS
	endpoint.TLSMode = config.TLSExplicit
	endpoint.Port = 21
	untrusted := security.Certificate{SHA256: strings.Repeat("ab", 32)}
	trusted := security.Certificate{SHA256: strings.Repeat("cd", 32), Trusted: true}

	silent := &prompter{}
	if result, err := PinIdentity(ctx, endpoint, scanner{certificate: trusted}, silent); err != nil || result.CertificatePin != "" || len(silent.asked) != 0 {
		t.Fatalf("trusted = %+v, %v, %v", result, err, silent.asked)
	}
	ask := &prompter{answer: true}
	pinned, err := PinIdentity(ctx, endpoint, scanner{certificate: untrusted}, ask)
	if err != nil || pinned.CertificatePin != untrusted.SHA256 || ask.asked[0] != "certificate" {
		t.Fatalf("untrusted = %+v, %v, %v", pinned, err, ask.asked)
	}
	if _, err := PinIdentity(ctx, endpoint, scanner{certificate: untrusted}, &prompter{}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("declined certificate = %v", err)
	}
	same := &prompter{}
	if _, err := PinIdentity(ctx, pinned, scanner{certificate: untrusted}, same); err != nil || len(same.asked) != 0 {
		t.Fatalf("unchanged certificate asked %v", same.asked)
	}
	renewed := security.Certificate{SHA256: strings.Repeat("ef", 32)}
	change := &prompter{answer: true}
	if result, err := PinIdentity(ctx, pinned, scanner{certificate: renewed}, change); err != nil || result.CertificatePin != renewed.SHA256 || change.asked[0] != "certificate-change" {
		t.Fatalf("changed certificate = %+v, %v", result, err)
	}
	if result, _ := PinIdentity(ctx, pinned, scanner{certificate: trusted}, &prompter{answer: true}); result.CertificatePin != "" {
		t.Fatal("a now-trusted certificate should drop the pin")
	}
	if _, err := PinIdentity(ctx, pinned, scanner{certificate: renewed}, &prompter{}); !errors.Is(err, ErrCancelled) {
		t.Fatal("declined change accepted")
	}
	if _, err := PinIdentity(ctx, endpoint, scanner{err: errors.New("refused")}, &prompter{answer: true}); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("scan failure = %v", err)
	}

	plain := sftpEndpoint()
	plain.Protocol = config.FTP
	if _, err := PinIdentity(ctx, plain, nil, &prompter{}); !errors.Is(err, ErrCancelled) {
		t.Fatal("plain FTP accepted without confirmation")
	}
	if _, err := PinIdentity(ctx, plain, nil, &prompter{answer: true}); err != nil {
		t.Fatal(err)
	}
}

func TestCheckLetter(t *testing.T) {
	endpoint := sftpEndpoint()
	usage := LetterUsage{
		Mask: 1<<('C'-'A') | 1<<('R'-'A'),
		Managed: []config.DriveIndexEntry{
			{Drive: "S", Scope: config.System, ID: "sys", Name: "Shared"},
			{Drive: "T", Scope: config.User, ID: "old", Name: "Old", Disabled: true},
			{Drive: "U", Scope: config.User, ID: "live", Name: "Live"},
			{Drive: "R", Scope: config.User, ID: "editing", Name: "Mine"},
		},
	}
	check := func(drive, editing string) error {
		candidate := endpoint
		candidate.DriveLetter = drive
		return CheckLetter(candidate, editing, usage)
	}
	if err := check("C", ""); err == nil || !strings.Contains(err.Error(), "in use on this computer") {
		t.Fatalf("C: = %v", err)
	}
	if err := check("S", ""); err == nil || !strings.Contains(err.Error(), "system-wide") {
		t.Fatalf("system drive = %v", err)
	}
	if err := check("U", ""); err == nil || !strings.Contains(err.Error(), "your drive") {
		t.Fatalf("user drive = %v", err)
	}
	if err := check("T", ""); err != nil {
		t.Fatalf("disabled same-scope drive should be replaceable: %v", err)
	}
	if err := check("T", "editing"); err == nil {
		t.Fatal("editing onto another disabled drive accepted")
	}
	if err := check("R", "editing"); err != nil {
		t.Fatalf("editing keeps its own mounted letter: %v", err)
	}
	if err := check("R", ""); err == nil {
		t.Fatal("R: is in use")
	}
	if err := check("V", ""); err != nil {
		t.Fatal(err)
	}
	if err := check("1", ""); err == nil {
		t.Fatal("invalid letter accepted")
	}
	letters := AvailableLetters(config.User, "R", usage)
	joined := strings.Join(letters, " ")
	for _, want := range []string{"D:", "R:", "T:", "Z:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("available letters %q lack %s", joined, want)
		}
	}
	for _, unwanted := range []string{"C:", "S:", "U:", "A:"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("available letters %q include %s", joined, unwanted)
		}
	}
}

func TestDescribeAndRows(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	endpoint := sftpEndpoint()
	endpoint.Port = 2222
	endpoint.RemotePath = "/srv"
	cases := map[string]control.EndpointStatus{
		"Mounted":                      {State: control.StateMounted},
		"Unmounted":                    {State: control.StateDisabled},
		"Connecting…":                  {State: control.StateConnecting},
		"Connecting (waiting)":         {State: control.StateConnecting, LastError: "waiting"},
		"Retrying in 42s: refused":     {State: control.StateRetrying, LastError: "refused", RetryAt: now.Add(42 * time.Second)},
		"Retrying in 0s: refused":      {State: control.StateRetrying, LastError: "refused", RetryAt: now.Add(-time.Second)},
		"Needs attention: key changed": {State: control.StateAttention, LastError: "key changed"},
		"strange":                      {State: "strange"},
	}
	for want, status := range cases {
		if got := DescribeState(status, now); got != want {
			t.Errorf("DescribeState = %q, want %q", got, want)
		}
	}
	for want, e := range map[string]config.Endpoint{
		"SFTP":              {Protocol: config.SFTP},
		"FTPS (explicit)":   {Protocol: config.FTPS, TLSMode: config.TLSExplicit},
		"FTPS (implicit)":   {Protocol: config.FTPS, TLSMode: config.TLSImplicit},
		"FTP (unencrypted)": {Protocol: config.FTP},
	} {
		if got := DescribeProtocol(e); got != want {
			t.Errorf("DescribeProtocol = %q, want %q", got, want)
		}
	}
	systemEndpoint := sftpEndpoint()
	systemEndpoint.DriveLetter = "A"
	reports := []*control.StatusReport{
		{Scope: config.System, Endpoints: []control.EndpointStatus{{Endpoint: systemEndpoint, State: control.StateMounted}}, Problems: []config.Problem{{Index: 1}}},
		nil,
		{Scope: config.User, Endpoints: []control.EndpointStatus{{Endpoint: endpoint, State: control.StateMounted, Trust: &control.TrustProblem{Kind: "x"}}}, StoreError: "unreadable"},
	}
	rows := Rows(reports, now)
	if len(rows) != 2 || rows[0].Scope != config.User || rows[0].Server != "example.test:2222/srv" || rows[0].Problem == nil || rows[1].Server != "example.test" {
		t.Fatalf("rows = %+v", rows)
	}
	problems := ReportProblems(reports)
	if !strings.Contains(problems, "unreadable") || !strings.Contains(problems, "1 invalid system") {
		t.Fatalf("problems = %q", problems)
	}
	usage := UsageFromReports(7, reports)
	if usage.Mask != 7 || len(usage.Managed) != 2 || usage.Managed[0].Scope != config.System {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestSubmitMapAndRepin(t *testing.T) {
	ctx := context.Background()
	key := hostKey(t)
	h := &host{response: control.Response{Endpoint: &config.Endpoint{ID: "assigned"}}}
	result := MapperResult{Endpoint: sftpEndpoint(), Password: "pw"}
	mapped, err := SubmitMap(ctx, h, scanner{key: key}, &prompter{answer: true}, result, nil)
	if err != nil || mapped.ID != "assigned" {
		t.Fatalf("map = %+v, %v", mapped, err)
	}
	request := h.requests[0]
	if request.Operation != control.Map || request.Password != "pw" || request.Endpoint.KnownHost != key.Line {
		t.Fatalf("map request = %+v", request)
	}

	stored := sftpEndpoint()
	stored.ID = "stored-id"
	stored.CredentialKey = "endpoint/stored-id"
	stored.KnownHost = key.Line
	edit := MapperResult{Endpoint: sftpEndpoint()}
	edit.Endpoint.Name = "Renamed"
	silent := &prompter{}
	h.response = control.Response{}
	if _, err := SubmitMap(ctx, h, scanner{key: key}, silent, edit, &stored); err != nil {
		t.Fatal(err)
	}
	update := h.requests[1]
	if update.Operation != control.Update || update.Endpoint.ID != "stored-id" || update.Endpoint.KnownHost != key.Line || len(silent.asked) != 0 {
		t.Fatalf("update request = %+v, asked %v", update, silent.asked)
	}
	moved := edit
	moved.Endpoint.Host = "new.example"
	ask := &prompter{answer: true}
	if _, err := SubmitMap(ctx, h, scanner{key: key}, ask, moved, &stored); err != nil || ask.asked[0] != "hostkey" {
		t.Fatalf("moved server must be pinned afresh: %v %v", err, ask.asked)
	}
	if _, err := SubmitMap(ctx, h, scanner{key: key}, &prompter{}, result, nil); !errors.Is(err, ErrCancelled) {
		t.Fatal("declined pin submitted")
	}
	h.err = errors.New("host down")
	if _, err := SubmitMap(ctx, h, scanner{key: key}, &prompter{answer: true}, result, nil); err == nil {
		t.Fatal("host error hidden")
	}
	h.err = nil

	other := hostKey(t)
	h.response = control.Response{Message: "still connecting"}
	if message, err := Repin(ctx, h, scanner{key: other}, &prompter{answer: true}, stored); err != nil || message != "still connecting" {
		t.Fatalf("repin = %q, %v", message, err)
	}
	last := h.requests[len(h.requests)-1]
	if last.Operation != control.Repin || last.KnownHost != other.Line || last.ID != "stored-id" {
		t.Fatalf("repin request = %+v", last)
	}
	unpinned := stored
	unpinned.KnownHost = ""
	first := &prompter{answer: true}
	if _, err := Repin(ctx, h, scanner{key: other}, first, unpinned); err != nil || first.asked[0] != "hostkey" {
		t.Fatalf("first pin = %v %v", err, first.asked)
	}
	if _, err := Repin(ctx, h, scanner{key: other}, &prompter{}, stored); !errors.Is(err, ErrCancelled) {
		t.Fatal("declined repin sent")
	}
	if _, err := Repin(ctx, h, scanner{err: errors.New("x")}, &prompter{answer: true}, stored); err == nil {
		t.Fatal("scan failure hidden")
	}

	ftps := stored
	ftps.Protocol = config.FTPS
	ftps.KnownHost = ""
	ftps.CertificatePin = strings.Repeat("ab", 32)
	certificate := security.Certificate{SHA256: strings.Repeat("cd", 32)}
	change := &prompter{answer: true}
	if _, err := Repin(ctx, h, scanner{certificate: certificate}, change, ftps); err != nil || change.asked[0] != "certificate-change" {
		t.Fatalf("certificate repin = %v %v", err, change.asked)
	}
	if h.requests[len(h.requests)-1].CertificatePin != certificate.SHA256 {
		t.Fatal("certificate digest not sent")
	}
	ftps.CertificatePin = ""
	firstCertificate := &prompter{answer: true}
	if _, err := Repin(ctx, h, scanner{certificate: certificate}, firstCertificate, ftps); err != nil || firstCertificate.asked[0] != "certificate" {
		t.Fatalf("first certificate pin = %v %v", err, firstCertificate.asked)
	}
	if _, err := Repin(ctx, h, scanner{err: errors.New("x")}, &prompter{answer: true}, ftps); err == nil {
		t.Fatal("certificate scan failure hidden")
	}
	if _, err := Repin(ctx, h, scanner{certificate: certificate}, &prompter{}, ftps); !errors.Is(err, ErrCancelled) {
		t.Fatal("declined certificate sent")
	}
	plain := stored
	plain.Protocol = config.FTP
	if _, err := Repin(ctx, h, nil, &prompter{answer: true}, plain); err == nil {
		t.Fatal("plain FTP re-pinned")
	}
}

func TestFormLogic(t *testing.T) {
	for index := range ProtocolChoices {
		protocol, mode := ProtocolAt(index)
		if ProtocolIndex(config.Endpoint{Protocol: protocol, TLSMode: mode}) != index {
			t.Errorf("protocol index %d does not round-trip", index)
		}
	}
	if DefaultPortText(0) != "22" || DefaultPortText(1) != "21" || DefaultPortText(2) != "990" || DefaultPortText(3) != "21" {
		t.Fatal("default ports")
	}
	if NextPortText("22", 0, 2) != "990" || NextPortText("", 2, 1) != "21" || NextPortText("2121", 0, 1) != "2121" {
		t.Fatal("port follows protocol incorrectly")
	}

	values := FormValues{Scope: config.User, Name: " Archive ", ProtocolIndex: 0, Host: "example.test", Port: "22", Username: "u", Password: "pw", Drive: "r:"}
	result, err := BuildResult(values)
	if err != nil || result.Endpoint.Name != "Archive" || result.Endpoint.DriveLetter != "R" || result.Password != "pw" || result.Endpoint.ID != "" {
		t.Fatalf("build = %+v, %v", result, err)
	}
	for name, mutate := range map[string]func(*FormValues){
		"port":      func(v *FormValues) { v.Port = "0" },
		"port text": func(v *FormValues) { v.Port = "ssh" },
		"name":      func(v *FormValues) { v.Name = "a/b" },
		"host":      func(v *FormValues) { v.Host = "" },
		"key ftp":   func(v *FormValues) { v.ProtocolIndex = 3; v.UseKey = true; v.KeyFile = `C:\k` },
		"key path":  func(v *FormValues) { v.UseKey = true; v.KeyFile = "relative" },
		"drive":     func(v *FormValues) { v.Drive = "" },
	} {
		candidate := values
		mutate(&candidate)
		if _, err := BuildResult(candidate); err == nil {
			t.Errorf("%s: invalid form accepted", name)
		}
	}
	keyed := values
	keyed.UseKey = true
	keyed.KeyFile = `C:\Users\me\.ssh\id_ed25519`
	if result, err := BuildResult(keyed); err != nil || result.Endpoint.Auth != config.AuthKey {
		t.Fatalf("key form = %+v, %v", result, err)
	}
	stored := result.Endpoint
	stored.Port = 2222
	form := FormFor(stored)
	if form.Port != "2222" || form.Drive != "R:" || form.Name != "Archive" || form.ProtocolIndex != 0 {
		t.Fatalf("form = %+v", form)
	}
	if message := RemovalMessage("Archive", `r:\`); !strings.Contains(message, "Archive (R:)") || !strings.Contains(message, "cache") {
		t.Fatalf("removal message = %q", message)
	}
}

func TestPromptTextsShowFingerprints(t *testing.T) {
	old, presented := hostKey(t), hostKey(t)
	if text := hostKeyPrompt("h", old); !strings.Contains(text, old.Fingerprint) || !strings.Contains(text, old.Algorithm) {
		t.Fatal(text)
	}
	if text := hostKeyChangePrompt("h", old, presented); !strings.Contains(text, old.Fingerprint) || !strings.Contains(text, presented.Fingerprint) {
		t.Fatal(text)
	}
	certificate := security.Certificate{SHA256: strings.Repeat("0a", 32), Subject: "CN=s", Issuer: "CN=i"}
	if text := certificatePrompt("h", certificate); !strings.Contains(text, "0A:0A") || !strings.Contains(text, "authority") {
		t.Fatal(text)
	}
	if text := certificateChangePrompt("h", strings.Repeat("ff", 32), certificate); !strings.Contains(text, "FF:FF") || !strings.Contains(text, "0A:0A") {
		t.Fatal(text)
	}
	if !strings.Contains(plainFTPPrompt, "without encryption") {
		t.Fatal(plainFTPPrompt)
	}
}

func TestShownErrorsAreRemembered(t *testing.T) {
	err := errors.New("drive Z: is not managed")
	if WasShown(err) {
		t.Fatal("unshown error reported as shown")
	}
	ShowError("MapFTPDrive", err)
	if !WasShown(err) || !WasShown(errors.New("MapFTPDrive host: drive Z: is not managed")) {
		t.Fatal("shown error not remembered")
	}
	if WasShown(nil) || WasShown(errors.New("other")) {
		t.Fatal("unrelated error reported as shown")
	}
}

func TestFetchReports(t *testing.T) {
	user := &host{response: control.Response{Status: &control.StatusReport{Endpoints: []control.EndpointStatus{{State: control.StateMounted}}}}}
	system := &host{err: errors.New("access denied")}
	reports := fetchReports(context.Background(), map[config.Scope]Host{config.User: user, config.System: system})
	if len(reports) != 2 || reports[0].Scope != config.User || len(reports[0].Endpoints) != 1 || !strings.Contains(reports[1].StoreError, "access denied") {
		t.Fatalf("reports = %+v", reports)
	}
	if user.requests[0].Operation != control.Status {
		t.Fatal("status not requested")
	}
	if _, err := ShowMapper(config.User); err == nil {
		t.Skip("native interface available")
	}
	if ShowManager(ManagerOptions{}) == nil {
		t.Fatal("manager reported success without a window")
	}
}
