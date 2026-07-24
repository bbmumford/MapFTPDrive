package security

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"mapftpdrive/internal/config"
)

type HostKey struct {
	Line        string
	Fingerprint string
	Algorithm   string
}

var errKeyCaptured = errors.New("host key captured")

func ScanSFTPHostKey(endpoint config.Endpoint) (HostKey, error) {
	address := net.JoinHostPort(endpoint.Host, strconv.Itoa(int(endpoint.Port)))
	connection, err := net.DialTimeout("tcp", address, 10*time.Second)
	if err != nil {
		return HostKey{}, err
	}
	defer connection.Close()

	var captured ssh.PublicKey
	clientConfig := &ssh.ClientConfig{
		User:    endpoint.Username,
		Timeout: 10 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			captured = key
			return errKeyCaptured
		},
	}
	_, _, _, handshakeErr := ssh.NewClientConn(connection, address, clientConfig)
	if captured == nil {
		return HostKey{}, fmt.Errorf("read SFTP host key: %w", handshakeErr)
	}
	return HostKey{
		Line:        knownhosts.Line([]string{address}, captured),
		Fingerprint: ssh.FingerprintSHA256(captured),
		Algorithm:   captured.Type(),
	}, nil
}
