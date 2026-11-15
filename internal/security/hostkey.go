// Package security reads and compares the server identities MapFTPDrive pins:
// SFTP host keys and FTPS server certificates. The mapper uses the scans for
// trust-on-first-use prompts, the supervisor uses them before each mount to
// report a changed identity precisely, and the engine turns pins into rclone
// options.
package security

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"mapftpdrive/internal/config"
)

// ScanTimeout bounds a whole scan: TCP connect, banner exchange and key
// exchange. ssh.ClientConfig.Timeout covers only the TCP connect, so the
// connection deadline is what stops a silent server from hanging the UI.
const ScanTimeout = 15 * time.Second

type HostKey struct {
	// Line is the known_hosts line stored as Endpoint.KnownHost.
	Line        string
	Fingerprint string
	Algorithm   string
	key         ssh.PublicKey
}

// ErrHostKeyNotPinned is returned for an SFTP endpoint without a stored host
// key; such endpoints are never mounted.
var ErrHostKeyNotPinned = errors.New("the SFTP server's host key has not been verified; use Re-pin host key in Manage FTP drives")

// HostKeyChangedError reports a server presenting a different host key from
// the pinned one. The management window shows both fingerprints and offers
// an explicit re-pin.
type HostKeyChangedError struct {
	Host      string
	Pinned    HostKey
	Presented HostKey
}

func (e *HostKeyChangedError) Error() string {
	return fmt.Sprintf("the SFTP host key of %s has changed (pinned %s, server presented %s); if the change is expected, re-pin the host key",
		e.Host, e.Pinned.Fingerprint, e.Presented.Fingerprint)
}

var errKeyCaptured = errors.New("host key captured")

// ScanSFTPHostKey reads the host key with ScanTimeout as the deadline.
func ScanSFTPHostKey(endpoint config.Endpoint) (HostKey, error) {
	ctx, cancel := context.WithTimeout(context.Background(), ScanTimeout)
	defer cancel()
	return ScanSFTPHostKeyContext(ctx, endpoint)
}

// ScanSFTPHostKeyContext connects, completes the key exchange far enough to
// capture the server's host key and disconnects without authenticating. The
// connection deadline is the earlier of ctx's deadline and ScanTimeout.
func ScanSFTPHostKeyContext(ctx context.Context, endpoint config.Endpoint) (HostKey, error) {
	address := net.JoinHostPort(endpoint.Host, strconv.Itoa(int(endpoint.Port)))
	connection, err := dialWithDeadline(ctx, address)
	if err != nil {
		return HostKey{}, err
	}
	defer connection.Close()

	var captured ssh.PublicKey
	clientConfig := &ssh.ClientConfig{
		User:    endpoint.Username,
		Timeout: ScanTimeout,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			captured = key
			return errKeyCaptured
		},
	}
	_, _, _, handshakeErr := ssh.NewClientConn(connection, address, clientConfig)
	if captured == nil {
		if ctx.Err() != nil {
			return HostKey{}, fmt.Errorf("read SFTP host key: %w", ctx.Err())
		}
		return HostKey{}, fmt.Errorf("read SFTP host key: %w", handshakeErr)
	}
	return newHostKey(knownhosts.Line([]string{address}, captured), captured), nil
}

// dialWithDeadline connects and arms a deadline that covers every later read
// and write, and closes the connection early if ctx is cancelled.
func dialWithDeadline(ctx context.Context, address string) (net.Conn, error) {
	deadline := time.Now().Add(ScanTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	dialer := net.Dialer{Deadline: deadline}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	if err := connection.SetDeadline(deadline); err != nil {
		connection.Close()
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { _ = connection.SetDeadline(time.Unix(1, 0)) })
	return &stoppingConn{Conn: connection, stop: stop}, nil
}

type stoppingConn struct {
	net.Conn
	stop func() bool
}

func (c *stoppingConn) Close() error {
	c.stop()
	return c.Conn.Close()
}

func newHostKey(line string, key ssh.PublicKey) HostKey {
	return HostKey{
		Line:        line,
		Fingerprint: ssh.FingerprintSHA256(key),
		Algorithm:   key.Type(),
		key:         key,
	}
}

// ParseKnownHost decodes a stored Endpoint.KnownHost line.
func ParseKnownHost(line string) (HostKey, error) {
	_, _, key, _, _, err := ssh.ParseKnownHosts([]byte(line))
	if err != nil {
		return HostKey{}, fmt.Errorf("pinned host key: %w", err)
	}
	return newHostKey(line, key), nil
}

// SameKey reports whether two host keys are byte-for-byte identical.
func (k HostKey) SameKey(other HostKey) bool {
	if k.key == nil || other.key == nil {
		return false
	}
	return bytes.Equal(k.key.Marshal(), other.key.Marshal())
}

// HostKeysOption renders the pinned key in the "algorithm base64-key" form
// of rclone's sftp host_keys option.
func (k HostKey) HostKeysOption() string {
	if k.key == nil {
		return ""
	}
	return k.key.Type() + " " + base64.StdEncoding.EncodeToString(k.key.Marshal())
}

// CheckHostKey compares the server's current key with the pinned line.
func CheckHostKey(endpoint config.Endpoint, presented HostKey) error {
	if endpoint.KnownHost == "" {
		return ErrHostKeyNotPinned
	}
	pinned, err := ParseKnownHost(endpoint.KnownHost)
	if err != nil {
		return err
	}
	if !pinned.SameKey(presented) {
		return &HostKeyChangedError{Host: endpoint.Host, Pinned: pinned, Presented: presented}
	}
	return nil
}
