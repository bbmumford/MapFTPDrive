package security

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"mapftpdrive/internal/config"
)

// Certificate describes the leaf certificate an FTPS server presented.
type Certificate struct {
	// SHA256 is the lowercase hexadecimal digest of the DER encoding; it is
	// the value stored as Endpoint.CertificatePin.
	SHA256    string
	Subject   string
	Issuer    string
	NotBefore time.Time
	NotAfter  time.Time
	// Trusted is set when the chain verifies against the system trust store
	// for the endpoint's host name. Such servers are verified normally by
	// rclone and are not pinned, so routine renewals do not interrupt them.
	Trusted bool
	// TrustError explains why the certificate is not trusted.
	TrustError string
	// NameMatches reports whether the certificate is valid for the host name
	// the endpoint dials, independent of who issued it.
	NameMatches bool
}

// ErrCertificateUntrusted is returned for an FTPS server whose certificate
// is neither trusted by Windows nor pinned.
var ErrCertificateUntrusted = errors.New("the FTPS server's certificate is not trusted by Windows and has not been pinned; use Re-pin certificate in Manage FTP drives")

// CertificateChangedError reports a server presenting a certificate other
// than the pinned one.
type CertificateChangedError struct {
	Host      string
	Pinned    string
	Presented Certificate
}

func (e *CertificateChangedError) Error() string {
	return fmt.Sprintf("the FTPS certificate of %s has changed (pinned SHA-256 %s, server presented %s); if the change is expected, re-pin the certificate",
		e.Host, FormatSHA256(e.Pinned), FormatSHA256(e.Presented.SHA256))
}

// CertificateSHA256 is the pin value for a certificate.
func CertificateSHA256(certificate *x509.Certificate) string {
	sum := sha256.Sum256(certificate.Raw)
	return hex.EncodeToString(sum[:])
}

// FormatSHA256 renders a hexadecimal digest as colon-separated uppercase
// byte pairs, the form Windows certificate dialogs use.
func FormatSHA256(digest string) string {
	digest = strings.ToUpper(digest)
	pairs := make([]string, 0, len(digest)/2)
	for index := 0; index+1 < len(digest); index += 2 {
		pairs = append(pairs, digest[index:index+2])
	}
	return strings.Join(pairs, ":")
}

// ScanTLSCertificate performs the FTPS handshake (implicit TLS directly,
// explicit TLS after AUTH TLS) without verifying, reports the leaf
// certificate and whether Windows would trust it, and disconnects before
// logging in.
func ScanTLSCertificate(ctx context.Context, endpoint config.Endpoint) (Certificate, error) {
	address := net.JoinHostPort(endpoint.Host, strconv.Itoa(int(endpoint.Port)))
	connection, err := dialWithDeadline(ctx, address)
	if err != nil {
		return Certificate{}, err
	}
	defer connection.Close()
	if endpoint.TLSMode != config.TLSImplicit {
		if err := startExplicitTLS(connection); err != nil {
			return Certificate{}, err
		}
	}
	client := tls.Client(connection, &tls.Config{
		ServerName:         endpoint.Host,
		InsecureSkipVerify: true, // the chain is verified below so an untrusted certificate can still be shown for pinning
	})
	if err := client.HandshakeContext(ctx); err != nil {
		return Certificate{}, fmt.Errorf("TLS handshake with %s: %w", address, err)
	}
	state := client.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return Certificate{}, fmt.Errorf("%s presented no certificate", address)
	}
	return describeCertificate(endpoint.Host, state.PeerCertificates, nil), nil
}

// describeCertificate summarises the leaf and verifies the chain; roots nil
// means the system trust store.
func describeCertificate(host string, chain []*x509.Certificate, roots *x509.CertPool) Certificate {
	leaf := chain[0]
	intermediates := x509.NewCertPool()
	for _, certificate := range chain[1:] {
		intermediates.AddCert(certificate)
	}
	result := Certificate{
		SHA256:      CertificateSHA256(leaf),
		Subject:     leaf.Subject.String(),
		Issuer:      leaf.Issuer.String(),
		NotBefore:   leaf.NotBefore,
		NotAfter:    leaf.NotAfter,
		NameMatches: leaf.VerifyHostname(host) == nil,
	}
	_, err := leaf.Verify(x509.VerifyOptions{DNSName: host, Intermediates: intermediates, Roots: roots})
	if err == nil {
		result.Trusted = true
	} else {
		result.TrustError = err.Error()
	}
	return result
}

// startExplicitTLS reads the FTP greeting and negotiates AUTH TLS.
func startExplicitTLS(connection net.Conn) error {
	reader := textproto.NewReader(bufio.NewReader(connection))
	if _, _, err := reader.ReadResponse(220); err != nil {
		return fmt.Errorf("FTP greeting: %w", err)
	}
	if _, err := connection.Write([]byte("AUTH TLS\r\n")); err != nil {
		return err
	}
	if _, _, err := reader.ReadResponse(234); err != nil {
		return fmt.Errorf("the server refused explicit TLS (AUTH TLS): %w", err)
	}
	return nil
}

// CheckCertificate decides whether a presented certificate is acceptable
// for endpoint: a pinned digest must match exactly; without a pin the
// certificate must be trusted by Windows.
func CheckCertificate(endpoint config.Endpoint, presented Certificate) error {
	if endpoint.CertificatePin != "" {
		if !strings.EqualFold(endpoint.CertificatePin, presented.SHA256) {
			return &CertificateChangedError{Host: endpoint.Host, Pinned: endpoint.CertificatePin, Presented: presented}
		}
		return nil
	}
	if !presented.Trusted {
		return fmt.Errorf("%w (%s)", ErrCertificateUntrusted, presented.TrustError)
	}
	return nil
}
