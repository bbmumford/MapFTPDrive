package mount

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rclone/rclone/backend/ftp"
	"github.com/rclone/rclone/fs/config/configmap"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/vfs/vfscommon"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"mapftpdrive/internal/config"
	"mapftpdrive/internal/security"
)

const testID = "00000000-0000-4000-8000-000000000001"

var testKnownHost = func() string {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		panic(err)
	}
	return knownhosts.Line([]string{"example.test"}, key)
}()

func testEndpoint(protocol config.Protocol) config.Endpoint {
	endpoint := config.Endpoint{
		ID:            testID,
		Name:          "Archive 2026 Ü",
		Protocol:      protocol,
		Host:          "example.test",
		Port:          config.DefaultPort(protocol, ""),
		Username:      "tester",
		DriveLetter:   "R",
		Scope:         config.User,
		CredentialKey: config.CredentialKeyFor(testID),
	}
	if protocol == config.SFTP {
		endpoint.KnownHost = testKnownHost
	}
	return endpoint
}

func TestBackendOptions(t *testing.T) {
	plain := testEndpoint(config.FTP)
	options, err := backendOptions(Spec{Endpoint: plain, Secret: Secret{Password: "s3cret"}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if options["host"] != "example.test" || options["port"] != "21" || options["user"] != "tester" {
		t.Fatalf("ftp options = %v", options)
	}
	if options["pass"] == "s3cret" {
		t.Fatal("password passed in clear")
	}
	if revealed, err := obscure.Reveal(options["pass"]); err != nil || revealed != "s3cret" {
		t.Fatalf("obscured password = %q, %v", revealed, err)
	}
	anonymous, err := backendOptions(Spec{Endpoint: plain}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, set := anonymous["pass"]; set {
		t.Fatal("empty password should not be passed")
	}

	explicit := testEndpoint(config.FTPS)
	explicit.TLSMode = config.TLSExplicit
	options, _ = backendOptions(Spec{Endpoint: explicit}, false)
	if options["explicit_tls"] != "true" || options["tls"] != "" || options["no_check_certificate"] != "" {
		t.Fatalf("explicit ftps = %v", options)
	}
	implicit := testEndpoint(config.FTPS)
	implicit.TLSMode = config.TLSImplicit
	implicit.CertificatePin = strings.Repeat("ab", 32)
	options, _ = backendOptions(Spec{Endpoint: implicit}, false)
	if options["tls"] != "true" || options["no_check_certificate"] != "true" {
		t.Fatalf("pinned implicit ftps = %v", options)
	}

	sftp := testEndpoint(config.SFTP)
	unpinned := sftp
	unpinned.KnownHost = ""
	if _, err := backendOptions(Spec{Endpoint: unpinned, KnownHostsFile: "x"}, true); !errors.Is(err, security.ErrHostKeyNotPinned) {
		t.Fatalf("unpinned sftp = %v", err)
	}
	options, err = backendOptions(Spec{Endpoint: sftp}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(options["host_keys"], "ssh-ed25519 AAAA") || options["known_hosts_file"] != "" {
		t.Fatalf("native pin = %v", options)
	}
	if options["shell_type"] != "none" || options["disable_hashcheck"] != "true" {
		t.Fatalf("remote commands not disabled: %v", options)
	}
	if _, err := backendOptions(Spec{Endpoint: sftp}, false); err == nil {
		t.Fatal("file pinning without a file accepted")
	}
	options, err = backendOptions(Spec{Endpoint: sftp, KnownHostsFile: `C:\kh`}, false)
	if err != nil || options["known_hosts_file"] != `C:\kh` || options["host_keys"] != "" {
		t.Fatalf("file pin = %v, %v", options, err)
	}
	broken := sftp
	broken.KnownHost = "garbage"
	if _, err := backendOptions(Spec{Endpoint: broken}, true); err == nil {
		t.Fatal("garbage host key accepted")
	}

	keyed := sftp
	keyed.Auth = config.AuthKey
	keyed.KeyFile = `C:\keys\id`
	options, _ = backendOptions(Spec{Endpoint: keyed, Secret: Secret{Password: "phrase"}, KnownHostsFile: `C:\kh`}, false)
	if options["key_file"] != `C:\keys\id` || options["pass"] != "" {
		t.Fatalf("key auth = %v", options)
	}
	if revealed, _ := obscure.Reveal(options["key_file_pass"]); revealed != "phrase" {
		t.Fatalf("passphrase = %q", revealed)
	}

	bogus := plain
	bogus.Protocol = "gopher"
	if _, err := backendOptions(Spec{Endpoint: bogus}, false); err == nil {
		t.Fatal("unknown protocol accepted")
	}
}

func TestMountSettings(t *testing.T) {
	user := testEndpoint(config.SFTP)
	mountOptions, vfsOptions := mountSettings(user)
	if mountOptions.VolumeName != user.Name || mountOptions.NetworkMode {
		t.Fatalf("user mount = %+v", mountOptions)
	}
	for _, flag := range mountOptions.ExtraFlags {
		if strings.HasPrefix(flag, "--VolumePrefix") {
			t.Fatalf("user mount has a prefix: %v", mountOptions.ExtraFlags)
		}
	}
	if vfsOptions.CacheMode != vfscommon.CacheModeWrites || time.Duration(vfsOptions.DirCacheTime) != dirCacheTime {
		t.Fatalf("vfs = %v %v", vfsOptions.CacheMode, vfsOptions.DirCacheTime)
	}

	system := user
	system.Scope = config.System
	mountOptions, _ = mountSettings(system)
	if !mountOptions.NetworkMode || mountOptions.VolumeName != system.Name {
		t.Fatalf("system mount = %+v", mountOptions)
	}
	var prefixes []string
	for _, flag := range mountOptions.ExtraFlags {
		if strings.HasPrefix(flag, "--VolumePrefix=") {
			prefixes = append(prefixes, flag)
		}
	}
	if len(prefixes) != 1 || prefixes[0] != `--VolumePrefix=\mapftpdrive\`+testID {
		t.Fatalf("prefix flags = %v", prefixes)
	}
	// Both scopes report the file system name the Explorer verbs match and
	// satisfy the adapter's volume contract.
	for _, endpoint := range []config.Endpoint{user, system} {
		options, _ := mountSettings(endpoint)
		flags := options.ExtraFlags
		if len(flags) == 0 || flags[len(flags)-1] != FileSystemNameFlag() {
			t.Fatalf("%s mount flags %v do not end with the file system name", endpoint.Scope, flags)
		}
		if _, network, err := resolveVolumePrefix(flags, options.NetworkMode, ""); err != nil || network != options.NetworkMode {
			t.Fatalf("%s mount breaks the adapter contract: network %v, %v", endpoint.Scope, network, err)
		}
	}

	// A 32-unit non-ASCII name is 64 bytes of UTF-8; mountlib would cut
	// VolumeName to 32 bytes, so the full label must travel in ExtraOptions.
	long := user
	long.Name = strings.Repeat("ü", config.MaxNameLength)
	mountOptions, _ = mountSettings(long)
	labels := 0
	for _, option := range mountOptions.ExtraOptions {
		if strings.HasPrefix(option, "volname=") {
			labels++
			if option != "volname="+long.Name {
				t.Fatalf("label option = %q", option)
			}
		}
	}
	if labels != 1 {
		t.Fatalf("label options = %v", mountOptions.ExtraOptions)
	}
	if got := volumeLabelOption(`a,b\c`); got != `volname=a\,b\\c` {
		t.Fatalf("escaped label = %q", got)
	}
}

func TestBackendHasOption(t *testing.T) {
	if !backendHasOption("sftp", "known_hosts_file") {
		t.Fatal("sftp known_hosts_file not found")
	}
	if backendHasOption("sftp", "no_such_option") || backendHasOption("no_such_backend", "x") {
		t.Fatal("unexpected option")
	}
	if RemoteName(testEndpoint(config.FTP)) != config.EndpointKey(testID) {
		t.Fatal("remote name is not the endpoint key")
	}
}

func TestPinRegistry(t *testing.T) {
	registry := &pinRegistry{byHost: make(map[string]map[string]int)}
	first := registry.add("Files.Example.", "AB")
	second := registry.add("files.example", "ab")
	if !registry.has("files.example", "ab") {
		t.Fatal("pin not found")
	}
	first()
	first()
	if !registry.has("files.example", "ab") {
		t.Fatal("release was not reference counted")
	}
	second()
	if registry.has("files.example", "ab") || len(registry.byHost) != 0 {
		t.Fatal("pin survived its last release")
	}
	release := registry.add("192.0.2.1", "cd")
	if !registry.has("", "cd") || pinKey("[2001:db8::1]") != "" {
		t.Fatal("IP-literal hosts must use the empty server name")
	}
	release()
	if err := registry.verify(tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{}}}); err != nil {
		t.Fatalf("verified chain rejected: %v", err)
	}
	if err := registry.verify(tls.ConnectionState{}); err == nil {
		t.Fatal("unverified handshake without certificate accepted")
	}
}

type fakeFTPS struct {
	listener    net.Listener
	certificate *x509.Certificate
}

// startFakeFTPS serves implicit FTPS just far enough for rclone's FTP
// backend to finish the TLS handshake and be refused at login.
func startFakeFTPS(t *testing.T) fakeFTPS {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "self-signed.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
				if _, err := connection.Write([]byte("220 fake\r\n")); err != nil {
					return
				}
				reader := bufio.NewReader(connection)
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					switch strings.ToUpper(strings.Fields(line + " x")[0]) {
					case "USER":
						_, _ = connection.Write([]byte("331 password please\r\n"))
					case "PASS":
						_, _ = connection.Write([]byte("530 login refused by fake server\r\n"))
					case "FEAT":
						_, _ = connection.Write([]byte("211 no features\r\n"))
					default:
						_, _ = connection.Write([]byte("502 not implemented\r\n"))
					}
				}
			}()
		}
	}()
	return fakeFTPS{listener: listener, certificate: leaf}
}

func (f fakeFTPS) options(t *testing.T) configmap.Simple {
	_, port, _ := net.SplitHostPort(f.listener.Addr().String())
	pass, _ := obscure.Obscure("pw")
	return configmap.Simple{
		"host":                 "127.0.0.1",
		"port":                 port,
		"user":                 "u",
		"pass":                 pass,
		"tls":                  "true",
		"no_check_certificate": "true",
	}
}

// TestTLSPinningEnforcedThroughFTPBackend drives rclone's own FTP backend
// against a self-signed server, proving that the pin check sits on every
// FTPS handshake rclone performs and fails closed when verification is off.
func TestTLSPinningEnforcedThroughFTPBackend(t *testing.T) {
	configureRclone(Options{CacheDir: t.TempDir()})
	server := startFakeFTPS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := ftp.NewFs(ctx, "pin-unpinned", "", server.options(t))
	if err == nil || !strings.Contains(err.Error(), "not the certificate pinned") {
		t.Fatalf("unpinned connection = %v", err)
	}

	wrong := tlsPins.add("127.0.0.1", strings.Repeat("0", 64))
	_, err = ftp.NewFs(ctx, "pin-wrong", "", server.options(t))
	wrong()
	if err == nil || !strings.Contains(err.Error(), "not the certificate pinned") {
		t.Fatalf("wrong pin = %v", err)
	}

	right := tlsPins.add("127.0.0.1", security.CertificateSHA256(server.certificate))
	defer right()
	_, err = ftp.NewFs(ctx, "pin-right", "", server.options(t))
	if err == nil || strings.Contains(err.Error(), "not the certificate pinned") || !strings.Contains(err.Error(), "530") {
		t.Fatalf("matching pin should reach login, got %v", err)
	}

	verified := server.options(t)
	delete(verified, "no_check_certificate")
	_, err = ftp.NewFs(ctx, "pin-verified", "", verified)
	if err == nil || strings.Contains(err.Error(), "530") {
		t.Fatalf("an untrusted certificate must fail normal verification, got %v", err)
	}
}

func TestProbeAndMountThroughEngine(t *testing.T) {
	server := startFakeFTPS(t)
	_, portText, _ := net.SplitHostPort(server.listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	engine := NewEngine(Options{CacheDir: t.TempDir()})
	endpoint := testEndpoint(config.FTPS)
	endpoint.Host = "127.0.0.1"
	endpoint.Port = uint16(port) //nolint:gosec // G115: a TCP port from the listener's own address fits in 16 bits.
	endpoint.TLSMode = config.TLSImplicit
	endpoint.CertificatePin = security.CertificateSHA256(server.certificate)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := engine.Probe(ctx, Spec{Endpoint: endpoint, Secret: Secret{Password: "pw"}})
	if err == nil || !strings.Contains(err.Error(), "530") {
		t.Fatalf("probe = %v", err)
	}
	if tlsPins.has("127.0.0.1", endpoint.CertificatePin) {
		t.Fatal("probe leaked its pin registration")
	}
	invalid := endpoint
	invalid.ID = "../escape"
	if err := engine.Probe(ctx, Spec{Endpoint: invalid}); err == nil {
		t.Fatal("invalid endpoint probed")
	}
}
