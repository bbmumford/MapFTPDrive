// Package config holds the persisted endpoint model, its validation rules and
// the versioned on-disk store shared by the per-user host, the system service
// and the user interface.
package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf16"
)

type Protocol string

const (
	FTP  Protocol = "ftp"
	FTPS Protocol = "ftps"
	SFTP Protocol = "sftp"
)

type Scope string

const (
	User   Scope = "user"
	System Scope = "system"
)

type TLSMode string

const (
	TLSExplicit TLSMode = "explicit"
	TLSImplicit TLSMode = "implicit"
)

// AuthMethod selects how the endpoint authenticates. The secret for either
// method (password or private-key passphrase) lives in Credential Manager
// under CredentialKey, never in the store.
type AuthMethod string

const (
	AuthPassword AuthMethod = "password"
	AuthKey      AuthMethod = "key"
)

// MaxNameLength is the WinFsp volume label capacity in UTF-16 code units. The
// name is passed verbatim as the label, so a longer name would be truncated
// by the driver and no longer match the stored definition.
const MaxNameLength = 32

// Default ports used by the mapper when the user has not typed one.
const (
	DefaultFTPPort          = 21
	DefaultImplicitFTPSPort = 990
	DefaultSFTPPort         = 22
)

type Endpoint struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Protocol       Protocol   `json:"protocol"`
	TLSMode        TLSMode    `json:"tlsMode,omitempty"`
	Host           string     `json:"host"`
	Port           uint16     `json:"port"`
	Username       string     `json:"username"`
	DriveLetter    string     `json:"driveLetter"`
	RemotePath     string     `json:"remotePath,omitempty"`
	Scope          Scope      `json:"scope"`
	CredentialKey  string     `json:"credentialKey"`
	Disabled       bool       `json:"disabled,omitempty"`
	Auth           AuthMethod `json:"auth,omitempty"`
	KeyFile        string     `json:"keyFile,omitempty"`
	KnownHost      string     `json:"knownHost,omitempty"`
	CertificatePin string     `json:"certificatePin,omitempty"`
}

// DefaultPort returns the conventional port for a protocol and FTPS mode:
// 22 for SFTP, 990 for implicit FTPS and 21 for FTP and explicit FTPS.
func DefaultPort(protocol Protocol, mode TLSMode) uint16 {
	switch {
	case protocol == SFTP:
		return DefaultSFTPPort
	case protocol == FTPS && mode == TLSImplicit:
		return DefaultImplicitFTPSPort
	default:
		return DefaultFTPPort
	}
}

// CredentialKeyFor is the only credential key a stored endpoint may name.
// Binding the key to the ID stops an edited store from pointing a mount at a
// different Credential Manager entry.
func CredentialKeyFor(id string) string {
	return "endpoint/" + id
}

// AuthMethodOrDefault reports the effective authentication method; an empty
// value is password authentication.
func (e Endpoint) AuthMethodOrDefault() AuthMethod {
	if e.Auth == "" {
		return AuthPassword
	}
	return e.Auth
}

// SameServer reports whether two definitions reach the same account on the
// same server, which lets Map reuse a disabled definition instead of
// creating a second one for the same letter.
func (e Endpoint) SameServer(other Endpoint) bool {
	a, b := e.Normalized(), other.Normalized()
	return a.Protocol == b.Protocol && strings.EqualFold(a.Host, b.Host) && a.Port == b.Port &&
		a.Username == b.Username && a.RemotePath == b.RemotePath
}

// ValidateID accepts only the canonical lowercase UUID form produced by
// app.NewID. IDs name credential targets, cache keys and the drive index, so
// anything else (path separators, dots, spaces) is rejected outright.
func ValidateID(id string) error {
	if len(id) != 36 {
		return fmt.Errorf("endpoint id %q is not a UUID", id)
	}
	for index, char := range id {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return fmt.Errorf("endpoint id %q is not a UUID", id)
			}
		default:
			if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
				return fmt.Errorf("endpoint id %q is not a UUID", id)
			}
		}
	}
	return nil
}

// ValidateName checks a drive name for use as the WinFsp volume label: at
// most MaxNameLength UTF-16 units and none of the characters that the FUSE
// option parser or a UNC path would split on.
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("drive name is required")
	}
	if units := len(utf16.Encode([]rune(name))); units > MaxNameLength {
		return fmt.Errorf("drive name is %d characters long; the limit is %d", units, MaxNameLength)
	}
	for _, char := range name {
		switch {
		case char == '\\' || char == '/' || char == ',':
			return fmt.Errorf("drive name cannot contain %q", char)
		case unicode.IsControl(char):
			return errors.New("drive name cannot contain control characters")
		}
	}
	return nil
}

