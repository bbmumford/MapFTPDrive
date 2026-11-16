package security

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

	"golang.org/x/crypto/ssh"
	"mapftpdrive/internal/config"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener
}

func endpointFor(t *testing.T, listener net.Listener, protocol config.Protocol) config.Endpoint {
	t.Helper()
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	return config.Endpoint{Host: host, Port: uint16(port), Protocol: protocol, Username: "probe"} //nolint:gosec // G115: a TCP port from the listener's own address fits in 16 bits.
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func serveSSH(t *testing.T, listener net.Listener, signer ssh.Signer) {
	serverConfig := &ssh.ServerConfig{NoClientAuth: false, PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
		return nil, errors.New("denied")
	}}
	serverConfig.AddHostKey(signer)
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _, _, _ = ssh.NewServerConn(connection, serverConfig)
			}()
		}
	}()
}

func TestScanSFTPHostKeyAndCheck(t *testing.T) {
	listener := listen(t)
	signer := newSigner(t)
	serveSSH(t, listener, signer)
	endpoint := endpointFor(t, listener, config.SFTP)

	key, err := ScanSFTPHostKey(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if key.Fingerprint != ssh.FingerprintSHA256(signer.PublicKey()) || key.Algorithm != ssh.KeyAlgoED25519 {
		t.Fatalf("scanned %+v", key)
	}
	if !strings.HasPrefix(key.HostKeysOption(), "ssh-ed25519 AAAA") {
		t.Fatalf("host_keys option = %q", key.HostKeysOption())
	}

	if err := CheckHostKey(endpoint, key); !errors.Is(err, ErrHostKeyNotPinned) {
		t.Fatalf("unpinned check = %v", err)
	}
	endpoint.KnownHost = key.Line
	if err := CheckHostKey(endpoint, key); err != nil {
		t.Fatalf("matching key rejected: %v", err)
	}
	parsed, err := ParseKnownHost(key.Line)
	if err != nil || !parsed.SameKey(key) || parsed.HostKeysOption() != key.HostKeysOption() {
		t.Fatalf("parsed %+v, %v", parsed, err)
	}

	other := newHostKey("x", newSigner(t).PublicKey())
	err = CheckHostKey(endpoint, other)
	var changed *HostKeyChangedError
	if !errors.As(err, &changed) || changed.Pinned.Fingerprint != key.Fingerprint || changed.Presented.Fingerprint != other.Fingerprint {
		t.Fatalf("changed key = %v", err)
	}
	if !strings.Contains(err.Error(), key.Fingerprint) || !strings.Contains(err.Error(), other.Fingerprint) {
		t.Fatalf("message lacks both fingerprints: %v", err)
	}

	endpoint.KnownHost = "not a key"
	if err := CheckHostKey(endpoint, key); err == nil {
		t.Fatal("garbage pin accepted")
	}
	if (HostKey{}).SameKey(key) || (HostKey{}).HostKeysOption() != "" {
		t.Fatal("empty key compared equal or rendered")
	}
}

func TestScanSFTPHostKeyHonoursDeadline(t *testing.T) {
	listener := listen(t)
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			defer connection.Close() // hold the connection open and say nothing
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := ScanSFTPHostKeyContext(ctx, endpointFor(t, listener, config.SFTP))
	if err == nil {
		t.Fatal("silent server produced a key")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("scan ignored its deadline: %v", elapsed)
	}
}

func TestScanRefusedConnection(t *testing.T) {
	listener := listen(t)
	endpoint := endpointFor(t, listener, config.SFTP)
	listener.Close()
	if _, err := ScanSFTPHostKey(endpoint); err == nil {
		t.Fatal("closed port scanned")
	}
	endpoint.Protocol = config.FTPS
	if _, err := ScanTLSCertificate(context.Background(), endpoint); err == nil {
		t.Fatal("closed port scanned")
	}
}

func selfSigned(t *testing.T) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "ftps.test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &private.PublicKey, private)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}, parsed
}

