package mount

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/rclone/rclone/fs/fshttp"
	"mapftpdrive/internal/security"
)

// rclone's FTP backend has no certificate-pinning option. Every FTPS
// connection it makes (control and data, implicit and explicit) uses a
// clone of the TLS configuration of rclone's process-wide HTTP transport,
// with ServerName set to the endpoint host and InsecureSkipVerify set by
// no_check_certificate. installTLSPinning adds a VerifyConnection callback
// to that shared configuration before any clone exists, and the clones
// inherit it:
//
//   - a handshake whose chain verified normally is accepted unchanged;
//   - otherwise (no_check_certificate, which the engine sets only for a
//     pinned endpoint) the leaf certificate's SHA-256 must be pinned for the
//     connection's server name, and anything else aborts the handshake.
//
// The second rule makes the check fail closed: no FTPS connection skips
// verification without a matching pin. TestTLSPinningEnforcedThroughFTPBackend
// exercises the real backend so a change in how rclone builds its TLS
// configuration is caught.
var tlsPins = &pinRegistry{byHost: make(map[string]map[string]int)}

func installTLSPinning() {
	shared := fshttp.NewTransport(context.Background()).TLSClientConfig
	shared.VerifyConnection = tlsPins.verify
}

type pinRegistry struct {
	mu     sync.RWMutex
	byHost map[string]map[string]int
}

// pinKey maps a host to the server name a TLS client reports. Go reports
// an empty server name when ServerName is an IP literal (no SNI), so pins
// for IP-addressed servers share the empty key; accepting one of them for
// another IP still requires that server's private key.
func pinKey(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if net.ParseIP(strings.Trim(host, "[]")) != nil {
		return ""
	}
	return host
}

// add registers digest for host and returns its release; registrations are
// counted so two mounts of one server can come and go independently.
func (r *pinRegistry) add(host, digest string) func() {
	key, digest := pinKey(host), strings.ToLower(digest)
	r.mu.Lock()
	if r.byHost[key] == nil {
		r.byHost[key] = make(map[string]int)
	}
	r.byHost[key][digest]++
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if r.byHost[key][digest]--; r.byHost[key][digest] <= 0 {
				delete(r.byHost[key], digest)
			}
			if len(r.byHost[key]) == 0 {
				delete(r.byHost, key)
			}
		})
	}
}

func (r *pinRegistry) has(serverName, digest string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byHost[pinKey(serverName)][digest] > 0
}

func (r *pinRegistry) verify(state tls.ConnectionState) error {
	if len(state.VerifiedChains) > 0 {
		return nil
	}
	if len(state.PeerCertificates) == 0 {
		return errors.New("FTPS server presented no certificate")
	}
	digest := security.CertificateSHA256(state.PeerCertificates[0])
	if r.has(state.ServerName, digest) {
		return nil
	}
	return fmt.Errorf("FTPS certificate %s is not the certificate pinned for this server", security.FormatSHA256(digest))
}