// IsAbsoluteWindowsPath accepts drive-rooted (C:\...) and UNC (\\server\...)
// paths. It is checked textually so that definitions written on Windows
// validate identically in tests on other systems.
func IsAbsoluteWindowsPath(path string) bool {
	if strings.HasPrefix(path, `\\`) {
		return len(path) > 2
	}
	return len(path) >= 3 && unicode.IsLetter(rune(path[0])) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func (e Endpoint) Validate() error {
	e = e.Normalized()
	if err := ValidateID(e.ID); err != nil {
		return err
	}
	if err := ValidateName(e.Name); err != nil {
		return err
	}
	switch e.Protocol {
	case FTP, FTPS, SFTP:
	default:
		return fmt.Errorf("unsupported protocol %q", e.Protocol)
	}
	if e.TLSMode != "" {
		if e.Protocol != FTPS {
			return errors.New("TLS mode applies only to FTPS")
		}
		if e.TLSMode != TLSExplicit && e.TLSMode != TLSImplicit {
			return fmt.Errorf("unsupported FTPS mode %q", e.TLSMode)
		}
	}
	if e.Host == "" {
		return errors.New("host is required")
	}
	if strings.ContainsAny(e.Host, " /\\@") || hasControl(e.Host) {
		return fmt.Errorf("host %q is not a valid server name", e.Host)
	}
	if e.Port == 0 {
		return errors.New("port is required")
	}
	if hasControl(e.Username) || hasControl(e.RemotePath) {
		return errors.New("username and remote path cannot contain control characters")
	}
	if len(e.DriveLetter) != 1 || e.DriveLetter[0] < 'A' || e.DriveLetter[0] > 'Z' {
		return errors.New("drive letter must be A through Z")
	}
	if e.Scope != User && e.Scope != System {
		return fmt.Errorf("unsupported scope %q", e.Scope)
	}
	if e.CredentialKey != CredentialKeyFor(e.ID) {
		return fmt.Errorf("credential key must be %q", CredentialKeyFor(e.ID))
	}
	switch e.AuthMethodOrDefault() {
	case AuthPassword:
		if e.KeyFile != "" {
			return errors.New("a key file requires key authentication")
		}
	case AuthKey:
		if e.Protocol != SFTP {
			return errors.New("private-key login is available only for SFTP")
		}
		if !IsAbsoluteWindowsPath(e.KeyFile) && !filepath.IsAbs(e.KeyFile) {
			return errors.New("private-key login needs the full path of the key file")
		}
		if hasControl(e.KeyFile) {
			return errors.New("key file path cannot contain control characters")
		}
	default:
		return fmt.Errorf("unsupported authentication method %q", e.Auth)
	}
	if e.KnownHost != "" && (e.Protocol != SFTP || hasControl(e.KnownHost)) {
		return errors.New("a pinned host key applies only to SFTP and must be a single line")
	}
	if e.CertificatePin != "" {
		if e.Protocol != FTPS {
			return errors.New("a pinned certificate applies only to FTPS")
		}
		if !isSHA256Hex(e.CertificatePin) {
			return errors.New("pinned certificate must be a SHA-256 digest in hexadecimal")
		}
	}
	return nil
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func (e Endpoint) Normalized() Endpoint {
	e.ID = strings.ToLower(strings.TrimSpace(e.ID))
	e.Name = strings.TrimSpace(e.Name)
	e.Host = strings.TrimSpace(e.Host)
	e.Username = strings.TrimSpace(e.Username)
	e.DriveLetter = NormalizeDrive(e.DriveLetter)
	e.RemotePath = strings.TrimSpace(strings.ReplaceAll(e.RemotePath, `\`, "/"))
	e.CredentialKey = strings.TrimSpace(e.CredentialKey)
	e.KeyFile = strings.TrimSpace(e.KeyFile)
	e.KnownHost = strings.TrimSpace(e.KnownHost)
	e.CertificatePin = strings.ToLower(strings.TrimSpace(e.CertificatePin))
	if e.Auth == AuthPassword {
		e.Auth = ""
	}
	return e
}

// NormalizeDrive reduces "x", "x:", `X:\` and `"X:\"` style arguments to the
// single uppercase letter the store keys on.
func NormalizeDrive(drive string) string {
	drive = strings.TrimSpace(drive)
	drive = strings.TrimRight(drive, `"\/.`)
	drive = strings.TrimSuffix(drive, ":")
	return strings.ToUpper(strings.TrimSpace(drive))
}