func serveTLS(listener net.Listener, certificate tls.Certificate, explicit bool) {
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
				if explicit {
					_, _ = connection.Write([]byte("220-Welcome\r\n220 Ready\r\n"))
					line, err := bufio.NewReader(connection).ReadString('\n')
					if err != nil || strings.TrimSpace(line) != "AUTH TLS" {
						_, _ = connection.Write([]byte("500 no\r\n"))
						return
					}
					_, _ = connection.Write([]byte("234 Proceed\r\n"))
				}
				server := tls.Server(connection, &tls.Config{Certificates: []tls.Certificate{certificate}})
				_ = server.Handshake()
			}()
		}
	}()
}

func TestScanTLSCertificate(t *testing.T) {
	certificate, leaf := selfSigned(t)
	for _, mode := range []config.TLSMode{config.TLSImplicit, config.TLSExplicit} {
		t.Run(string(mode), func(t *testing.T) {
			listener := listen(t)
			serveTLS(listener, certificate, mode == config.TLSExplicit)
			endpoint := endpointFor(t, listener, config.FTPS)
			endpoint.TLSMode = mode
			scanned, err := ScanTLSCertificate(context.Background(), endpoint)
			if err != nil {
				t.Fatal(err)
			}
			if scanned.SHA256 != CertificateSHA256(leaf) || scanned.Trusted || !scanned.NameMatches || scanned.TrustError == "" {
				t.Fatalf("scanned %+v", scanned)
			}
			if !strings.Contains(scanned.Subject, "ftps.test") {
				t.Fatalf("subject = %q", scanned.Subject)
			}
			if err := CheckCertificate(endpoint, scanned); !errors.Is(err, ErrCertificateUntrusted) {
				t.Fatalf("untrusted unpinned = %v", err)
			}
			endpoint.CertificatePin = scanned.SHA256
			if err := CheckCertificate(endpoint, scanned); err != nil {
				t.Fatalf("pinned = %v", err)
			}
			endpoint.CertificatePin = strings.Repeat("0", 64)
			var changed *CertificateChangedError
			if err := CheckCertificate(endpoint, scanned); !errors.As(err, &changed) || !strings.Contains(err.Error(), FormatSHA256(scanned.SHA256)) {
				t.Fatalf("changed = %v", err)
			}
		})
	}
}

func TestExplicitTLSRefused(t *testing.T) {
	listener := listen(t)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		_, _ = connection.Write([]byte("220 Ready\r\n"))
		_, _ = bufio.NewReader(connection).ReadString('\n')
		_, _ = connection.Write([]byte("530 TLS not available\r\n"))
	}()
	endpoint := endpointFor(t, listener, config.FTPS)
	endpoint.TLSMode = config.TLSExplicit
	if _, err := ScanTLSCertificate(context.Background(), endpoint); err == nil || !strings.Contains(err.Error(), "AUTH TLS") {
		t.Fatalf("refused AUTH TLS = %v", err)
	}
}

func TestDescribeCertificateTrustedRoot(t *testing.T) {
	_, leaf := selfSigned(t)
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	described := describeCertificate("localhost", []*x509.Certificate{leaf}, roots)
	if !described.Trusted || described.TrustError != "" {
		t.Fatalf("trusted root = %+v", described)
	}
	if err := CheckCertificate(config.Endpoint{Host: "localhost"}, described); err != nil {
		t.Fatalf("trusted unpinned = %v", err)
	}
	mismatch := describeCertificate("other.example", []*x509.Certificate{leaf}, roots)
	if mismatch.Trusted || mismatch.NameMatches {
		t.Fatalf("name mismatch = %+v", mismatch)
	}
}

func TestFormatSHA256(t *testing.T) {
	if got := FormatSHA256("0aff10"); got != "0A:FF:10" {
		t.Fatalf("FormatSHA256 = %q", got)
	}
}
